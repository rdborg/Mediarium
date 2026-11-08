package api

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// Automatic searches wait for a title to come out: a movie before its release
// date and an episode before it airs are left alone, and a film that is only in
// cinemas waits for its digital or disc release (home_release.go). The wait is
// on unless someone switches it off (Settings > Library > Quality >
// Automation); a search by hand always looks. The pickers are plain functions,
// so the switch is kept here, like the minimum size check.
var holdUnreleasedOff atomic.Bool

func holdUnreleasedOn() bool { return !holdUnreleasedOff.Load() }

func setHoldUnreleased(on bool) { holdUnreleasedOff.Store(!on) }

// loadHoldUnreleased reads the saved switch.
func (s *Server) loadHoldUnreleased() {
	raw, _ := s.Settings.Get(settings.KeyHoldUnreleased)
	setHoldUnreleased(raw != "0")
}

// waitingForRelease is whether an automatic search should leave a movie with
// this release date alone for now.
func waitingForRelease(releaseDate string) bool {
	return holdUnreleasedOn() && unreleased(releaseDate)
}

// unairedHoldNote words why a show has nothing to search for yet, from the
// next episode it waits for.
func unairedHoldNote(ep library.Episode) string {
	label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
	if t, err := time.Parse("2006-01-02", ep.AirDate); err == nil {
		return fmt.Sprintf("Waiting for %s to air on %s. Automatic searches skip an episode until it has aired. Search now looks anyway.", label, t.Format("2 Jan 2006"))
	}
	return fmt.Sprintf("Waiting for %s to air. Automatic searches skip an episode until it has aired. Search now looks anyway.", label)
}

// unreleasedMovieNote words why a movie is not searched for yet.
func unreleasedMovieNote(releaseDate string) string {
	if t, err := time.Parse("2006-01-02", releaseDate); err == nil {
		return fmt.Sprintf("Waiting for the release on %s. Automatic searches skip a film until it is out. Search now looks anyway.", t.Format("2 Jan 2006"))
	}
	return "Waiting for the release. Automatic searches skip a film until it is out. Search now looks anyway."
}

var holdNoted sync.Map // "m<id>" or "s<id>" -> the note already written, so the history gets it once

// noteHold writes a waiting note to the history of a movie (seriesID 0) or a
// show, once for as long as the note stays the same.
func (s *Server) noteHold(movieID, seriesID int64, note string) {
	key := fmt.Sprintf("m%d", movieID)
	if seriesID != 0 {
		key = fmt.Sprintf("s%d", seriesID)
	}
	if prev, ok := holdNoted.Load(key); ok && prev == note {
		return
	}
	holdNoted.Store(key, note)
	s.itemEvent(movieID, seriesID, "searched", queue.LevelInfo, note)
}
