package api

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
)

// errAlreadyGrabbed is what an automatic grab is refused with when what it
// is for is already being downloaded by another grab. Automation runs (the
// hunt, RSS sync, the retry after a bad release, the search after adding a
// title) can overlap; without this check two of them could both see an
// episode as missing and grab it twice, or grab a season pack and then its
// single episodes as well.
var errAlreadyGrabbed = errors.New("already being downloaded")

// errBlocklisted is what an automatic grab is refused with when the release
// went on the blocklist after the search that found it.
var errBlocklisted = errors.New("on the blocklist")

// releaseBlocklisted looks the release up in the blocklist as it is right
// now. An automation run builds its view of the blocklist when it starts; a
// download that fails while the run is still going puts its release there
// afterwards, and the run must not grab it again. Call with grabMu held.
func (s *Server) releaseBlocklisted(releaseTitle string) bool {
	keys, err := s.Blocklist.Keys()
	return err == nil && keys[blocklist.Key(releaseTitle)]
}

// The scheduled loops (the hunt and the RSS sync) may not add more than this
// many downloads to the download line in any hour. A person grabbing by hand,
// adding a title or pressing "search now" is never held back, and does not
// count. How many run at once is the job of the line itself (dispatch.go):
// what the automatic searches add waits its turn. Without the hourly limit, an
// app that has just been given a big library, or restarted with a long wanted
// list, would fill the line with hundreds of downloads in its first minutes.
const (
	maxAutoDownloadsPerHr = 10
	// maxAutoInLine is how many downloads the automatic searches may have in the
	// line at once, waiting or running. It keeps the line from growing faster
	// than it empties on a slow disk.
	maxAutoInLine = 10
)

// autoGrabRoom reports whether a scheduled loop may add another download to
// the line now. It counts from the database, so a restart does not reset it.
func (s *Server) autoGrabRoom() bool {
	open, err := s.QueueRepo.CountAutomaticOpen()
	if err != nil || open >= maxAutoInLine {
		return false
	}
	recent, err := s.QueueRepo.CountAutomaticAddedSince(time.Now().Add(-time.Hour))
	return err == nil && recent < maxAutoDownloadsPerHr
}

// holdBack says (once per run) that a scheduled loop stopped because it has
// added as many downloads as it may for now.
func holdBack(job string) {
	log.Printf("automation: %s: holding back new downloads for now (at most %d an hour, and %d in the line at once)", job, maxAutoDownloadsPerHr, maxAutoInLine)
}

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
