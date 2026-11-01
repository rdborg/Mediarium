package api

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

// errNoTMDB is returned when there is no client to ask, or TMDB sent an
// empty answer. It is not a passing failure: the title is left for the next run.
var errNoTMDB = errors.New("TMDB has no answer for this title")

const (
	// displayFaceInterval is how often the library is checked for titles
	// whose translated title, poster and plot are not stored yet. The whole library
	// needs one, so this runs more often than the genre backfill, which only
	// fills the few titles that are still missing genres.
	displayFaceInterval = 2 * time.Minute
	// displayFaceBatch caps the TMDB lookups one run makes, and
	// displayFacePause spaces them out, the same polite pace as the genre
	// and country backfill.
	displayFaceBatch = 40
	displayFacePause = 250 * time.Millisecond
)

// shownFace is the title and poster drawn for a movie or show already in the
// library. kind is "movie", or "series", "show" or "tv". It reads only what
// is stored: lists, the dashboard, the calendar, statistics and notifications
// must not ask TMDB while they answer. Until the background job has stored a
// translation, the English title and poster are shown. English itself, and a
// title with no TMDB id, are never looked up.
func (s *Server) shownFace(kind string, tmdbID int, title, posterPath string) (string, string) {
	kind = displayKind(kind)
	title = strings.TrimSpace(title)
	posterPath = strings.TrimSpace(posterPath)
	lang := s.infoLanguage()
	if tmdbID <= 0 || lang == metadata.DefaultLanguage {
		return title, metadata.PosterURL(posterPath)
	}
	stored, ok, err := s.MovieRepo.DisplayFace(kind, tmdbID, lang)
	if err != nil || !ok {
		return title, metadata.PosterURL(posterPath)
	}
	face := chooseDisplayFace(title, posterPath, "", stored.Title, stored.Poster, "")
	return face.title, metadata.PosterURL(face.poster)
}

// shownOverview is the plot drawn for a movie or show already in the library.
// It reads only what is stored. Until the background job has saved a plot in
// the info language, the English plot is shown.
func (s *Server) shownOverview(kind string, tmdbID int, overview string) string {
	kind = displayKind(kind)
	overview = strings.TrimSpace(overview)
	lang := s.infoLanguage()
	if tmdbID <= 0 || lang == metadata.DefaultLanguage {
		return overview
	}
	stored, ok, err := s.MovieRepo.DisplayFace(kind, tmdbID, lang)
	if err != nil || !ok || !stored.HasOverview {
		return overview
	}
	if strings.TrimSpace(stored.Overview) == "" {
		return overview
	}
	return stored.Overview
}

func displayKind(kind string) string {
	switch kind {
	case "series", "show", "tv":
		return "tv"
	default:
		return "movie"
	}
}

// chooseDisplayFace keeps the English title, poster or plot when TMDB gave
// none in the wanted language.
func chooseDisplayFace(storedTitle, storedPoster, storedOverview, localTitle, localPoster, localOverview string) displayFace {
	title := strings.TrimSpace(localTitle)
	if title == "" {
		title = storedTitle
	}
	poster := strings.TrimSpace(localPoster)
	if poster == "" {
		poster = storedPoster
	}
	overview := strings.TrimSpace(localOverview)
	if overview == "" {
		overview = strings.TrimSpace(storedOverview)
	}
	return displayFace{title: title, poster: poster, overview: overview}
}

// displayFace is the title, poster path and plot kept for one title in one language.
type displayFace struct {
	title    string
	poster   string
	overview string
}

// backfillDisplayFaces stores translated titles and posters for library
// titles that do not have one in the current info language yet. A lookup
// that fails for a passing reason ends the run (the next run tries again).
// One run is enough to be going at a time; a second start, for example when
// the language has just changed, leaves the run that is already walking.
func (s *Server) backfillDisplayFaces(ctx context.Context) {
	if s.infoLanguage() == metadata.DefaultLanguage || !s.TMDB().HasAPIKey() {
		return
	}
	if !s.displayFaceMu.TryLock() {
		return
	}
	defer s.displayFaceMu.Unlock()
	s.fillDisplayFacesLocked(ctx, displayFaceBatch)
}

// rememberDisplayFace stores the face of one title that was just added or
// refreshed. It does nothing for English. A failure leaves the title for the
// background job.
func (s *Server) rememberDisplayFace(kind string, tmdbID int, title, poster, overview string) {
	kind = displayKind(kind)
	if tmdbID <= 0 || s.infoLanguage() == metadata.DefaultLanguage || !s.TMDB().HasAPIKey() {
		return
	}
	s.displayFaceMu.Lock()
	defer s.displayFaceMu.Unlock()
	if stored, ok, err := s.MovieRepo.DisplayFace(kind, tmdbID, s.infoLanguage()); err == nil && ok && stored.HasOverview {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := s.storeOneDisplayFace(ctx, kind, tmdbID, title, poster, overview); err != nil {
		log.Printf("display face: %s %d: %v", kind, tmdbID, err)
	}
}

// fillDisplayFacesLocked stores up to limit faces. The caller holds
// displayFaceMu. It stops at the first lookup that failed for a reason other
// than TMDB no longer knowing the title.
func (s *Server) fillDisplayFacesLocked(ctx context.Context, limit int) {
	lang := s.infoLanguage()
	gaps, err := s.MovieRepo.MissingDisplayFaces(lang, limit)
	if err != nil {
		log.Printf("automation: display-faces: %v", err)
		return
	}
	for i, g := range gaps {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(displayFacePause):
			}
		}
		if s.infoLanguage() != lang {
			lang = s.infoLanguage()
			if lang == metadata.DefaultLanguage {
				return
			}
		}
		if err := s.storeOneDisplayFace(ctx, g.Kind, g.TMDBID, g.Title, g.Poster, g.Overview); err != nil {
			log.Printf("automation: display-faces: %q: %v", g.Title, err)
			return
		}
	}
}

// storeOneDisplayFace asks TMDB and saves the face. A title TMDB no longer
// lists keeps the English one, so it is not asked for again. Any other
// failure is returned and nothing is saved.
func (s *Server) storeOneDisplayFace(ctx context.Context, kind string, tmdbID int, title, poster, overview string) error {
	localTitle, localPoster, localOverview, err := s.lookupDisplayFace(ctx, kind, tmdbID)
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return s.MovieRepo.SaveDisplayFace(kind, tmdbID, s.infoLanguage(), title, poster, overview)
		}
		return err
	}
	face := chooseDisplayFace(title, poster, overview, localTitle, localPoster, localOverview)
	return s.MovieRepo.SaveDisplayFace(kind, tmdbID, s.infoLanguage(), face.title, face.poster, face.overview)
}

// lookupDisplayFace asks TMDB for the title, poster and plot in the info language.
func (s *Server) lookupDisplayFace(ctx context.Context, kind string, tmdbID int) (string, string, string, error) {
	client := s.TMDB()
	if client == nil || !client.HasAPIKey() {
		return "", "", "", errNoTMDB
	}
	call, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	local := client.Localized(s.infoLanguage())
	switch kind {
	case "tv":
		show, err := local.GetShow(call, tmdbID)
		if err != nil {
			return "", "", "", err
		}
		if show == nil {
			return "", "", "", errNoTMDB
		}
		return show.Name, show.PosterPath, show.Overview, nil
	default:
		movie, err := local.GetMovie(call, tmdbID)
		if err != nil {
			return "", "", "", err
		}
		if movie == nil {
			return "", "", "", errNoTMDB
		}
		return movie.Title, movie.PosterPath, movie.Overview, nil
	}
}
