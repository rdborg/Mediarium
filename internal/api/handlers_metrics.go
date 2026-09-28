package api

import (
	"fmt"
	"net/http"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/queue"
)

// handleMetrics exposes a Prometheus-format /metrics endpoint (PRD §7 —
// "cheap to add, useful for NAS users running Grafana"). Written by hand
// with plain fmt.Fprintf rather than pulling in the Prometheus client
// library: the exposition format is simple text and this endpoint has
// exactly two counters, so a dependency would outweigh the few lines it
// saves — worth revisiting if metrics coverage grows substantially.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	movies, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queueItems, err := s.QueueRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	moviesByStatus := map[library.Status]int{}
	for _, m := range movies {
		moviesByStatus[m.Status]++
	}
	queueByStatus := map[queue.Status]int{}
	for _, q := range queueItems {
		queueByStatus[q.Status]++
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintln(w, "# HELP mediarium_up Whether the Mediarium API is up.")
	fmt.Fprintln(w, "# TYPE mediarium_up gauge")
	fmt.Fprintln(w, "mediarium_up 1")

	fmt.Fprintln(w, "# HELP mediarium_library_movies_total Movies in the library by status.")
	fmt.Fprintln(w, "# TYPE mediarium_library_movies_total gauge")
	for _, status := range []library.Status{library.StatusMissing, library.StatusDownloading, library.StatusDownloaded} {
		fmt.Fprintf(w, "mediarium_library_movies_total{status=%q} %d\n", status, moviesByStatus[status])
	}

	fmt.Fprintln(w, "# HELP mediarium_queue_items_total Download queue items by status.")
	fmt.Fprintln(w, "# TYPE mediarium_queue_items_total gauge")
	for _, status := range []queue.Status{queue.StatusQueued, queue.StatusDownloading, queue.StatusImporting, queue.StatusCompleted, queue.StatusFailed} {
		fmt.Fprintf(w, "mediarium_queue_items_total{status=%q} %d\n", status, queueByStatus[status])
	}
}
