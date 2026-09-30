package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
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
var coverLocalNames = []string{"cover.jpg", "cover.jpeg", "cover.png", "folder.jpg", "folder.jpeg", "folder.png"}

// mbidPattern is what a MusicBrainz id looks like (a UUID); anything else is
// never used to build a file name.
var mbidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// maxCoverFiles is how many files (covers and "no cover" marks) the cache
// keeps. Any account can ask for the cover of any id, so without a limit the
// folder would grow with every made-up one.
const maxCoverFiles = 4000

// coverPruneEvery is how many new cache files are written between clean-ups.
const coverPruneEvery = 50

// coverLocks makes one download per release group at a time.
type coverLocks struct {
	mu     sync.Mutex
	m      map[string]*sync.Mutex
	writes atomic.Int64 // cache files written, to know when to prune
}

// noteWrite counts a new cache file and cleans the cache now and then.
func (c *coverLocks) noteWrite(dir string) {
	if c.writes.Add(1)%coverPruneEvery == 0 {
		pruneCoverCache(dir, maxCoverFiles, coverMissingFor)
	}
}

// pruneCoverCache removes "no cover" marks older than missingFor, then the
// oldest files until at most max remain.
func pruneCoverCache(dir string, max int, missingFor time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		mod  time.Time
	}
	var files []file
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
		files = append(files, file{filepath.Join(dir, name), info.ModTime()})
	}
	if len(files) <= max {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files[:len(files)-max] {
		_ = os.Remove(f.path)
	}
}

func (c *coverLocks) lock(key string) (unlock func()) {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*sync.Mutex{}
	}
	l := c.m[key]
	if l == nil {
		l = &sync.Mutex{}
		c.m[key] = l
	}
	c.mu.Unlock()
	l.Lock()
	return l.Unlock
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
// is one inside the music folder.
func (s *Server) localCover(album music.Album) (string, bool) {
	root := s.musicRoot()
	if album.Path == "" || root == "" {
		return "", false
	}
	for _, name := range coverLocalNames {
		real, err := cleanup.InsideRoot(root, filepath.Join(album.Path, name))
		if err != nil {
			continue
		}
		if st, err := os.Stat(real); err == nil && st.Mode().IsRegular() {
			return real, true
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

// cachedReleaseGroupCover is cachedCover for a release group id (an album in
// the library, or one shown on the Discover page).
func (s *Server) cachedReleaseGroupCover(ctx context.Context, mbid string) (string, error) {
	if !mbidPattern.MatchString(mbid) {
		return "", musicbrainz.ErrNotFound
	}
	mbid = strings.ToLower(mbid) // one file per id, whatever case it was asked in
	dir := s.coverCacheDir()
	img, none := filepath.Join(dir, mbid+".img"), filepath.Join(dir, mbid+".none")
	if st, err := os.Stat(img); err == nil && st.Size() > 0 {
		return img, nil
	}
	unlock := s.music.covers.lock(mbid)
	defer unlock()
	if st, err := os.Stat(img); err == nil && st.Size() > 0 { // fetched while we waited
		return img, nil
	}
	if st, err := os.Stat(none); err == nil && time.Since(st.ModTime()) < coverMissingFor {
		return "", musicbrainz.ErrNotFound
	}

	ctx, cancel := context.WithTimeout(ctx, coverFetchTimeout)
	defer cancel()
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

// serveCoverFile sends an image with the headers that let a browser keep it.
func serveCoverFile(w http.ResponseWriter, r *http.Request, path string) {
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
	if ct != "image/jpeg" && ct != "image/png" {
		http.NotFound(w, r) // never send anything but a picture
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(coverMaxAge.Seconds())))
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleAlbumCover serves an album's cover image (any account): the cover
// file in the album's folder, else the front cover from the Cover Art
// Archive, fetched once and kept. Answers a plain 404 when there is none.
func (s *Server) handleAlbumCover(w http.ResponseWriter, r *http.Request) {
	album, _, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	path, err := s.coverFor(r.Context(), album)
	if err != nil {
		if !errors.Is(err, musicbrainz.ErrNotFound) {
			slog.Info("music: cover art unavailable", "album", album.Title, "err", err)
		}
		http.NotFound(w, r)
		return
	}
	serveCoverFile(w, r, path)
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
				serveCoverFile(w, r, p)
				return
			}
		}
	}
	path, err := s.cachedReleaseGroupCover(r.Context(), mbid)
	if err != nil {
		if !errors.Is(err, musicbrainz.ErrNotFound) {
			slog.Info("music: cover art unavailable", "releaseGroup", mbid, "err", err)
		}
		http.NotFound(w, r)
		return
	}
	serveCoverFile(w, r, path)
}

// handleArtistCover serves a picture for an artist (any account): the cover
// of its first album that has one, studio albums before EPs and singles.
func (s *Server) handleArtistCover(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	if _, err := s.MusicRepo.GetArtist(id); err != nil {
		http.NotFound(w, r)
		return
	}
	albums, err := s.MusicRepo.ListAlbums(id)
	if err != nil {
		http.NotFound(w, r)
		return
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
	for i, album := range albums {
		if i >= 6 { // a few tries; the archive is not asked for a whole discography
			break
		}
		if path, err := s.coverFor(r.Context(), album); err == nil {
			serveCoverFile(w, r, path)
			return
		}
	}
	http.NotFound(w, r)
}

// saveAlbumCover writes cover.jpg (or cover.png) into a freshly imported
// album folder when the folder has no cover file yet. An existing cover is
// never replaced. A failure is logged and does not fail the import.
func (s *Server) saveAlbumCover(ctx context.Context, album music.Album, folder string) {
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
	}
}
