package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// Confirming a reviewed import only writes down the titles, with what the
// scan and the match already know, in one database transaction. It makes no
// request to the movie database, so it answers at once. The details follow
// in the background (import_worker.go), and the banner and the report read
// the progress from the import's own record, which survives a restart.

type importSelection struct {
	Key    string `json:"key"`
	TMDBID int    `json:"tmdbId"`
	// Title and Year name a match picked by searching by hand; matches from
	// the scan are known to the server already.
	Title string `json:"title,omitempty"`
	Year  int    `json:"year,omitempty"`
}

type importRequest struct {
	JobID      string            `json:"jobId"`
	Selections []importSelection `json:"selections"`
	// The titles are added safe: not monitored, and left out of the search
	// for better versions, so an import never downloads anything by itself.
	// Monitor watches them for new episodes and better versions. NoUpgrade
	// leaves them out of the search for better versions (it follows Monitor
	// when left out). MonitorMissing makes the episodes a show is missing
	// wanted. All three are off when left out.
	Monitor        *bool `json:"monitor,omitempty"`
	NoUpgrade      *bool `json:"noUpgrade,omitempty"`
	MonitorMissing *bool `json:"monitorMissing,omitempty"`
}

// importOptions turns what the request chose into the options of the import.
func (req importRequest) options(kind libimport.Kind, root string) library.ImportOptions {
	monitor := boolOr(req.Monitor, false)
	if req.Monitor == nil && req.NoUpgrade != nil && !*req.NoUpgrade {
		monitor = true // an older client that only asks for better versions
	}
	return library.ImportOptions{
		Kind: string(kind), Root: root,
		Monitor:        monitor,
		NoUpgrade:      boolOr(req.NoUpgrade, !monitor),
		MonitorMissing: kind == libimport.KindTV && boolOr(req.MonitorMissing, false),
	}
}

func kindLabel(kind libimport.Kind) string {
	if kind == libimport.KindTV {
		return "TV show"
	}
	return "movie"
}

// importRunning explains why a new import of kind cannot start, or returns
// an empty string when it can. Two imports of the same kind would fight over
// the same titles; one of each kind is fine.
func (s *Server) importRunning(kind libimport.Kind, includeScans bool) string {
	busy := false
	if includeScans {
		s.importMu.Lock()
		for _, j := range s.importJobs {
			j.mu.Lock()
			if j.kind == kind && (j.phase == importScanning || j.phase == importMatching || j.phase == importImporting) {
				busy = true
			}
			j.mu.Unlock()
		}
		s.importMu.Unlock()
	}
	if !busy {
		kinds, err := s.MovieRepo.RunningImportKinds()
		if err != nil {
			log.Printf("import: check running imports: %v", err)
		}
		busy = kinds[string(kind)]
	}
	if !busy {
		return ""
	}
	return fmt.Sprintf("A %s import is already running. It carries on in the background, so you can keep using Mediarium. Start another one when it has finished.", kindLabel(kind))
}

func boolOr(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// handleRunImport registers the reviewed selections. Files are looked up
// from the server's own scan result by key, never taken from the request, so
// the endpoint can only register files a scan actually found.
func (s *Server) handleRunImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := decodeJSON(r, &req); err != nil || req.JobID == "" {
		writeError(w, http.StatusBadRequest, "jobId and selections are required")
		return
	}
	job, ok := s.findImportJob(req.JobID)
	if !ok {
		writeError(w, http.StatusNotFound, "That scan isn't available any more. A newer scan may have replaced it.")
		return
	}
	if len(req.Selections) == 0 {
		writeError(w, http.StatusBadRequest, "Choose at least one title to import.")
		return
	}

	s.importRegisterMu.Lock()
	defer s.importRegisterMu.Unlock()
	if msg := s.importRunning(job.kind, false); msg != "" {
		writeError(w, http.StatusConflict, msg)
		return
	}

	job.mu.Lock()
	if job.phase != importReady {
		phase := job.phase
		job.mu.Unlock()
		writeError(w, http.StatusConflict, "This scan isn't ready to import yet (it's "+phase+").")
		return
	}
	byKey := map[string]int{}
	for i, g := range job.groups {
		byKey[g.Key] = i
	}
	var entries []library.ImportEntry
	seen := map[string]bool{}
	for _, sel := range req.Selections {
		idx, found := byKey[sel.Key]
		if !found || sel.TMDBID <= 0 {
			job.mu.Unlock()
			writeError(w, http.StatusBadRequest, "Some of the selected items are unknown or have no match. Scan again and try once more.")
			return
		}
		if seen[sel.Key] {
			continue
		}
		seen[sel.Key] = true
		entries = append(entries, buildImportEntry(job.kind, job.groups[idx], job.items[idx], sel))
	}
	job.phase, job.done, job.total = importImporting, 0, len(entries)
	kind, root := job.kind, job.root
	job.mu.Unlock()

	opts := req.options(kind, root)
	batchID, _, err := s.MovieRepo.RegisterImport(opts, entries)
	if err != nil {
		log.Printf("import: register %d titles: %v", len(entries), err)
		job.setPhase(importReady, 0, len(job.items))
		writeError(w, http.StatusInternalServerError, "Couldn't save your import, so nothing was added. Try again.")
		return
	}
	job.mu.Lock()
	job.phase, job.done, job.batchID = importDone, len(entries), batchID
	job.mu.Unlock()

	s.kickImportWorker()
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": job.id, "batchId": batchID})
}

// buildImportEntry turns a reviewed group and the chosen match into what
// gets registered: only what is already known, no lookups.
func buildImportEntry(kind libimport.Kind, g libimport.Group, item importItemPayload, sel importSelection) library.ImportEntry {
	e := library.ImportEntry{TMDBID: sel.TMDBID, Title: g.Title, Year: g.Year}
	found := false
	for _, c := range item.Candidates {
		if c.TMDBID == sel.TMDBID {
			e.Title, e.Year, e.PosterPath, found = c.Title, c.Year, c.PosterPath, true
			break
		}
	}
	if title := strings.TrimSpace(sel.Title); !found && title != "" && len(title) <= 300 {
		e.Title, e.Year = title, sel.Year
	}

	if kind == libimport.KindMovie {
		e.Kind = "movie"
		main := g.Files[0]
		for _, f := range g.Files[1:] {
			if f.SizeBytes > main.SizeBytes {
				main = f
			}
		}
		e.FilePath, e.Quality, e.AddedAt = main.Path, main.Quality, main.ModTime
		return e
	}
	e.Kind = "series"
	for _, f := range g.Files {
		e.Files = append(e.Files, library.ImportFile{Path: f.Path, Quality: f.Quality, Season: f.Season, Episodes: f.Episodes})
		if f.ModTime.After(e.AddedAt) {
			e.AddedAt = f.ModTime
		}
	}
	return e
}

// ---- progress: the banner, the import page and the report ----

type importActiveJob struct {
	ID    string         `json:"id"`
	Kind  libimport.Kind `json:"kind"`
	Root  string         `json:"root"`
	Phase string         `json:"phase"`
	Done  int            `json:"done"`
	Total int            `json:"total"`
}

type importBatchPayload struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"` // movie|tv
	Root           string `json:"root"`
	CreatedAt      string `json:"createdAt"`
	FinishedAt     string `json:"finishedAt,omitempty"`
	ElapsedMs      int64  `json:"elapsedMs"` // how long it has run, by the server's clock
	Running        bool   `json:"running"`
	Dismissed      bool   `json:"dismissed"`
	Monitor        bool   `json:"monitor"`
	NoUpgrade      bool   `json:"noUpgrade"`
	MonitorMissing bool   `json:"monitorMissing"`
	Total          int    `json:"total"`    // titles being added
	Done           int    `json:"done"`     // of those, details finished (or given up on for now)
	Added          int    `json:"added"`    // titles added
	Already        int    `json:"already"`  // titles that were in the library
	Problems       int    `json:"problems"` // titles with a problem
}

func toImportBatchPayload(b library.ImportBatch) importBatchPayload {
	return importBatchPayload{
		ID: b.ID, Kind: b.Kind, Root: b.Root, CreatedAt: b.CreatedAt, FinishedAt: b.FinishedAt, ElapsedMs: b.Elapsed.Milliseconds(), Running: b.Running(),
		Dismissed: b.Dismissed, Monitor: b.Monitor, NoUpgrade: b.NoUpgrade, MonitorMissing: b.MonitorMissing,
		Total: b.Added, Done: b.Added - b.Pending, Added: b.Added, Already: b.Already, Problems: b.Problems,
	}
}

type importBatchItemPayload struct {
	Title    string `json:"title"`
	Kind     string `json:"kind"`    // movie|series
	Outcome  string `json:"outcome"` // added|already|failed
	State    string `json:"state"`   // pending|done|problem
	Note     string `json:"note,omitempty"`
	Imported int    `json:"imported"` // shows: episodes marked downloaded
	Skipped  int    `json:"skipped"`  // shows: episodes that already were
}

type importBatchDetailPayload struct {
	importBatchPayload
	Items []importBatchItemPayload `json:"items"`
}

type importActivePayload struct {
	Jobs    []importActiveJob    `json:"jobs"`
	Batches []importBatchPayload `json:"batches"`
}

// handleActiveImports lists what is going on: scans waiting to be reviewed or
// still running, imports still filling in details, and finished imports whose
// banner has not been dismissed. The banner and the import page use it to pick
// up where they left off after the person left the page.
func (s *Server) handleActiveImports(w http.ResponseWriter, r *http.Request) {
	out := importActivePayload{Jobs: []importActiveJob{}, Batches: []importBatchPayload{}}

	newest := map[libimport.Kind]*importJob{}
	s.importMu.Lock()
	for _, j := range s.importJobs {
		j.mu.Lock()
		live := j.phase == importScanning || j.phase == importMatching || j.phase == importReady || j.phase == importImporting
		j.mu.Unlock()
		if live && (newest[j.kind] == nil || j.created.After(newest[j.kind].created)) {
			newest[j.kind] = j
		}
	}
	s.importMu.Unlock()
	for _, j := range newest {
		j.mu.Lock()
		out.Jobs = append(out.Jobs, importActiveJob{ID: j.id, Kind: j.kind, Root: j.root, Phase: j.phase, Done: j.done, Total: j.total})
		j.mu.Unlock()
	}
	sort.Slice(out.Jobs, func(a, b int) bool { return out.Jobs[a].Kind < out.Jobs[b].Kind })

	batches, err := s.MovieRepo.ActiveImportBatches()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the import progress. Reload the page and try again.")
		log.Printf("import: list active batches: %v", err)
		return
	}
	for _, b := range batches {
		out.Batches = append(out.Batches, toImportBatchPayload(b))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetImportBatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid import.")
		return
	}
	b, err := s.MovieRepo.ImportBatchByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That import isn't there any more.")
		return
	}
	if err != nil {
		log.Printf("import: read batch %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that import. Reload the page and try again.")
		return
	}
	items, err := s.MovieRepo.ImportBatchItems(id)
	if err != nil {
		log.Printf("import: read batch %d items: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that import. Reload the page and try again.")
		return
	}
	out := importBatchDetailPayload{importBatchPayload: toImportBatchPayload(b), Items: make([]importBatchItemPayload, len(items))}
	for i, it := range items {
		out.Items[i] = importBatchItemPayload{
			Title: it.Title, Kind: it.Kind, Outcome: it.Outcome, State: it.State, Note: it.Note,
			Imported: it.Imported, Skipped: it.Skipped,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type watchImportRequest struct {
	// Watch starts watching the titles for new episodes and better versions.
	Watch bool `json:"watch"`
	// Missing makes the episodes the shows do not have wanted.
	Missing bool `json:"missing"`
}

type watchImportResult struct {
	Movies  int    `json:"movies"`
	Shows   int    `json:"shows"`
	Message string `json:"message"`
}

// handleWatchImportBatch switches monitoring on for exactly the titles an
// import added, once it has finished, so the person can start it with one
// click on the report. Titles that were in the library before are untouched.
func (s *Server) handleWatchImportBatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid import.")
		return
	}
	var req watchImportRequest
	if err := decodeJSON(r, &req); err != nil || (!req.Watch && !req.Missing) {
		writeError(w, http.StatusBadRequest, "Choose what to start: watching the titles, or looking for missing episodes.")
		return
	}
	b, err := s.MovieRepo.ImportBatchByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That import isn't there any more.")
		return
	}
	if err != nil {
		log.Printf("import: read batch %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that import. Reload the page and try again.")
		return
	}
	if b.Running() {
		writeError(w, http.StatusConflict, "This import is still getting details. Wait until it has finished, then try again.")
		return
	}
	if req.Missing && b.Kind != "tv" {
		writeError(w, http.StatusBadRequest, "Only TV shows have missing episodes.")
		return
	}
	res, err := s.MovieRepo.StartWatchingImport(id, req.Watch, req.Missing)
	if err != nil {
		log.Printf("import: start watching batch %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Nothing was changed because the database could not save it. Try again in a moment.")
		return
	}
	out := watchImportResult{Movies: res.Movies, Shows: res.Shows}
	switch n := res.Movies + res.Shows; {
	case n == 0:
		out.Message = "No titles needed changing."
	case req.Watch && b.Kind == "tv":
		out.Message = fmt.Sprintf("Done: %s monitored now.", plural(n, "show"))
	case req.Watch:
		out.Message = fmt.Sprintf("Done: %s monitored now.", plural(n, "movie"))
	default:
		out.Message = fmt.Sprintf("Done: looking for missing episodes of %s.", plural(n, "show"))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDismissImportBatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid import.")
		return
	}
	if err := s.MovieRepo.DismissImportBatch(id); err != nil {
		log.Printf("import: dismiss batch %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Couldn't close that message. Try again.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- one-off fix for the quality of episodes imported earlier ----

// backfillEpisodeQuality reads the quality of episodes an earlier version
// imported as Unknown from their file names (and the folders above them). It
// runs once; a file that says nothing stays Unknown.
func (s *Server) backfillEpisodeQuality() {
	done, err := s.Settings.Get(settings.KeyEpisodeQualityBackfill)
	if err != nil || done == "1" {
		return
	}
	eps, err := s.MovieRepo.EpisodesWithoutQuality()
	if err != nil {
		log.Printf("import: episode quality backfill: %v", err)
		return
	}
	byPath := map[string]quality.Tier{} // a multi-episode file is read once
	found := map[int64]string{}
	for _, e := range eps {
		tier, seen := byPath[e.FilePath]
		if !seen {
			tier = libimport.TierFromPath(e.FilePath)
			byPath[e.FilePath] = tier
		}
		if tier != quality.TierUnknown {
			found[e.ID] = string(tier)
		}
	}
	if len(found) > 0 {
		if err := s.MovieRepo.SetEpisodeQualities(found); err != nil {
			log.Printf("import: episode quality backfill: %v", err)
			return
		}
	}
	if err := s.Settings.Set(settings.KeyEpisodeQualityBackfill, "1", false); err != nil {
		log.Printf("import: episode quality backfill: %v", err)
		return
	}
	if len(found) > 0 {
		log.Printf("import: set the quality of %d episodes from their file names", len(found))
	}
}

// ---- one-off fix for the added date of titles imported earlier ----

// backfillImportedAddedDates gives titles that an earlier version imported
// the age of their files as the date they were added, so "Recently added"
// makes sense again. It runs once. A file that is newer than the recorded
// date (a later download replaced it) never moves the date forward.
func (s *Server) backfillImportedAddedDates() {
	done, err := s.Settings.Get(settings.KeyImportAddedBackfill)
	if err != nil || done == "1" {
		return
	}
	cands, err := s.MovieRepo.ImportedForAddedDates()
	if err != nil {
		log.Printf("import: added-date backfill: %v", err)
		return
	}
	changed := 0
	for _, c := range cands {
		var newest time.Time
		for _, p := range c.Paths {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		if newest.IsZero() || !newest.Before(c.AddedAt) {
			continue
		}
		if err := s.MovieRepo.SetAddedAt(c.Kind, c.ID, newest); err != nil {
			log.Printf("import: added-date backfill: %v", err)
			return
		}
		changed++
	}
	if err := s.Settings.Set(settings.KeyImportAddedBackfill, "1", false); err != nil {
		log.Printf("import: added-date backfill: %v", err)
		return
	}
	if changed > 0 {
		log.Printf("import: set the added date of %d imported titles from their files", changed)
	}
}
