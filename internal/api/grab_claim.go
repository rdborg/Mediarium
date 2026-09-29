package api

import (
	"errors"
	"fmt"

	"github.com/ryanborg/mediarium/internal/library"
)

// errAlreadyGrabbed is what an automatic grab is refused with when what it
// is for is already being downloaded by another grab. Automation runs (the
// hunt, RSS sync, the retry after a bad release, the search after adding a
// title) can overlap; without this check two of them could both see an
// episode as missing and grab it twice, or grab a season pack and then its
// single episodes as well.
var errAlreadyGrabbed = errors.New("already being downloaded")

// background runs fn as tracked background work (a download pipeline or an
// automatic retry), so tests can wait for all of it to finish.
func (s *Server) background(fn func()) { s.bg.Go(fn) }

// claimMovie reports whether an automatic grab for movieID may go ahead:
// not while another download for it is still running. Call with grabMu held.
func (s *Server) claimMovie(movieID int64) error {
	busy, err := s.QueueRepo.HasActiveForMovie(movieID)
	if err != nil {
		return err
	}
	if busy {
		return errAlreadyGrabbed
	}
	return nil
}

// claimEpisodes marks the episodes a TV grab will deliver as downloading,
// before the grab's pipeline starts, so every automation run that looks
// after this sees them as taken. For an automatic grab it first refuses
// when any of them is already being downloaded. Call with grabMu held.
func (s *Server) claimEpisodes(targets []library.Episode, auto bool) error {
	if auto {
		for _, ep := range targets {
			if ep.Status == library.StatusDownloading {
				return errAlreadyGrabbed
			}
		}
	}
	for _, ep := range targets {
		if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloading, "", ""); err != nil {
			return fmt.Errorf("mark episode S%02dE%02d as downloading: %w", ep.Season, ep.Episode, err)
		}
	}
	return nil
}
