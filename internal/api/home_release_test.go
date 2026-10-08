package api

import (
	"testing"
	"time"
)

func TestHomeHoldUntil(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	zero := time.Time{}
	tests := []struct {
		name                string
		theatrical, home    time.Time
		now                 time.Time
		wantHold, wantEstim bool
		wantUntil           string
	}{
		{"in cinemas, digital date still ahead", day("2026-09-01"), day("2026-11-10"), day("2026-10-08"), true, false, "2026-11-10"},
		{"digital date reached", day("2026-08-01"), day("2026-09-25"), day("2026-10-08"), false, false, "2026-09-25"},
		{"no digital date yet: wait 90 days from cinemas", day("2026-09-01"), zero, day("2026-10-08"), true, true, "2026-11-30"},
		{"no digital date, 90 days passed", day("2026-06-01"), zero, day("2026-10-08"), false, true, "2026-08-30"},
		{"no cinema date (a streaming film): never held", zero, day("2026-10-01"), day("2026-10-08"), false, false, ""},
		{"long out: never held", day("2025-01-01"), zero, day("2026-10-08"), false, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			until, estimated, hold := homeHoldUntil(tc.theatrical, tc.home, tc.now)
			if hold != tc.wantHold || estimated != tc.wantEstim {
				t.Fatalf("hold = %v, estimated = %v; want %v, %v", hold, estimated, tc.wantHold, tc.wantEstim)
			}
			if tc.wantUntil != "" && until.Format("2006-01-02") != tc.wantUntil {
				t.Fatalf("until = %s, want %s", until.Format("2006-01-02"), tc.wantUntil)
			}
		})
	}
}
