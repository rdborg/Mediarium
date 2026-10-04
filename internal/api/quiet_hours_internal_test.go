package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

func TestUrgentMessagesIgnoreQuietHours(t *testing.T) {
	cases := []struct {
		typ    string
		urgent bool
	}{
		{"failed", true}, {"health", true}, {"conflict", true}, {"test", true},
		{"imported", false}, {"grabbed", false}, {"subtitle", false}, {"update", false},
	}
	for _, tc := range cases {
		if got := urgentEvent(notify.Event{Type: tc.typ}); got != tc.urgent {
			t.Errorf("%s: urgent = %v, want %v", tc.typ, got, tc.urgent)
		}
	}
	_ = time.Now
}
