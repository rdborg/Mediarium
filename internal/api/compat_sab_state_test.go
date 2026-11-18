package api

import "testing"

// The word for the whole queue that SABnzbd gives and dashboards show.
func TestSabQueueState(t *testing.T) {
	tests := []struct {
		name            string
		slots           int
		paused, running bool
		want            string
	}{
		{"nothing in it", 0, false, false, "Idle"},
		{"nothing in it, paused flag", 0, true, false, "Idle"},
		{"all paused", 2, true, false, "Paused"},
		{"one running", 2, false, true, "Downloading"},
		{"waiting only", 1, false, false, "Idle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sabQueueState(tt.slots, tt.paused, tt.running); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
