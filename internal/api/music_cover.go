package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/fileperms"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
)

// Cover art. The interface never talks to an outside image host: the
// front cover of a release group is fetched once from the Cover Art Archive,
// kept under the config folder (music-covers/) and served from there by
// GET /api/music/albums/{id}/cover and /api/music/artists/{id}/cover. An
// album that already has a cover.jpg (or folder.jpg, cover.png...) in its
// own folder is served from that file instead, and never changed.

const (
	// coverMissingFor is how long "this release group has no cover art" is
	// remembered before the archive is asked again.
	coverMissingFor = 24 * time.Hour
	// coverMaxAge is how long a browser may keep a cover.
	coverMaxAge = 24 * time.Hour
	// coverFetchTimeout bounds one download from the archive.
	coverFetchTimeout = 20 * time.Second
)

// coverLocalNames are the cover files looked for in an album folder, in
// order of preference.
var coverLocalNames = []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.webp", "folder.jpg", "folder.jpeg", "folder.png", "folder.webp"}

// mbidPattern is what a MusicBrainz id looks like (a UUID); anything else is
// never used to build a file name.
var mbidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// maxCoverFiles is how many cover pictures the cache keeps (the small "no
// cover" marks are not counted, they have a clock of their own). Any account
// can ask for the cover of any id, so without a limit the folder would grow
// with every made-up one; the limit is well above the albums of a big library
// so a library cover is not pushed out and fetched again. Thumbnails are tiny
// and have their own, larger limit.
const (
	maxCoverFiles = 20000
	maxThumbFiles = 60000
)

// coverPruneEvery is how many new cache files are written between clean-ups.
const coverPruneEvery = 50

// coverTouchEvery is how often a cover that is looked at gets its modified
// time refreshed (the clean-up removes the oldest first). Rarely is enough
// with the generous limit, and it saves a disk write on a busy page.
const coverTouchEvery = 3 * 24 * time.Hour

// coverLocks holds the work in progress for covers: one download per release
// group, one thumbnail per picture, one search per artist, all shared between
// the requests that want the same thing.
type coverLocks struct {
	mu     sync.Mutex
	m      map[string]*sync.Mutex
	refs   map[string]int // how many callers hold or wait for each lock in m
	writes atomic.Int64   // cache files written, to know when to prune

	fetches flightGroup[string]      // archive downloads, by release group id
	thumbs  flightGroup[string]      // thumbnails, by file name
	artists flightGroup[artistCover] // the search for an artist's picture, by artist id
	failed  sync.Map                 // release group id -> time of the last failed download
	memo    sync.Map                 // artist id -> artistCover, what the last search found
}

// coverWork lets a few jobs at most run at once: downloads from the archive and
// the resizing of pictures. A grid of covers then cannot fill every connection
// to the archive (or the CPU) at once, and the rest wait their turn.
var coverWork = make(chan struct{}, 4)

func acquireCoverWork(ctx context.Context) error {
	select {
	case coverWork <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseCoverWork() { <-coverWork }

// flightGroup runs one call at a time for a key and gives its answer to every
// request that asked meanwhile. The call is not tied to the request that
// started it: when the browser gives up (the picture scrolled away), the work
// finishes anyway and the next request finds it done.
type flightGroup[T any] struct {
	mu sync.Mutex
	m  map[string]*flightCall[T]
}

type flightCall[T any] struct {
	done chan struct{}
	val  T
	err  error
}

func (g *flightGroup[T]) do(ctx context.Context, key string, fn func() (T, error)) (T, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = map[string]*flightCall[T]{}
	}
	c, ok := g.m[key]
	if !ok {
		c = &flightCall[T]{done: make(chan struct{})}
		g.m[key] = c
		go func() {
			defer func() {
				if r := recover(); r != nil {
					c.err = fmt.Errorf("cover work failed: %v", r)
				}
				g.mu.Lock()
				delete(g.m, key)
				g.mu.Unlock()
				close(c.done)
			}()
			c.val, c.err = fn()
		}()
	}
	g.mu.Unlock()
	select {
	case <-c.done:
		return c.val, c.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// noteWrite counts a new cache file and cleans the cache now and then.
func (c *coverLocks) noteWrite(dir string) {
	if c.writes.Add(1)%coverPruneEvery == 0 {
		pruneCoverCache(dir, maxCoverFiles, maxThumbFiles, coverMissingFor)
	}
}

// touchCover marks a cached cover as used now (at most every few days), so the
// clean-up, which removes the oldest files first, keeps the covers people look
// at and drops one-off ones.
func touchCover(path string, st os.FileInfo) {
	if time.Since(st.ModTime()) > coverTouchEvery {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
}

// pruneCoverCache removes "no cover" marks older than missingFor and stale
// leftovers, then the oldest pictures until at most maxCovers remain and the
// oldest thumbnails until at most maxThumbs remain. The marks are never
// counted: they are empty files that expire on their own.
func pruneCoverCache(dir string, maxCovers, maxThumbs int, missingFor time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		mod  time.Time
	}
	var covers, thumbs []file
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".img") || strings.HasSuffix(name, ".none") || strings.HasSuffix(name, ".tmp")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if (strings.HasSuffix(name, ".none") && time.Since(info.ModTime()) > missingFor) ||
			(strings.HasSuffix(name, ".tmp") && time.Since(info.ModTime()) > time.Hour) {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		switch {
		case !strings.HasSuffix(name, ".img"):
		case strings.HasPrefix(name, thumbPrefix):
			thumbs = append(thumbs, file{filepath.Join(dir, name), info.ModTime()})
		default:
			covers = append(covers, file{filepath.Join(dir, name), info.ModTime()})
		}
	}
	for _, group := range []struct {
		files []file
		max   int
	}{{covers, maxCovers}, {thumbs, maxThumbs}} {
		if len(group.files) <= group.max {
			continue
		}
		sort.Slice(group.files, func(i, j int) bool { return group.files[i].mod.Before(group.files[j].mod) })
		for _, f := range group.files[:len(group.files)-group.max] {
			_ = os.Remove(f.path)
		}
	}
}

// lock is used by the book covers (books_cover.go): one download per key.
func (c *coverLocks) lock(key string) (unlock func()) {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*sync.Mutex{}
		c.refs = map[string]int{}
	}
	l := c.m[key]
	if l == nil {
		l = &sync.Mutex{}
		c.m[key] = l
	}
	c.refs[key]++
	c.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		// The entry goes with its last user: an address nobody asks for again
		// must not stay in the map for ever.
		c.mu.Lock()
		if c.refs[key]--; c.refs[key] <= 0 {
			delete(c.refs, key)
			delete(c.m, key)
		}
		c.mu.Unlock()
	}
}

func (s *Server) coverCacheDir() string { return filepath.Join(s.cfg.ConfigDir, "music-covers") }

// coverURL is the address of an album's cover on this server.
func albumCoverURL(albumID int64) string {
	return fmt.Sprintf("/api/music/albums/%d/cover", albumID)
}

func artistCoverURL(artistID int64) string {
	return fmt.Sprintf("/api/music/artists/%d/cover", artistID)
}

// localCover returns a cover file kept in the album's own folder, if there
// is one inside the music folder. The folder is resolved once (not once per
// file name) and read once; a file that is a real file inside it needs no
// further checking, only a link is followed and checked.
func (s *Server) localCover(album music.Album) (string, bool) {
	root := s.musicRoot()
	if album.Path == "" || root == "" {
		return "", false
	}
	folder, err := cleanup.InsideRoot(root, album.Path)
	if err != nil {
		return "", false
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", false
	}
	have := make(map[string]fs.DirEntry, len(entries))
	for _, e := range entries {
		have[e.Name()] = e
	}
	for _, name := range coverLocalNames {
		e, ok := have[name]
		if !ok {
			continue
		}
		path := filepath.Join(folder, name)
		switch {
		case e.Type().IsRegular():
			return path, true
		case e.Type()&fs.ModeSymlink != 0:
			real, err := cleanup.InsideRoot(root, path)
			if err != nil {
				continue
			}
			if st, err := os.Stat(real); err == nil && st.Mode().IsRegular() {
				return real, true
			}
		}
	}
	return "", false
}

// cachedCover fetches an album's cover into the cache when it is not there
// and returns the cached file. It returns musicbrainz.ErrNotFound when the
// archive has no cover for the release group (remembered for a day), and
// another error when the archive could not be reached (not remembered).
func (s *Server) cachedCover(ctx context.Context, album music.Album) (string, error) {
	return s.cachedReleaseGroupCover(ctx, album.MBID)
}

// errCoverBusy is what a release group answers for a minute after the archive
// could not be reached, so a page full of covers does not wait on a dead
// connection once per picture.
var errCoverBusy = errors.New("cover art: the archive could not be reached a moment ago")

// coverRetryAfter is how long a failed download is not tried again.
const coverRetryAfter = time.Minute

// cachedReleaseGroupCover is cachedCover for a release group id (an album in
// the library, or one shown on the Discover page). Requests for the same id
// share one download, and only a few downloads run at once.
func (s *Server) cachedReleaseGroupCover(ctx context.Context, mbid string) (string, error) {
	if !mbidPattern.MatchString(mbid) {
		return "", musicbrainz.ErrNotFound
	}
	mbid = strings.ToLower(mbid) // one file per id, whatever case it was asked in
	dir := s.coverCacheDir()
	img, none := filepath.Join(dir, mbid+".img"), filepath.Join(dir, mbid+".none")
	if st, err := os.Stat(img); err == nil && st.Size() > 0 {
		touchCover(img, st)
		return img, nil
	}
	if st, err := os.Stat(none); err == nil && time.Since(st.ModTime()) < coverMissingFor {
		return "", musicbrainz.ErrNotFound
	}
	if at, ok := s.music.covers.failed.Load(mbid); ok && time.Since(at.(time.Time)) < coverRetryAfter {
		return "", errCoverBusy
	}
	return s.music.covers.fetches.do(ctx, mbid, func() (string, error) {
		path, err := s.downloadCover(mbid, dir, img, none)
		if err != nil && !errors.Is(err, musicbrainz.ErrNotFound) {
			s.music.covers.failed.Store(mbid, time.Now())
		}
		return path, err
	})
}

// downloadCover fetches one release group's cover into the cache. It runs on
// its own, not for one request: the work is finished and kept even when the
// browser that asked has gone.
func (s *Server) downloadCover(mbid, dir, img, none string) (string, error) {
	if st, err := os.Stat(img); err == nil && st.Size() > 0 { // fetched meanwhile
		return img, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*coverFetchTimeout)
	defer cancel()
	if err := acquireCoverWork(ctx); err != nil { // a long queue of downloads
		return "", fmt.Errorf("cover art: waiting for a free slot: %w", err)
	}
	defer releaseCoverWork()
	ctx, cancelFetch := context.WithTimeout(ctx, coverFetchTimeout)
	defer cancelFetch()
	data, _, err := s.MusicBrainz().FrontCover(ctx, mbid)
	if errors.Is(err, musicbrainz.ErrNotFound) {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			_ = os.WriteFile(none, nil, 0o644)
			s.music.covers.noteWrite(dir)
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cover cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "cover-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cache cover: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache cover: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), img); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache cover: %w", err)
	}
	_ = os.Remove(none)
	s.music.covers.noteWrite(dir)
	return img, nil
}

// coverFor is the file to serve for an album: its own cover file if it has
// one, else the cached front cover from the Cover Art Archive.
func (s *Server) coverFor(ctx context.Context, album music.Album) (string, error) {
	if p, ok := s.localCover(album); ok {
		return p, nil
	}
	return s.cachedCover(ctx, album)
}

var warmingMusicCovers sync.Map // release group id -> struct{}, covers already being fetched

// warmMusicCovers fetches the covers of a Discover page in the background, six
// at a time, so the page finds them cached instead of waiting for the archive
// one by one. It skips what is cached or already on its way.
func (s *Server) warmMusicCovers(mbids []string) {
	var todo []string
	for _, id := range mbids {
		id = strings.ToLower(id)
		if !mbidPattern.MatchString(id) {
			continue
		}
		dir := s.coverCacheDir()
		if st, err := os.Stat(filepath.Join(dir, id+".img")); err == nil && st.Size() > 0 {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, id+".none")); err == nil && time.Since(st.ModTime()) < coverMissingFor {
			continue
		}
		if _, busy := warmingMusicCovers.LoadOrStore(id, struct{}{}); busy {
			continue
		}
		todo = append(todo, id)
	}
	if len(todo) == 0 {
		return
	}
	go func() {
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, id := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				defer warmingMusicCovers.Delete(id)
				_, _ = s.cachedReleaseGroupCover(context.Background(), id)
			}()
		}
		wg.Wait()
	}()
}

// coverMissing answers for a cover that is not there. Browsers keep the answer
// for an hour, so a grid full of albums without art does not ask again on every
// visit.
func coverMissing(w http.ResponseWriter, r *http.Request) { coverMissingKeep(w, r, time.Hour) }

// coverMissingKeep is coverMissing with how long the browser keeps the answer.
func coverMissingKeep(w http.ResponseWriter, r *http.Request, keep time.Duration) {
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(keep.Seconds())))
	http.NotFound(w, r)
}

// serveCover sends the picture at src, or its small thumbnail when the request
// says ?size=thumb (the grids use that). A thumbnail that cannot be made
// (a picture the decoder does not know) falls back to the picture itself.
func (s *Server) serveCover(w http.ResponseWriter, r *http.Request, src string) {
	if r.URL.Query().Get("size") == "thumb" {
		thumb, err := s.thumbFor(r.Context(), src)
		if err == nil {
			serveCoverFile(w, r, thumb, thumbMaxAge)
			return
		}
		if r.Context().Err() != nil {
			return
		}
		slog.Debug("music: cover thumbnail not made, sending the picture", "err", err)
	}
	serveCoverFile(w, r, src, coverMaxAge)
}

// serveCoverFile sends an image with the headers that let a browser keep it:
// a Cache-Control of maxAge, a Last-Modified and an ETag, so a browser that
// asks again gets a 304 with no body.
func serveCoverFile(w http.ResponseWriter, r *http.Request, path string, maxAge time.Duration) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	ct := http.DetectContentType(head[:n])
	if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" { // browsers show all three
		http.NotFound(w, r) // never send anything but a picture
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(maxAge.Seconds())))
	h.Set("ETag", fmt.Sprintf(`"%x-%x"`, st.Size(), st.ModTime().UnixNano()))
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleAlbumCover serves an album's cover image (any account): the cover
// file in the album's folder, else the front cover from the Cover Art
// Archive, fetched once and kept. ?size=thumb sends a small thumbnail. Answers
// a 404 when there is none (browsers keep that for a while).
func (s *Server) handleAlbumCover(w http.ResponseWriter, r *http.Request) {
	album, _, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	path, err := s.coverFor(r.Context(), album)
	if err != nil {
		coverUnavailable(w, r, err, "album", album.Title, libraryCoverKeep)
		return
	}
	s.serveCover(w, r, path)
}

// libraryCoverKeep is how long a browser keeps "no cover" for an album or
// artist in the library: shorter than for Discover, because an import can add
// a cover at any time.
const libraryCoverKeep = 30 * time.Minute

// coverUnavailable answers when no cover could be had. "The archive has none"
// is kept by the browser for a while; "could not be reached" only briefly.
func coverUnavailable(w http.ResponseWriter, r *http.Request, err error, what, name string, keep time.Duration) {
	if errors.Is(err, musicbrainz.ErrNotFound) {
		coverMissingKeep(w, r, keep)
		return
	}
	if r.Context().Err() == nil && !errors.Is(err, errCoverBusy) {
		slog.Info("music: cover art unavailable", what, name, "err", err)
	}
	coverMissingKeep(w, r, coverRetryAfter)
}

// handleReleaseGroupCover serves the front cover of any release group by its
// MusicBrainz id (any account), for the Discover page: fetched once from the
// archive and kept, so the browser never talks to an outside image host.
// An album already in the library is served from its own folder first.
func (s *Server) handleReleaseGroupCover(w http.ResponseWriter, r *http.Request) {
	mbid := r.PathValue("mbid")
	if !mbidPattern.MatchString(mbid) {
		http.NotFound(w, r)
		return
	}
	if albums, err := s.MusicRepo.AlbumsByMBID([]string{mbid}); err == nil {
		if album, ok := albums[mbid]; ok {
			if p, ok := s.localCover(album); ok {
				s.serveCover(w, r, p)
				return
			}
		}
	}
	path, err := s.cachedReleaseGroupCover(r.Context(), mbid)
	if err != nil {
		coverUnavailable(w, r, err, "releaseGroup", mbid, time.Hour)
		return
	}
	s.serveCover(w, r, path)
}

// artistCover is what the search for an artist's picture found: a file, or
// that there is none, or that the archive could not be reached.
type artistCover struct {
	path string
	err  error // musicbrainz.ErrNotFound: no picture; another error: not reachable now
	at   time.Time
}

const (
	artistCoverFoundFor   = 10 * time.Minute // a found picture is looked for again after this
	artistCoverMissingFor = 3 * time.Hour    // an artist without any picture is not searched again for this long
)

// artistCoverFor finds the picture of an artist's library entry: the cover of
// its first album that has one, studio albums before EPs and singles. The
// answer is remembered (for a long time when there is none), and requests that
// come together share one search. It returns an error for an artist that
// does not exist.
func (s *Server) artistCoverFor(ctx context.Context, id int64) (artistCover, error) {
	if v, ok := s.music.covers.memo.Load(id); ok {
		c := v.(artistCover)
		age := time.Since(c.at)
		switch {
		case c.err == nil && age < artistCoverFoundFor:
			if _, err := os.Stat(c.path); err == nil {
				return c, nil
			}
		case errors.Is(c.err, musicbrainz.ErrNotFound) && age < artistCoverMissingFor:
			return c, nil
		case c.err != nil && !errors.Is(c.err, musicbrainz.ErrNotFound) && age < coverRetryAfter:
			return c, nil
		}
	}
	return s.music.covers.artists.do(ctx, strconv.FormatInt(id, 10), func() (artistCover, error) {
		if _, err := s.MusicRepo.GetArtist(id); err != nil {
			return artistCover{}, err
		}
		albums, err := s.MusicRepo.ListAlbums(id)
		if err != nil {
			return artistCover{}, err
		}
		rank := func(a music.Album) int {
			switch a.Type {
			case music.TypeAlbum:
				return 0
			case music.TypeEP:
				return 1
			}
			return 2
		}
		sort.SliceStable(albums, func(i, j int) bool { return rank(albums[i]) < rank(albums[j]) })
		res := artistCover{err: musicbrainz.ErrNotFound, at: time.Now()}
		for i, album := range albums {
			if i >= 6 { // a few tries; the archive is not asked for a whole discography
				break
			}
			path, err := s.coverFor(context.Background(), album)
			if err == nil {
				res = artistCover{path: path, at: time.Now()}
				break
			}
			if !errors.Is(err, musicbrainz.ErrNotFound) {
				res.err = err // not reachable: do not call it "none" for hours
			}
		}
		s.music.covers.memo.Store(id, res)
		return res, nil
	})
}

// forgetArtistCover drops what is remembered about an artist's picture, for
// when its albums change (an import brought a cover).
func (s *Server) forgetArtistCover(artistID int64) { s.music.covers.memo.Delete(artistID) }

// handleArtistCover serves a picture for an artist (any account): the cover
// of its first album that has one, studio albums before EPs and singles.
// ?size=thumb sends a small thumbnail.
func (s *Server) handleArtistCover(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	res, err := s.artistCoverFor(r.Context(), id)
	if err != nil {
		http.NotFound(w, r) // no such artist (or the list could not be read)
		return
	}
	if res.err != nil {
		coverUnavailable(w, r, res.err, "artist", strconv.FormatInt(id, 10), libraryCoverKeep)
		return
	}
	s.serveCover(w, r, res.path)
}

// saveAlbumCover writes cover.jpg (or cover.png) into a freshly imported
// album folder when the folder has no cover file yet. An existing cover is
// never replaced. A failure is logged and does not fail the import.
func (s *Server) saveAlbumCover(ctx context.Context, album music.Album, folder string) {
	defer s.forgetArtistCover(album.ArtistID)
	for _, name := range coverLocalNames {
		if _, err := os.Stat(filepath.Join(folder, name)); err == nil {
			return
		}
	}
	cached, err := s.cachedCover(ctx, album)
	if err != nil {
		if !errors.Is(err, musicbrainz.ErrNotFound) {
			slog.Info("music: no cover art saved", "album", album.Title, "err", err)
		}
		return
	}
	data, err := os.ReadFile(cached)
	if err != nil {
		return
	}
	name := "cover.jpg"
	if strings.HasPrefix(http.DetectContentType(data), "image/png") {
		name = "cover.png"
	}
	// O_EXCL: if a cover appeared in the meantime, it stays.
	f, err := os.OpenFile(filepath.Join(folder, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return
	}
	if _, err := f.Write(data); err != nil {
		slog.Warn("music: write cover", "folder", folder, "err", err)
	}
	if err := f.Close(); err != nil {
		slog.Warn("music: write cover", "folder", folder, "err", err)
		return
	}
	_ = fileperms.ApplyFile(filepath.Join(folder, name))
}
