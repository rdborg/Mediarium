package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	maxMusicScanJobs = 5

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
	done    int // albums handled
	total   int // albums found
	results []musicScanResult
	err     string
	created time.Time
}

type musicScanPayload struct {
	ID      string            `json:"id"`
	Root    string            `json:"root"`
	Phase   string            `json:"phase"` // scanning, matching, done or failed
	Done    int               `json:"done"`
	Total   int               `json:"total"`
	Summary musicScanSummary  `json:"summary"`
	Results []musicScanResult `json:"results"`
	Error   string            `json:"error,omitempty"`
}

func (j *musicScanJob) snapshot() musicScanPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	p := musicScanPayload{ID: j.id, Root: j.root, Phase: j.phase, Done: j.done, Total: j.total, Error: j.err,
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
	j.mu.Unlock()
}

func (j *musicScanJob) setPhase(phase string, total int) {
	j.mu.Lock()
	j.phase = phase
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

// start registers a new job, or returns the one still running.
func (m *musicScans) start(root string) (job *musicScanJob, created bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		if j := m.jobs[id]; j.running() {
			return j, false
		}
	}
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	job = &musicScanJob{id: hex.EncodeToString(buf), root: root, phase: musicScanScanning, created: time.Now()}
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
		writeError(w, http.StatusPreconditionFailed, "The music folder isn't set. Choose one in Settings > Library > Folders and file names.")
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
	ctx := context.Background()
	artists, err := music.ScanLibrary(job.root)
	if err != nil {
		job.mu.Lock()
		job.phase, job.err = musicScanFailed, err.Error()
		job.mu.Unlock()
		slog.Warn("music: scan music folder", "root", job.root, "err", err)
		return
	}
	total := 0
	for _, a := range artists {
		total += len(a.Albums)
	}
	job.setPhase(musicScanMatching, total)
	cache := map[string]*scanArtist{}
	for _, a := range artists {
		s.matchScannedArtist(ctx, job, a, userID, cache)
	}
	job.setPhase(musicScanDone, -1)
	slog.Info("music: library scan finished", "root", job.root, "artists", len(artists), "albums", total)
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
func (s *Server) resolveScanArtist(ctx context.Context, cache map[string]*scanArtist, name string, userID int64) *scanArtist {
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
	idx := music.BestArtist(name, cands)
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
		sa.cands = append(sa.cands, music.AlbumCandidate{Title: a.Title, Year: a.Year()})
	}
	return sa
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
	for _, al := range folder.Albums {
		infos := make([]music.FileInfo, len(al.Files))
		for i, f := range al.Files {
			infos[i] = music.IdentifyFile(f, musicTagReader)
		}
		tags := music.ConsensusOf(infos)
		res := musicScanResult{Artist: folder.Name, Album: al.Title, Folder: al.Path, Files: len(al.Files)}

		var sa *scanArtist
		var errs []string
		for _, name := range scanNames(tags.Artist, folder.Name) {
			cand := s.resolveScanArtist(ctx, cache, name, userID)
			if cand.err == "" {
				sa = cand
				break
			}
			errs = append(errs, cand.err)
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
			i = music.BestAlbum(tags.Album, y, sa.cands)
		}
		if i < 0 {
			i = music.BestAlbum(al.Title, al.Year, sa.cands)
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
