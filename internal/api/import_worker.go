package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
)

// After an import registers the titles, this worker fills in their details
// from the movie database in the background: poster, summary, genres and
// release date for movies, the full season and episode list for shows (and
// then the episodes found on disk are marked downloaded). What is left to do
// is kept in the database (import_items), so a restart carries on where it
// stopped and a title whose lookup fails is tried again later, never lost.

const (
	importDetailsParallel = 5  // titles looked up at the same time
	importDetailsBatch    = 50 // titles read from the database at a time
	importDetailsTimeout  = 2 * time.Minute
	importDetailsInterval = time.Minute // how often waiting titles are checked for a retry
	// A title is called a problem after this many failed tries in a row. It
	// is still tried again, just less often.
	importProblemAfter = 3
)

// importState is the worker's bookkeeping on the Server.
type importState struct {
	mu      sync.Mutex
	running bool
	rerun   bool

	// parallel overrides importDetailsParallel and backoff the retry wait
	// (tests set them); zero values mean the defaults.
	parallel int
	backoff  func(attempt int, permanent bool) time.Duration
}

func (st *importState) workers() int {
	if st.parallel > 0 {
		return st.parallel
	}
	return importDetailsParallel
}

func (st *importState) wait(attempt int, permanent bool) time.Duration {
	if st.backoff != nil {
		return st.backoff(attempt, permanent)
	}
	return defaultImportBackoff(attempt, permanent)
}

// defaultImportBackoff is how long to wait before the next try of a title
// that has failed attempt times in a row. A title the movie database does
// not know is looked for once a day.
func defaultImportBackoff(attempt int, permanent bool) time.Duration {
	if permanent {
		return 24 * time.Hour
	}
	switch attempt {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	case 3:
		return 10 * time.Minute
	case 4:
		return 30 * time.Minute
	case 5:
		return 2 * time.Hour
	}
	return 6 * time.Hour
}

// kickImportWorker starts the worker if it is not running, or asks the
// running one to look again once it is done with what it has.
func (s *Server) kickImportWorker() {
	s.importWork.mu.Lock()
	defer s.importWork.mu.Unlock()
	if s.importWork.running {
		s.importWork.rerun = true
		return
	}
	s.importWork.running = true
	s.background(s.drainImportDetails)
}

// importDetailsJob is the scheduled check. It also resumes the work that
// was left unfinished when the app last stopped.
func (s *Server) importDetailsJob(context.Context) { s.kickImportWorker() }

func (s *Server) drainImportDetails() {
	for {
		s.processDueImports()
		s.importWork.mu.Lock()
		if !s.importWork.rerun {
			s.importWork.running = false
			s.importWork.mu.Unlock()
			return
		}
		s.importWork.rerun = false
		s.importWork.mu.Unlock()
	}
}

// processDueImports works through every title that is due, a few at a time,
// until none is left.
func (s *Server) processDueImports() {
	tried := map[int64]bool{} // each title once per run, so a failing save cannot loop
	for {
		items, err := s.MovieRepo.DueImportItems(time.Now(), importDetailsBatch)
		if err != nil {
			log.Printf("import: list titles waiting for details: %v", err)
			return
		}
		var todo []library.ImportItem
		for _, it := range items {
			if !tried[it.ID] {
				tried[it.ID] = true
				todo = append(todo, it)
			}
		}
		if len(todo) == 0 {
			return
		}
		sem := make(chan struct{}, s.importWork.workers())
		var wg sync.WaitGroup
		for _, it := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				s.fetchImportDetails(it)
			}()
		}
		wg.Wait()
	}
}

func (s *Server) fetchImportDetails(item library.ImportItem) {
	ctx, cancel := context.WithTimeout(context.Background(), importDetailsTimeout)
	defer cancel()

	var err error
	if item.Kind == "movie" {
		err = s.fetchMovieDetails(ctx, item)
	} else {
		err = s.fetchSeriesDetails(ctx, item)
	}
	if err == nil {
		return
	}
	note, permanent, hard := describeImportFailure(err)
	attempts := item.Attempts + 1
	state := library.ImportPending
	if hard || attempts >= importProblemAfter {
		state = library.ImportProblem
	}
	if serr := s.MovieRepo.SetImportItemState(item, state, note, attempts, time.Now().Add(s.importWork.wait(attempts, permanent))); serr != nil {
		log.Printf("import: save state of %q: %v", item.Title, serr)
	}
	log.Printf("import: details for %q (attempt %d): %v", item.Title, attempts, err)
}

func (s *Server) fetchMovieDetails(ctx context.Context, item library.ImportItem) error {
	m, err := s.TMDB().GetMovie(ctx, item.TMDBID)
	if err != nil {
		return err
	}
	return s.MovieRepo.CompleteMovieImport(item, library.MovieDetails{
		Title: m.Title, Year: m.Year(), Overview: m.Overview, PosterPath: m.PosterPath,
		ReleaseDate: m.ReleaseDate, Genres: s.TMDB().MovieGenres(ctx, *m),
	})
}

func (s *Server) fetchSeriesDetails(ctx context.Context, item library.ImportItem) error {
	// GetShowEpisodes reads the seasons in parallel.
	detail, infos, err := s.TMDB().GetShowEpisodes(ctx, item.TMDBID)
	if err != nil {
		return err
	}
	_, err = s.MovieRepo.CompleteSeriesImport(item, library.SeriesDetails{
		Title: detail.Name, Year: detail.Year(), Overview: detail.Overview, PosterPath: detail.PosterPath,
		FirstAirDate: detail.FirstAirDate, Genres: s.TMDB().ShowGenres(ctx, detail.Show),
	}, toLibraryEpisodes(infos))
	return err
}

// describeImportFailure turns a failed lookup into a plain reason for the
// person. permanent means the movie database does not have the title; hard
// means it will not fix itself without the person doing something.
func describeImportFailure(err error) (note string, permanent, hard bool) {
	var status *metadata.StatusError
	switch {
	case errors.Is(err, metadata.ErrNoAPIKey):
		return "There's no movie database key yet. Add one in Settings and it tries again.", false, true
	case errors.Is(err, metadata.ErrInvalidKey):
		return "The movie database didn't accept your key. Check it in Settings and it tries again.", false, true
	case errors.Is(err, context.DeadlineExceeded):
		return "The movie database took too long to answer. It tries again soon.", false, false
	case errors.As(err, &status):
		switch {
		case status.Code == http.StatusNotFound:
			return "The movie database doesn't have this title. It looks again tomorrow.", true, true
		case status.Code == http.StatusTooManyRequests:
			return "The movie database asked Mediarium to slow down. It tries again soon.", false, false
		default:
			return "The movie database isn't answering right now. It tries again soon.", false, false
		}
	}
	return "Couldn't reach the movie database. It tries again soon.", false, false
}
