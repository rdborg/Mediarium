package api

// TestManualCoverPath is where the cover given by hand for an album is kept.
func (s *Server) TestManualCoverPath(albumID int64) string { return s.manualCoverPath(albumID) }
