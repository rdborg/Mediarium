package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
)

// Importing an existing music collection: the music folder is read as
// <Artist>/<Album>/tracks, every artist and album is looked up on
// MusicBrainz by name (and year), and what matches is registered where it
// is, without moving, renaming or changing a file. What does not match is
// reported. It runs as a background job the interface polls.

const (
	musicScanScanning = "scanning"
	musicScanMatching = "matching"
	musicScanDone     = "done"
	musicScanFailed   = "failed"
	musicScanStopped  = "stopped" // cancelled by hand

	maxMusicScanJobs = 5

	// musicScanStale is how long a scan may go without any progress before a
	// new scan replaces it. Reading one file's tags is capped at a few
	// seconds and every lookup has its own timeout, so a scan that hasn't
	// moved for this long is stuck.
	musicScanStale = 10 * time.Minute

	// Result statuses.
	musicResultImported  = "imported"  // registered in the library
	musicResultAlready   = "already"   // the album was already in the library with its files
	musicResultUnmatched = "unmatched" // not found on MusicBrainz, or not among the artist's albums
	musicResultFailed    = "failed"    // found, but registering it failed
)

// musicScanResult is one album folder (or loose files) and what became of it.
type musicScanResult struct {
	Artist   string `json:"artist"`
	Album    string `json:"album,omitempty"` // empty for loose files in an artist folder
	Folder   string `json:"folder"`
	Files    int    `json:"files"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	ArtistID int64  `json:"artistId,omitempty"`
	AlbumID  int64  `json:"albumId,omitempty"`
	Quality  string `json:"quality,omitempty"`
	Tracks   int    `json:"tracks,omitempty"` // tracks matched to a file
	// Note is shown with any result: here, files whose tags couldn't be read.
	Note string `json:"note,omitempty"`
}

type musicScanSummary struct {
	Artists   int `json:"artists"`
	Albums    int `json:"albums"`
	Imported  int `json:"imported"`
	Already   int `json:"already"`
	Unmatched int `json:"unmatched"`
	Failed    int `json:"failed"`
}

type musicScanJob struct {
	mu      sync.Mutex
	id      string
	root    string
	phase   string
	done    int    // albums handled
	total   int    // albums found
	current string // the folder being read or looked up
	results []musicScanResult
	err     string
	created time.Time
	moved   time.Time // the last time the scan made progress
	ctx     context.Context
	cancel  context.CancelFunc
}

type musicScanPayload struct {
	ID      string            `json:"id"`
	Root    string            `json:"root"`
	Phase   string            `json:"phase"` // scanning, matching, done or failed
	Done    int               `json:"done"`
	Total   int               `json:"total"`
	Summary musicScanSummary  `json:"summary"`
	Current string            `json:"current,omitempty"` // the folder being read or looked up
	Results []musicScanResult `json:"results"`
	Error   string            `json:"error,omitempty"`
}

func (j *musicScanJob) snapshot() musicScanPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	p := musicScanPayload{ID: j.id, Root: j.root, Phase: j.phase, Done: j.done, Total: j.total, Current: j.current, Error: j.err,
		Results: append([]musicScanResult{}, j.results...)}
	artists := map[string]bool{}
	for _, r := range j.results {
		artists[r.Artist] = true
		if r.Album != "" {
			p.Summary.Albums++
		}
		switch r.Status {
		case musicResultImported:
			p.Summary.Imported++
		case musicResultAlready:
			p.Summary.Already++
		case musicResultUnmatched:
			p.Summary.Unmatched++
		case musicResultFailed:
			p.Summary.Failed++
		}
	}
	p.Summary.Artists = len(artists)
	return p
}

func (j *musicScanJob) add(r musicScanResult) {
	j.mu.Lock()
	j.results = append(j.results, r)
	if r.Album != "" {
		j.done++
	}
	j.moved = time.Now()
	j.mu.Unlock()
}

// working notes the folder being worked on, which is progress too.
func (j *musicScanJob) working(folder string) {
	j.mu.Lock()
	j.current, j.moved = folder, time.Now()
	j.mu.Unlock()
}

// stop ends a running scan with the given phase and reason. It reports
// whether the scan was still running.
func (j *musicScanJob) stop(phase, reason string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.phase != musicScanScanning && j.phase != musicScanMatching {
		return false
	}
	j.phase, j.err, j.current = phase, reason, ""
	if j.cancel != nil {
		j.cancel()
	}
	return true
}

func (j *musicScanJob) setPhase(phase string, total int) {
	j.mu.Lock()
	if j.phase == musicScanStopped || j.phase == musicScanFailed {
		j.mu.Unlock()
		return // stopped by hand or replaced: it stays that way
	}
	j.phase, j.moved = phase, time.Now()
	if phase == musicScanDone {
		j.current = ""
	}
	if total >= 0 {
		j.total = total
	}
	j.mu.Unlock()
}

func (j *musicScanJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.phase == musicScanScanning || j.phase == musicScanMatching
}

// musicScans holds the recent scan jobs.
type musicScans struct {
	mu    sync.Mutex
	jobs  map[string]*musicScanJob
	order []string
}

func (m *musicScans) get(id string) *musicScanJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

// latest is the most recent job, nil when there is none.
func (m *musicScans) latest() *musicScanJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return nil
	}
	return m.jobs[m.order[len(m.order)-1]]
}

// start registers a new job, or returns the one still running. A running
// job that hasn't made progress for musicScanStale is stopped and replaced,
// so a stuck scan never blocks new ones.
func (m *musicScans) start(root string) (job *musicScanJob, created bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		j := m.jobs[id]
		if !j.running() {
			continue
		}
		j.mu.Lock()
		stale := time.Since(j.moved) > musicScanStale
		j.mu.Unlock()
		if !stale {
			return j, false
		}
		j.stop(musicScanFailed, "it made no progress for 10 minutes")
		slog.Warn("music: replaced a scan that made no progress", "id", j.id, "root", j.root)
	}
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	now := time.Now()
	job = &musicScanJob{id: hex.EncodeToString(buf), root: root, phase: musicScanScanning, created: now, moved: now}
	job.ctx, job.cancel = context.WithCancel(context.Background())
	if m.jobs == nil {
		m.jobs = map[string]*musicScanJob{}
	}
	m.jobs[job.id] = job
	m.order = append(m.order, job.id)
	for len(m.order) > maxMusicScanJobs {
		delete(m.jobs, m.order[0])
		m.order = m.order[1:]
	}
	return job, true
}

// handleMusicImportScan starts reading the music folder and registering
// what MusicBrainz recognises (administrators). Files are never moved or
// changed. Answers 202 with {id}; poll GET /api/music/import/scan/{id}. A
// scan already running is returned instead of starting another (200).
func (s *Server) handleMusicImportScan(w http.ResponseWriter, r *http.Request) {
	root := s.musicRoot()
	if root == "" {
		writeError(w, http.StatusPreconditionFailed, "The music folder isn't set. Choose one in Settings > Library > Folders.")
		return
	}
	job, created := s.music.scans.start(root)
	if !created {
		writeJSON(w, http.StatusOK, map[string]string{"id": job.id})
		return
	}
	userID, _ := requester(r)
	s.background(func() { s.runMusicScan(job, userID) })
	writeJSON(w, http.StatusAccepted, map[string]string{"id": job.id})
}

// handleMusicImportLatest returns the most recent scan job, running or
// finished, so the page shows it again when it is reopened (administrators).
// Answers {"id": ""} when no scan has run since the server started.
func (s *Server) handleMusicImportLatest(w http.ResponseWriter, r *http.Request) {
	job := s.music.scans.latest()
	if job == nil {
		writeJSON(w, http.StatusOK, map[string]string{"id": ""})
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

// handleMusicImportCancel stops a running scan (administrators). What was
// registered so far stays in the library.
func (s *Server) handleMusicImportCancel(w http.ResponseWriter, r *http.Request) {
	job := s.music.scans.get(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, "That scan isn't available any more.")
		return
	}
	if job.stop(musicScanStopped, "") {
		slog.Info("music: scan stopped by hand", "id", job.id, "root", job.root)
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

// handleMusicImportResults returns a scan job: its phase, progress, the
// summary and one result per album folder found (administrators).
func (s *Server) handleMusicImportResults(w http.ResponseWriter, r *http.Request) {
	job := s.music.scans.get(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, "That scan isn't available any more.")
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

func (s *Server) runMusicScan(job *musicScanJob, userID int64) {
	ctx := job.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if job.cancel != nil {
			job.cancel()
		}
	}()
	slog.Info("music: library scan started", "root", job.root)
	artists, err := music.ScanLibrary(job.root)
	if err != nil {
		job.stop(musicScanFailed, err.Error())
		slog.Warn("music: scan music folder", "root", job.root, "err", err)
		return
	}
	total := 0
	for _, a := range artists {
		total += len(a.Albums)
	}
	job.setPhase(musicScanMatching, total)
	slog.Info("music: library scan read the folder", "root", job.root, "artists", len(artists), "albums", total)
	cache := map[string]*scanArtist{}
	for _, a := range artists {
		if ctx.Err() != nil {
			slog.Info("music: library scan stopped", "root", job.root, "albums_done", job.snapshot().Done, "albums", total)
			return
		}
		s.matchScannedArtist(ctx, job, a, userID, cache)
		slog.Info("music: library scan progress", "artist", a.Name, "albums_done", job.snapshot().Done, "albums", total)
	}
	job.setPhase(musicScanDone, -1)
	slog.Info("music: library scan finished", "root", job.root, "artists", len(artists), "albums", total)
}

// skippedTags is a TagReader that counts the files whose tags took too long
// to read (they are then matched by their names alone).
type skippedTags struct {
	inner   music.TagReader
	skipped int
}

func (r *skippedTags) ReadTags(path string) (music.FileInfo, bool, error) {
	info, ok, err := r.inner.ReadTags(path)
	if errors.Is(err, music.ErrTagReadLimit) {
		r.skipped++
		slog.Warn("music scan: gave up reading a file's tags, it may be damaged", "file", path)
	}
	return info, ok, err
}

// skippedTagsNote is the note for an album with files whose tags couldn't
// be read in time.
func skippedTagsNote(n int) string {
	if n == 1 {
		return "The tags of 1 file couldn't be read in time, so it was matched by its name. It may be damaged."
	}
	return fmt.Sprintf("The tags of %d files couldn't be read in time, so they were matched by their names. They may be damaged.", n)
}

// scanArtist is an artist resolved on MusicBrainz (and added to the
// library) during a scan, with the albums it has, kept for the whole scan
// so a name is looked up once.
type scanArtist struct {
	artist music.Artist
	albums []music.Album
	cands  []music.AlbumCandidate
	err    string // why the artist could not be resolved
}

// scanNames is the ordered, distinct list of names to try for an artist:
// the tags first, then the folder.
func scanNames(names ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		if k := music.NormalizeName(n); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	return out
}

// resolveScanArtist looks a name up on MusicBrainz and returns the artist
// in the library (adding it, with none of its albums monitored, when it is
// new). Results are cached for the scan by name.
func (s *Server) resolveScanArtist(ctx context.Context, cache map[string]*scanArtist, name string, userID int64, hints []string) *scanArtist {
	key := music.NormalizeName(name)
	if sa, ok := cache[key]; ok {
		return sa
	}
	sa := &scanArtist{}
	cache[key] = sa
	found, err := s.MusicBrainz().SearchArtists(ctx, name, 10)
	if err != nil {
		slog.Warn("music: scan could not look up an artist", "artist", name, "err", err)
		sa.err = "The music database couldn't be reached. Scan again in a moment."
		return sa
	}
	cands := make([]music.NameCandidate, len(found))
	for i, f := range found {
		cands[i] = music.NameCandidate{Name: f.Name, SortName: f.SortName, Score: f.Score}
	}
	// Two artists can have the same name: the one that has the albums found
	// on disk is the right one, whichever the search ranks first.
	idx := music.BestArtistByAlbums(name, cands, hints, func(i int) []string { return s.albumTitlesOf(ctx, found[i].ID) })
	if idx < 0 {
		sa.err = "no artist called " + name + " in the music database"
		return sa
	}
	mb := found[idx]
	artist, ok, err := s.MusicRepo.GetArtistByMBID(mb.ID)
	if err == nil && !ok {
		// New to the library: listed with its albums, none monitored, so
		// finding your collection never starts a wave of downloads.
		artist, _, err = s.addArtistFromMusicBrainz(ctx, mb.ID, monitorNone, 0, userID)
		if err == nil {
			_ = s.MusicRepo.SetArtistMonitored(artist.ID, true)
			artist.Monitored = true
		}
	}
	if err != nil {
		sa.err = "couldn't add " + mb.Name + ": " + err.Error()
		return sa
	}
	albums, err := s.MusicRepo.ListAlbums(artist.ID)
	if err != nil {
		sa.err = err.Error()
		return sa
	}
	sa.artist, sa.albums = artist, albums
	for _, a := range albums {
		sa.cands = append(sa.cands, music.AlbumCandidate{Title: a.Title, Year: a.Year(), Type: a.Type})
	}
	return sa
}

// albumTitlesOf lists the album names of a MusicBrainz artist: from the
// library when the artist is already in it, otherwise from the music
// database. An artist that can't be listed has none.
func (s *Server) albumTitlesOf(ctx context.Context, mbid string) []string {
	if artist, ok, err := s.MusicRepo.GetArtistByMBID(mbid); err == nil && ok {
		albums, err := s.MusicRepo.ListAlbums(artist.ID)
		if err != nil {
			return nil
		}
		titles := make([]string, len(albums))
		for i, a := range albums {
			titles[i] = a.Title
		}
		return titles
	}
	groups, err := s.MusicBrainz().ArtistReleaseGroups(ctx, mbid, nil, true)
	if err != nil {
		return nil
	}
	titles := make([]string, len(groups))
	for i, g := range groups {
		titles[i] = g.Title
	}
	return titles
}

// matchScannedArtist registers the album folders of one artist folder. The
// tags of an album's files decide who and what it is (the artist, album and
// year most of the files agree on); the folder names supply what the tags
// lack, and are a second try when the tags find nothing on MusicBrainz.
func (s *Server) matchScannedArtist(ctx context.Context, job *musicScanJob, folder music.ScannedArtist, userID int64, cache map[string]*scanArtist) {
	if len(folder.Loose) > 0 {
		job.add(musicScanResult{Artist: folder.Name, Folder: folder.Path, Files: len(folder.Loose), Status: musicResultUnmatched,
			Message: "Audio files directly in the artist folder can't be matched. Put them in an album folder."})
	}
	// The album names found on disk (from the tags, and from the folder
	// names) help to tell apart two artists that share a name.
	scanned := make([][]music.FileInfo, len(folder.Albums))
	skipped := make([]int, len(folder.Albums))
	var hints []string
	for n, al := range folder.Albums {
		job.working(al.Path)
		infos := make([]music.FileInfo, len(al.Files))
		reader := &skippedTags{inner: musicTagReader}
		for i, f := range al.Files {
			if ctx.Err() != nil {
				return
			}
			infos[i] = music.IdentifyFile(f, reader)
		}
		scanned[n], skipped[n] = infos, reader.skipped
		hints = append(hints, al.Title)
		if t := music.ConsensusOf(infos).Album; t != "" {
			hints = append(hints, t)
		}
	}
	for n, al := range folder.Albums {
		if ctx.Err() != nil {
			return
		}
		job.working(al.Path)
		infos := scanned[n]
		tags := music.ConsensusOf(infos)
		res := musicScanResult{Artist: folder.Name, Album: al.Title, Folder: al.Path, Files: len(al.Files)}
		if skipped[n] > 0 {
			res.Note = skippedTagsNote(skipped[n])
		}

		var sa *scanArtist
		var errs []string
		for _, name := range scanNames(tags.Artist, folder.Name) {
			cand := s.resolveScanArtist(ctx, cache, name, userID, hints)
			if cand.err == "" {
				sa = cand
				break
			}
			errs = append(errs, cand.err)
		}
		if ctx.Err() != nil {
			return // stopped: a lookup cut short says nothing about the album
		}
		if sa == nil {
			res.Status, res.Message = musicResultUnmatched, errs[0]
			job.add(res)
			continue
		}
		res.Artist, res.ArtistID = sa.artist.Name, sa.artist.ID

		i := -1
		if tags.Album != "" {
			y := tags.Year
			if y == 0 {
				y = al.Year
			}
			i = music.BestAlbumForFiles(tags.Album, y, len(al.Files), sa.cands)
		}
		if i < 0 {
			i = music.BestAlbumForFiles(al.Title, al.Year, len(al.Files), sa.cands)
		}
		if i < 0 {
			res.Status = musicResultUnmatched
			res.Message = fmt.Sprintf("no album called %q among the albums, EPs and singles of %s in the music database", firstNonEmpty(tags.Album, al.Title), sa.artist.Name)
			job.add(res)
			continue
		}
		album := sa.albums[i]
		res.AlbumID, res.Album = album.ID, album.Title
		s.registerScannedAlbum(ctx, &res, album, al, infos)
		job.add(res)
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// registerScannedAlbum marks an album downloaded where its files already
// are, links each matched track to its file and records the quality read
// from the files' headers.
func (s *Server) registerScannedAlbum(ctx context.Context, res *musicScanResult, album music.Album, found music.ScannedAlbum, infos []music.FileInfo) {
	if album.Status == music.StatusDownloaded && album.Path != "" {
		res.Status, res.Quality, res.Message = musicResultAlready, album.Quality, "already in your library"
		return
	}
	if album.Status == music.StatusDownloading {
		res.Status, res.Message = musicResultFailed, "the album is downloading right now. Scan again when it has finished"
		return
	}
	tracks, err := s.ensureTracks(ctx, album)
	if err != nil {
		res.Status, res.Message = musicResultFailed, err.Error()
		return
	}
	slots := make([]music.TrackSlot, len(tracks))
	for i, t := range tracks {
		slots[i] = music.TrackSlot{Disc: t.Disc, Position: t.Position, Title: t.Title}
	}
	matches, unmatched := music.MatchTracks(infos, slots)
	for _, m := range matches {
		if err := s.MusicRepo.SetTrackFile(tracks[m.Slot].ID, found.Files[m.File]); err != nil {
			res.Status, res.Message = musicResultFailed, err.Error()
			return
		}
	}
	// The real quality, from the files' headers. Files whose headers cannot
	// be read leave the album Unknown, which automation never upgrades.
	tier := music.AlbumTier(found.Files)
	s.releaseWronglyLinked(album, found.Path)
	if err := s.MusicRepo.SetAlbumImported(album.ID, tier, found.Path); err != nil {
		res.Status, res.Message = musicResultFailed, err.Error()
		return
	}
	_ = s.MusicRepo.SetAlbumMonitored(album.ID, true)
	res.Status, res.Quality, res.Tracks = musicResultImported, string(tier), len(matches)
	res.Message = fmt.Sprintf("%d of %d tracks matched", len(matches), len(tracks))
	if len(unmatched) > 0 {
		res.Message += fmt.Sprintf(", %s fit no track", plural(len(unmatched), "file"))
	}
	s.albumEvent(album.ID, "imported", queue.LevelInfo, fmt.Sprintf("Found in your music folder at %s: %s (%s)", found.Path, res.Message, tier))
}

// releaseWronglyLinked frees a folder that an earlier scan linked to a smaller
// release of the same artist (a single that shares the album's name and year,
// which older versions picked first): the single goes back to wanted, so the
// folder can belong to the album that it really is.
func (s *Server) releaseWronglyLinked(album music.Album, path string) {
	if path == "" {
		return
	}
	others, err := s.MusicRepo.ListAlbums(album.ArtistID)
	if err != nil {
		return
	}
	for _, o := range others {
		if o.ID == album.ID || o.Path != path || music.ReleaseTypeRank(o.Type) >= music.ReleaseTypeRank(album.Type) {
			continue
		}
		if tracks, err := s.MusicRepo.ListTracks(o.ID); err == nil {
			for _, t := range tracks {
				if t.FilePath != "" {
					_ = s.MusicRepo.SetTrackFile(t.ID, "")
				}
			}
		}
		_ = s.MusicRepo.SetAlbumMissing(o.ID)
	}
}
