package api

import (
	"context"
	"time"

	"github.com/rdborg/mediarium/internal/listenbrainz"
	"github.com/rdborg/mediarium/internal/musicbrainz"
)

// TestSetListenBrainz points the Discover lists at a fake server.
func (s *Server) TestSetListenBrainz(baseURL string) {
	s.music.lbMu.Lock()
	defer s.music.lbMu.Unlock()
	s.music.lb = listenbrainz.New("test", listenbrainz.WithBaseURL(baseURL))
}

// TestSetMusicBrainz points the server's MusicBrainz client at a fake,
// without the one-request-per-second limit or retry pauses.
func (s *Server) TestSetMusicBrainz(baseURL string) {
	s.mbMu.Lock()
	defer s.mbMu.Unlock()
	s.mb = musicbrainz.New("test", musicbrainz.WithBaseURL(baseURL), musicbrainz.WithCoverArtBaseURL(baseURL), musicbrainz.WithRateLimit(0),
		musicbrainz.WithBackoff(func(int) time.Duration { return 0 }))
}

// TestConfigDir is the config folder the server was started with.
func (s *Server) TestConfigDir() string { return s.cfg.ConfigDir }

// TestRefreshMusicArtists runs the scheduled check of followed artists for new releases.
func (s *Server) TestRefreshMusicArtists(ctx context.Context) { s.refreshMusicArtists(ctx) }
