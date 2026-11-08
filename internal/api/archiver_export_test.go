package api

import (
	"context"
	"time"
)

// TestArchiverCheck runs one check of a source and waits for it.
func (s *Server) TestArchiverCheck(id int64) bool {
	ctx, done, ok := s.archiverBegin(context.Background(), id)
	if !ok {
		return false
	}
	defer done()
	s.archiverCheck(ctx, id)
	return true
}

// TestArchiverJob runs the scheduled job once.
func (s *Server) TestArchiverJob() { s.archiverJob(context.Background()) }

// TestArchiverNotBefore sets the first moment the job may act.
func (s *Server) TestArchiverNotBefore(t time.Time) { s.archiver.notBefore = t }

// TestArchiverAge makes a source look as if it was last checked at t.
func (s *Server) TestArchiverAge(id int64, t time.Time) {
	_, _ = s.db.Exec(`UPDATE archiver_sources SET last_checked = ? WHERE id = ?`, t.UTC().Format(time.RFC3339), id)
}
