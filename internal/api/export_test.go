package api

import (
	"context"

	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/subtitles"
	"github.com/ryanborg/mediarium/internal/trakt"
)

// TestHunt and TestRSSSync re-export the unexported hunt/rssSync jobs for
// automation_hunt_test.go (package api_test) to call directly, instead of
// waiting on the real 30-minute/15-minute scheduler intervals.
func (s *Server) TestHunt(ctx context.Context)    { s.hunt(ctx) }
func (s *Server) TestRSSSync(ctx context.Context) { s.rssSync(ctx) }

// TestSetTraktBaseURL and TestSetTMDBBaseURL point the live Trakt/TMDB
// clients at a local fixture server for import_list_test.go (package
// api_test), instead of the real third-party APIs (CLAUDE.md: local
// fixtures, not live network calls, in tests).
func (s *Server) TestSetTraktBaseURL(clientID, baseURL string) {
	s.traktMu.Lock()
	s.trakt = trakt.NewWithBaseURL(clientID, baseURL)
	s.traktMu.Unlock()
}

func (s *Server) TestSetTMDBBaseURL(apiKey, baseURL string) {
	s.tmdbMu.Lock()
	s.tmdb = metadata.NewWithBaseURL(apiKey, baseURL)
	s.tmdbMu.Unlock()
}

// TestSetSubtitlesBaseURL points the live OpenSubtitles client at a local
// fixture server; TestSubtitleSweepJob runs the scheduled sweep once.
func (s *Server) TestSetSubtitlesBaseURL(apiKey, baseURL string) {
	s.subsMu.Lock()
	s.subs = subtitles.NewWithBaseURL(apiKey, baseURL)
	s.subsMu.Unlock()
}

func (s *Server) TestSubtitleSweepJob(ctx context.Context) { s.subtitleSweepJob(ctx) }
