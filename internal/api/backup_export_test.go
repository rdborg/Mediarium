package api

import "time"

// TestSetRestartDelay shortens the pause between answering a restore request
// and calling the exit function, so backup_test.go does not wait 1.5 s.
func (s *Server) TestSetRestartDelay(d time.Duration) { s.restartDelay = d }
