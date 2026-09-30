package api

import (
	"context"

	"github.com/rdborg/mediarium/internal/queue"
)

// The pieces of the download line and the pipeline that pipeline_races_test.go
// (package api_test) drives directly.

// TestStartQueued is what the download line does with an item it has just
// taken out of the line.
func (s *Server) TestStartQueued(item queue.Item, done func()) error {
	return s.startQueued(item, done)
}

// TestLaunch runs a function as an item's pipeline, the way the line does.
func (s *Server) TestLaunch(item queue.Item, done func(), run func()) {
	s.launch(item, done, run)
}

// TestPreparePipeline is the set-up half of starting an item.
func (s *Server) TestPreparePipeline(item queue.Item) (func(), error) {
	return s.preparePipeline(item)
}

// TestBeginPipeline registers a pipeline for queueID as a real one does, and
// returns its context (cancelled when the title is removed) and the function
// that ends it.
func (s *Server) TestBeginPipeline(queueID int64) (context.Context, func()) {
	ctx, run := s.pipelines.begin(queueID)
	return ctx, func() { s.pipelines.end(queueID, run) }
}
