package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ryanborg/mediarium/internal/libimport"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/metadata"
)

// Importing an existing library is a background job the UI polls: scanning a
// big folder and looking every title up on TMDB takes a while, and the
// matched items need a human review step before anything is registered.
//
//	scanning -> matching -> ready -> importing -> done   (or failed)
const (
	importScanning  = "scanning"
	importMatching  = "matching"
	importReady     = "ready"
	importImporting = "importing"
	importDone      = "done"
	importFailed    = "failed"

	importMatchWorkers = 4
	maxImportJobs      = 5
)

type importCandidatePayload struct {
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	PosterURL string `json:"posterUrl,omitempty"`
}

type importItemPayload struct {
	Key        string                   `json:"key"`
	Title      string                   `json:"title"`
	Year       int                      `json:"year"`
	FileCount  int                      `json:"fileCount"`
	SizeBytes  int64                    `json:"sizeBytes"`
	SamplePath string                   `json:"samplePath"`
	Seasons    []int                    `json:"seasons,omitempty"`
	Quality    string                   `json:"quality,omitempty"`
	Match      libimport.MatchStatus    `json:"match"`
	Candidates []importCandidatePayload `json:"candidates"`
	InLibrary  bool                     `json:"inLibrary"`
	Error      string                   `json:"error,omitempty"`
}

type importResultPayload struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
}

type importJob struct {
	mu      sync.Mutex
	id      string
	kind    libimport.Kind
	root    string
	phase   string
	done    int
	total   int
	groups  []libimport.Group
	items   []importItemPayload
	skipped []string
	results []importResultPayload
	err     string
	created time.Time
}

type importJobPayload struct {
	ID      string                `json:"id"`
	Kind    libimport.Kind        `json:"kind"`
	Root    string                `json:"root"`
	Phase   string                `json:"phase"`
	Done    int                   `json:"done"`
	Total   int                   `json:"total"`
	Items   []importItemPayload   `json:"items"`
	Skipped []string              `json:"skipped"`
	Results []importResultPayload `json:"results"`
	Error   string                `json:"error,omitempty"`
}

func (j *importJob) snapshot() importJobPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	return importJobPayload{
		ID: j.id, Kind: j.kind, Root: j.root, Phase: j.phase, Done: j.done, Total: j.total,
		Items:   append([]importItemPayload{}, j.items...),
		Skipped: append([]string{}, j.skipped...),
		Results: append([]importResultPayload{}, j.results...),
		Error:   j.err,
	}
}

func (j *importJob) setPhase(phase string, done, total int) {
	j.mu.Lock()
	j.phase, j.done, j.total = phase, done, total
	j.mu.Unlock()
}

func (j *importJob) fail(err error) {
	j.mu.Lock()
	j.phase, j.err = importFailed, err.Error()
	j.mu.Unlock()
}

func (s *Server) newImportJob(kind libimport.Kind, root string) *importJob {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	job := &importJob{id: hex.EncodeToString(buf), kind: kind, root: root, phase: importScanning, created: time.Now()}

	s.importMu.Lock()
	defer s.importMu.Unlock()
	if s.importJobs == nil {
		s.importJobs = map[string]*importJob{}
	}
	s.importJobs[job.id] = job
	// Keep only the most recent few; each holds a full scan result.
	if len(s.importJobs) > maxImportJobs {
		var oldest *importJob
		for _, j := range s.importJobs {
			if j != job && (oldest == nil || j.created.Before(oldest.created)) {
				oldest = j
			}
		}
		if oldest != nil {
			delete(s.importJobs, oldest.id)
		}
	}
	return job
}

func (s *Server) findImportJob(id string) (*importJob, bool) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	j, ok := s.importJobs[id]
	return j, ok
}

type scanRequest struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// handleScanLibrary starts a scan of an existing folder.
func (s *Server) handleScanLibrary(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	kind := libimport.Kind(req.Kind)
	if kind != libimport.KindMovie && kind != libimport.KindTV {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "TMDB API key not configured yet — set it in Settings")
		return
	}
	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s isn't a folder Mediarium can read — in Docker, mount it into the container first", req.Path))
		return
	}

	job := s.newImportJob(kind, req.Path)
	go s.runImportScan(job)
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.id})
}

func (s *Server) handleGetImportJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.findImportJob(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "scan not found — it may have been replaced by a newer one")
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

func (s *Server) runImportScan(job *importJob) {
	var (
		res libimport.Result
		err error
	)
	if job.kind == libimport.KindMovie {
		res, err = libimport.ScanMovies(job.root)
	} else {
		res, err = libimport.ScanTV(job.root)
	}
	if err != nil {
		job.fail(err)
		return
	}

	items := make([]importItemPayload, len(res.Groups))
	for i, g := range res.Groups {
		items[i] = describeGroup(g)
	}
	job.mu.Lock()
	job.groups, job.items, job.skipped = res.Groups, items, res.Skipped
	job.mu.Unlock()
	job.setPhase(importMatching, 0, len(items))

	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		sem  = make(chan struct{}, importMatchWorkers)
		mu   sync.Mutex
		done int
	)
	for i := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			status, cands, matchErr := s.matchGroup(ctx, job.kind, res.Groups[i])
			inLibrary := status == libimport.MatchMatched && len(cands) > 0 && s.alreadyInLibrary(job.kind, cands[0].TMDBID)

			payload := make([]importCandidatePayload, len(cands))
			for c, cand := range cands {
				payload[c] = importCandidatePayload{TMDBID: cand.TMDBID, Title: cand.Title, Year: cand.Year, PosterURL: metadata.PosterURL(cand.PosterPath)}
			}
			job.mu.Lock()
			items[i].Match, items[i].Candidates, items[i].InLibrary = status, payload, inLibrary
			if matchErr != nil {
				items[i].Error = "TMDB lookup failed: " + matchErr.Error()
			}
			job.mu.Unlock()

			mu.Lock()
			done++
			d := done
			mu.Unlock()
			job.setPhase(importMatching, d, len(items))
		}(i)
	}
	wg.Wait()
	job.setPhase(importReady, len(items), len(items))
}

func describeGroup(g libimport.Group) importItemPayload {
	item := importItemPayload{Key: g.Key, Title: g.Title, Year: g.Year, FileCount: len(g.Files), Candidates: []importCandidatePayload{}}
	seasons := map[int]bool{}
	best := -1
	for i, f := range g.Files {
		item.SizeBytes += f.SizeBytes
		if f.Season > 0 {
			seasons[f.Season] = true
		}
		if best < 0 || f.SizeBytes > g.Files[best].SizeBytes {
			best = i
		}
	}
	if best >= 0 {
		item.SamplePath = g.Files[best].Path
		item.Quality = g.Files[best].Quality
	}
	for n := range seasons {
		item.Seasons = append(item.Seasons, n)
	}
	sort.Ints(item.Seasons)
	return item
}

func (s *Server) matchGroup(ctx context.Context, kind libimport.Kind, g libimport.Group) (libimport.MatchStatus, []libimport.Candidate, error) {
	var cands []libimport.Candidate
	if kind == libimport.KindMovie {
		movies, err := s.TMDB().SearchMovies(ctx, g.Title)
		if err != nil {
			return libimport.MatchUnmatched, nil, err
		}
		for _, m := range movies {
			cands = append(cands, libimport.Candidate{TMDBID: m.TMDBID, Title: m.Title, Year: m.Year(), PosterPath: m.PosterPath})
		}
	} else {
		shows, err := s.TMDB().SearchTV(ctx, g.Title)
		if err != nil {
			return libimport.MatchUnmatched, nil, err
		}
		for _, sh := range shows {
			cands = append(cands, libimport.Candidate{TMDBID: sh.TMDBID, Title: sh.Name, Year: sh.Year(), PosterPath: sh.PosterPath})
		}
	}
	status, ordered := libimport.Decide(g.Title, g.Year, cands)
	return status, ordered, nil
}

// alreadyInLibrary reports whether registering this match would add nothing
// for a movie (already downloaded). A series is never "already in": a
// half-filled series can still gain episodes from the import.
func (s *Server) alreadyInLibrary(kind libimport.Kind, tmdbID int) bool {
	if kind != libimport.KindMovie {
		return false
	}
	m, ok, err := s.MovieRepo.GetByTMDBID(tmdbID)
	return err == nil && ok && m.Status == library.StatusDownloaded
}

type importSelection struct {
	Key    string `json:"key"`
	TMDBID int    `json:"tmdbId"`
}

type importRequest struct {
	JobID      string            `json:"jobId"`
	Selections []importSelection `json:"selections"`
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
		writeError(w, http.StatusNotFound, "scan not found — it may have been replaced by a newer one")
		return
	}
	job.mu.Lock()
	if job.phase != importReady {
		phase := job.phase
		job.mu.Unlock()
		writeError(w, http.StatusConflict, "this scan isn't ready to import (it is "+phase+")")
		return
	}
	byKey := map[string]libimport.Group{}
	for _, g := range job.groups {
		byKey[g.Key] = g
	}
	var chosen []struct {
		group  libimport.Group
		tmdbID int
	}
	for _, sel := range req.Selections {
		g, found := byKey[sel.Key]
		if !found || sel.TMDBID == 0 {
			job.mu.Unlock()
			writeError(w, http.StatusBadRequest, "unknown item or missing TMDB match in selections")
			return
		}
		chosen = append(chosen, struct {
			group  libimport.Group
			tmdbID int
		}{g, sel.TMDBID})
	}
	job.phase, job.done, job.total, job.results = importImporting, 0, len(chosen), nil
	job.mu.Unlock()

	go func() {
		ctx := context.Background()
		for i, c := range chosen {
			var res importResultPayload
			if job.kind == libimport.KindMovie {
				res = s.registerMovieGroup(ctx, c.group, c.tmdbID)
			} else {
				res = s.registerSeriesGroup(ctx, c.group, c.tmdbID)
			}
			res.Key = c.group.Key
			job.mu.Lock()
			job.results = append(job.results, res)
			job.done = i + 1
			job.mu.Unlock()
		}
		job.setPhase(importDone, len(chosen), len(chosen))
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.id})
}

// registerMovieGroup adds the movie to the library (if it isn't already)
// and marks it downloaded pointing at the file where it already lives — no
// copy, move or rename. With several files (multiple versions) the largest
// is taken as the main feature.
func (s *Server) registerMovieGroup(ctx context.Context, g libimport.Group, tmdbID int) importResultPayload {
	res := importResultPayload{Title: g.Title}
	main := g.Files[0]
	for _, f := range g.Files[1:] {
		if f.SizeBytes > main.SizeBytes {
			main = f
		}
	}

	existing, exists, err := s.MovieRepo.GetByTMDBID(tmdbID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if exists && existing.Status == library.StatusDownloaded {
		res.Skipped, res.Message = 1, "already in the library"
		return res
	}
	if !exists {
		tm, err := s.TMDB().GetMovie(ctx, tmdbID)
		if err != nil {
			res.Error = "look up movie on TMDB: " + err.Error()
			return res
		}
		existing, err = s.MovieRepo.Add(library.Movie{
			TMDBID: tm.TMDBID, Title: tm.Title, Year: tm.Year(), Overview: tm.Overview,
			PosterPath: tm.PosterPath, Monitored: true, ReleaseDate: tm.ReleaseDate,
			Genres: s.TMDB().MovieGenres(ctx, *tm),
		})
		if err != nil {
			res.Error = err.Error()
			return res
		}
	}
	if err := s.MovieRepo.SetStatus(existing.ID, library.StatusDownloaded, main.Quality, main.Path); err != nil {
		res.Error = err.Error()
		return res
	}
	res.Title = existing.Title
	res.Imported = 1
	_ = s.QueueRepo.LogActivity(existing.ID, "imported", fmt.Sprintf("%s: registered existing file %s", existing.Title, main.Path))
	return res
}

// registerSeriesGroup adds the series (with its full TMDB episode list) if
// needed, then marks every episode found on disk as downloaded in place.
func (s *Server) registerSeriesGroup(ctx context.Context, g libimport.Group, tmdbID int) importResultPayload {
	res := importResultPayload{Title: g.Title}

	series, exists, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if !exists {
		series, err = s.addSeriesFromTMDB(ctx, tmdbID, 0)
		if err != nil {
			res.Error = "add series from TMDB: " + err.Error()
			return res
		}
	} else if err := s.refreshSeries(ctx, series); err != nil {
		// Not fatal: the episodes we already know about can still be filled.
		log.Printf("api: import: refresh %q: %v", series.Title, err)
	}
	res.Title = series.Title

	eps, err := s.MovieRepo.ListEpisodes(series.ID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	byNumber := map[[2]int]library.Episode{}
	for _, ep := range eps {
		byNumber[[2]int{ep.Season, ep.Episode}] = ep
	}

	unknown := 0
	for _, f := range g.Files {
		for _, n := range f.Episodes {
			ep, ok := byNumber[[2]int{f.Season, n}]
			if !ok {
				unknown++ // TMDB doesn't list this episode (specials, or numbering differences)
				continue
			}
			if ep.Status == library.StatusDownloaded {
				res.Skipped++
				continue
			}
			if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, f.Quality, f.Path); err != nil {
				res.Error = err.Error()
				return res
			}
			res.Imported++
		}
	}
	if unknown > 0 {
		res.Message = fmt.Sprintf("%d episode(s) aren't listed on TMDB for this show and were left out", unknown)
	}
	_ = s.QueueRepo.LogActivity(0, "imported", fmt.Sprintf("%s: registered %d existing episode(s)", series.Title, res.Imported))
	return res
}

// handleTMDBSearch is a plain TMDB title search for either kind, used by the
// import review to fix a wrong or missing automatic match.
func (s *Server) handleTMDBSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "TMDB API key not configured yet — set it in Settings")
		return
	}
	out := []importCandidatePayload{}
	switch r.URL.Query().Get("kind") {
	case "tv":
		shows, err := s.TMDB().SearchTV(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusBadGateway, "search TMDB: "+err.Error())
			return
		}
		for _, sh := range shows {
			out = append(out, importCandidatePayload{TMDBID: sh.TMDBID, Title: sh.Name, Year: sh.Year(), PosterURL: metadata.PosterURL(sh.PosterPath)})
		}
	default:
		movies, err := s.TMDB().SearchMovies(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusBadGateway, "search TMDB: "+err.Error())
			return
		}
		for _, m := range movies {
			out = append(out, importCandidatePayload{TMDBID: m.TMDBID, Title: m.Title, Year: m.Year(), PosterURL: metadata.PosterURL(m.PosterPath)})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
