package api

// TestFlushMediaServerRefresh runs the media server refresh that imports
// queued up now, instead of after its quiet period, for
// media_servers_test.go (package api_test).
func (s *Server) TestFlushMediaServerRefresh() { s.mediaRefresher.Flush() }

// TestSetPlexTVURL points Sign in with Plex at a fake plex.tv, for
// media_servers_signin_test.go (package api_test).
func (s *Server) TestSetPlexTVURL(u string) { s.signIns().plexTVURL = u }
