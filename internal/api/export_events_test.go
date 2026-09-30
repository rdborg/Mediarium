package api

// TestIncompleteDir is the downloads working folder, and TestRetryMovie runs
// the immediate retry that follows a blocklisted release, for
// item_events_test.go (package api_test).
func (s *Server) TestIncompleteDir() string    { return s.downloadsIncompleteDir() }
func (s *Server) TestRetryMovie(movieID int64) { s.retryMovie(movieID) }
