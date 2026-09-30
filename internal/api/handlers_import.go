package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
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
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	PosterURL  string `json:"posterUrl,omitempty"`
	PosterPath string `json:"-"`
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
	batchID int64 // set once the review has been confirmed
	err     string
	created time.Time
}

type importJobPayload struct {
	ID      string              `json:"id"`
	Kind    libimport.Kind      `json:"kind"`
	Root    string              `json:"root"`
	Phase   string              `json:"phase"`
	Done    int                 `json:"done"`
	Total   int                 `json:"total"`
	Items   []importItemPayload `json:"items"`
	Skipped []string            `json:"skipped"`
	BatchID int64               `json:"batchId,omitempty"`
	Error   string              `json:"error,omitempty"`
}

func (j *importJob) snapshot() importJobPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	return importJobPayload{
		ID: j.id, Kind: j.kind, Root: j.root, Phase: j.phase, Done: j.done, Total: j.total,
		Items:   append([]importItemPayload{}, j.items...),
		Skipped: append([]string{}, j.skipped...),
		BatchID: j.batchID,
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
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	req.Path = strings.TrimSpace(req.Path)
	if rejectBad(w,
		checkRequired(req.Path, "Enter the folder to scan, for example /media/movies."),
		checkAbsPath(req.Path, "/media/movies"),
		checkMaxLen(req.Path, "The folder path", maxPathLen),
	) {
		return
	}
	kind := libimport.Kind(req.Kind)
	if kind != libimport.KindMovie && kind != libimport.KindTV {
		writeError(w, http.StatusBadRequest, `Choose whether the folder holds "movie" or "tv" titles.`)
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie info and lists).")
		return
	}
	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s isn't a folder Mediarium can read. If you use Docker, mount it into the container first.", req.Path))
		return
	}

	if msg := s.importRunning(kind, true); msg != "" {
		writeError(w, http.StatusConflict, msg)
		return
	}

	job := s.newImportJob(kind, req.Path)
	go s.runImportScan(job)
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.id})
}

func (s *Server) handleGetImportJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.findImportJob(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "That scan isn't available any more. A newer scan may have replaced it.")
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
				payload[c] = importCandidatePayload{TMDBID: cand.TMDBID, Title: cand.Title, Year: cand.Year, PosterURL: metadata.PosterURL(cand.PosterPath), PosterPath: cand.PosterPath}
			}
			job.mu.Lock()
			items[i].Match, items[i].Candidates, items[i].InLibrary = status, payload, inLibrary
			if matchErr != nil {
				items[i].Error = "Couldn't look this up in the movie database: " + matchErr.Error()
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

// handleTMDBSearch is a plain TMDB title search for either kind, used by the
// import review to fix a wrong or missing automatic match.
func (s *Server) handleTMDBSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie info and lists).")
		return
	}
	out := []importCandidatePayload{}
	switch r.URL.Query().Get("kind") {
	case "tv":
		shows, err := s.TMDB().SearchTV(r.Context(), query)
		if err != nil {
			writeUpstreamError(w, "search", err)
			return
		}
		for _, sh := range shows {
			out = append(out, importCandidatePayload{TMDBID: sh.TMDBID, Title: sh.Name, Year: sh.Year(), PosterURL: metadata.PosterURL(sh.PosterPath)})
		}
	default:
		movies, err := s.TMDB().SearchMovies(r.Context(), query)
		if err != nil {
			writeUpstreamError(w, "search", err)
			return
		}
		for _, m := range movies {
			out = append(out, importCandidatePayload{TMDBID: m.TMDBID, Title: m.Title, Year: m.Year(), PosterURL: metadata.PosterURL(m.PosterPath)})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
