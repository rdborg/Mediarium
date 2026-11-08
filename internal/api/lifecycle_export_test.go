package api

import "time"

// TestAgeLifecycleFlags makes every title on the Leaving soon shelf look like
// it has waited out its grace period.
func (s *Server) TestAgeLifecycleFlags() {
	at := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	_, _ = s.db.Exec(`UPDATE lifecycle_flags SET due_at = ?`, at)
}

// TestRunLifecycle runs the lifecycle rules once, now.
func (s *Server) TestRunLifecycle() error {
	_, err := s.lifecycleRun(time.Now())
	return err
}
