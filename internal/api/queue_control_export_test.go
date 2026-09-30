package api

import (
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
)

// TestGrabMovie grabs a release for a movie without going through a search,
// as the grab endpoint does once it has checked the link; TestResumeQueueItem
// resumes a paused download the way the Resume button does. Both are for
// queue_control_test.go (package api_test).
func (s *Server) TestGrabMovie(m library.Movie, releaseTitle, downloadURL string) (int64, error) {
	return s.grabRelease(m, releaseTitle, downloadURL, 0, indexers.ProtocolUsenet)
}

func (s *Server) TestResumeQueueItem(id int64) error {
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		return err
	}
	return s.resumeItem(item)
}

// TestWorkDirFor is queue item id's working folder in the downloads area.
func (s *Server) TestWorkDirFor(id int64) string { return s.workDirFor(id) }

// TestGrabMovieTorrent is TestGrabMovie for a torrent release, and
// TestTorrentRunning reports whether queue item id has a torrent in the
// engine.
func (s *Server) TestGrabMovieTorrent(m library.Movie, releaseTitle, magnet string) (int64, error) {
	return s.grabRelease(m, releaseTitle, magnet, 0, indexers.ProtocolTorrent)
}

func (s *Server) TestTorrentRunning(id int64) bool { return s.torrents.active(id) }

// TestGrabMovieAs grabs a release the way a search does: by a person's
// "search now" (automatic false) or by the scheduled hunt (automatic true).
func (s *Server) TestGrabMovieAs(m library.Movie, releaseTitle, downloadURL string, automatic bool) (int64, error) {
	return s.grabMovie(m, releaseTitle, downloadURL, 0, indexers.ProtocolUsenet, searchKind(!automatic))
}

// TestKickDownloads looks at the download line, as the scheduled check does.
func (s *Server) TestKickDownloads() { s.kickDownloads() }

// TestRunningDownloads is how many downloads hold a place in the line.
func (s *Server) TestRunningDownloads() int { return s.dispatch.Running() }
