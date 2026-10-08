package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

// While a film is only in cinemas, nearly everything posted for it is a
// recording made in the cinema, and some of those are named like a normal
// WEB-DL so no quality profile can tell. Automatic searches therefore wait
// for the digital or disc release (a plain-language version of Radarr's
// "Released" availability), unless the film's profile takes cinema
// recordings. A person can always search by hand.
const (
	homeCheckWindow = 150 * 24 * time.Hour // only films that reached cinemas this recently are looked at
	homeFallback    = 90 * 24 * time.Hour  // after the cinema date, when TMDB lists no digital or disc date
)

// homeHoldUntil says whether automatic searches should still wait, and until
// when. estimated is set when TMDB has no digital or disc date and the date is
// the cinema date plus homeFallback.
func homeHoldUntil(theatrical, home, now time.Time) (until time.Time, estimated, hold bool) {
	if theatrical.IsZero() || now.After(theatrical.Add(homeCheckWindow)) {
		return time.Time{}, false, false // not a cinema film, or long out
	}
	until = home
	if home.IsZero() {
		until, estimated = theatrical.Add(homeFallback), true
	}
	return until, estimated, now.Before(until)
}

// homeReleaseHold reports whether automatic searches for m should wait, with a
// sentence for its history. It asks TMDB only for films released in the last
// few months, and lets the search go ahead when TMDB can't be asked.
func (s *Server) homeReleaseHold(ctx context.Context, m library.Movie, profile quality.Profile) (string, bool) {
	if profile.AllowsTier(quality.TierPreRelease) || m.ReleaseDate == "" || m.TMDBID == 0 {
		return "", false
	}
	released, err := time.Parse("2006-01-02", m.ReleaseDate)
	now := time.Now().UTC()
	if err != nil || now.After(released.Add(homeCheckWindow)) {
		return "", false
	}
	c := s.TMDB()
	if c == nil {
		return "", false
	}
	detail, err := c.GetMovieDetail(ctx, m.TMDBID)
	if err != nil {
		return "", false
	}
	theatrical, home := detail.ReleaseWindow()
	until, estimated, hold := homeHoldUntil(theatrical, home, now)
	if !hold {
		return "", false
	}
	when := until.Format("2 Jan 2006")
	if estimated {
		return fmt.Sprintf("Waiting for the digital or disc release. TMDB hasn't listed a date yet, so Mediarium waits until about %s (90 days after it reached cinemas), because what is posted before then is mostly recordings made in the cinema. Search now looks anyway.", when), true
	}
	return fmt.Sprintf("Waiting for the digital or disc release, due %s, because what is posted before then is mostly recordings made in the cinema. Search now looks anyway.", when), true
}

var homeNoted sync.Map // movie id -> the note already written, so the history gets it once

// noteHomeHold writes the waiting note to the title's history, once per run.
func (s *Server) noteHomeHold(m library.Movie, note string) {
	if prev, ok := homeNoted.Load(m.ID); ok && prev == note {
		return
	}
	homeNoted.Store(m.ID, note)
	s.itemEvent(m.ID, 0, "searched", queue.LevelInfo, note)
}
