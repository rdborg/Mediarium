package api

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

// How long a translated title and poster are remembered, and how long a
// failed lookup is remembered. The Library and Activity ask again every few
// seconds, so a TMDB outage must not turn into a request on every refresh.
const (
	displayFaceRemember = 6 * time.Hour
	displayFaceRetry    = 2 * time.Minute
)

// displayFace is the title and poster path TMDB gave for one movie or show
// in one language.
type displayFace struct {
	title  string
	poster string
}

// displayFaceCache remembers those faces. The key is "movie:603:it-IT".
type displayFaceCache struct {
	mu   sync.Mutex
	m    map[string]displayFaceHit
	wait map[string]chan struct{}
}

type displayFaceHit struct {
	face  displayFace
	until time.Time
}

func (c *displayFaceCache) get(key string) (displayFace, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hit, ok := c.m[key]
	if !ok || time.Now().After(hit.until) {
		return displayFace{}, false
	}
	return hit.face, true
}

func (c *displayFaceCache) set(key string, face displayFace, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]displayFaceHit)
	}
	c.m[key] = displayFaceHit{face: face, until: until}
}

// begin waits for a lookup of key that is already running, or marks this
// caller as the one that runs it. ran is false when another caller is doing
// the lookup; the caller then reads the cache again.
func (c *displayFaceCache) begin(key string) (wait <-chan struct{}, leader bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wait == nil {
		c.wait = make(map[string]chan struct{})
	}
	if ch, ok := c.wait[key]; ok {
		return ch, false
	}
	ch := make(chan struct{})
	c.wait[key] = ch
	return ch, true
}

func (c *displayFaceCache) finish(key string) {
	c.mu.Lock()
	ch := c.wait[key]
	delete(c.wait, key)
	c.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// shownFace is displayTitlePoster for either a movie or a show. kind is
// "movie", or "series", "show" or "tv".
func (s *Server) shownFace(kind string, tmdbID int, title, posterPath string) (string, string) {
	switch kind {
	case "series", "show", "tv":
		kind = "tv"
	default:
		kind = "movie"
	}
	return s.displayTitlePoster(kind, tmdbID, title, posterPath)
}

// displayTitlePoster is the title and poster shown for a movie or show that
// is already in the library (Library, Dashboard, Activity, the calendar,
// reports, requests, statistics and notifications). kind is "movie" or "tv".
// The library keeps the English title and poster; this asks TMDB for the
// language chosen for movie and show info and uses those when they exist.
// English, a missing TMDB id, or a lookup that fails leaves the stored ones,
// so the cards still draw. Indexer searches keep using the stored English title.
func (s *Server) displayTitlePoster(kind string, tmdbID int, title, posterPath string) (string, string) {
	title = strings.TrimSpace(title)
	posterPath = strings.TrimSpace(posterPath)
	if tmdbID <= 0 || s.infoLanguage() == metadata.DefaultLanguage {
		return title, metadata.PosterURL(posterPath)
	}
	key := kind + ":" + strconv.Itoa(tmdbID) + ":" + s.infoLanguage()
	if face, ok := s.displayFaces.get(key); ok {
		return face.title, metadata.PosterURL(face.poster)
	}
	wait, leader := s.displayFaces.begin(key)
	if !leader {
		<-wait
		if face, ok := s.displayFaces.get(key); ok {
			return face.title, metadata.PosterURL(face.poster)
		}
		return title, metadata.PosterURL(posterPath)
	}
	defer s.displayFaces.finish(key)

	localTitle, localPoster, ok := s.lookupDisplayFace(kind, tmdbID)
	face := displayFace{title: title, poster: posterPath}
	until := time.Now().Add(displayFaceRetry)
	if ok {
		face = chooseDisplayFace(title, posterPath, localTitle, localPoster)
		until = time.Now().Add(displayFaceRemember)
	}
	s.displayFaces.set(key, face, until)
	return face.title, metadata.PosterURL(face.poster)
}

// chooseDisplayFace keeps the stored title or poster when TMDB gave none in
// the wanted language.
func chooseDisplayFace(storedTitle, storedPoster, localTitle, localPoster string) displayFace {
	title := strings.TrimSpace(localTitle)
	if title == "" {
		title = storedTitle
	}
	poster := strings.TrimSpace(localPoster)
	if poster == "" {
		poster = storedPoster
	}
	return displayFace{title: title, poster: poster}
}

// lookupDisplayFace asks TMDB for the title and poster in the info language.
// ok is false when there is no key or TMDB does not answer.
func (s *Server) lookupDisplayFace(kind string, tmdbID int) (string, string, bool) {
	client := s.TMDB()
	if client == nil || !client.HasAPIKey() {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	local := client.Localized(s.infoLanguage())
	switch kind {
	case "tv":
		show, err := local.GetShow(ctx, tmdbID)
		if err != nil || show == nil {
			return "", "", false
		}
		return show.Name, show.PosterPath, true
	default:
		movie, err := local.GetMovie(ctx, tmdbID)
		if err != nil || movie == nil {
			return "", "", false
		}
		return movie.Title, movie.PosterPath, true
	}
}
