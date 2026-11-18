package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// The "Searches used" part counts each reason once per release, puts the most
// frequent first, and keeps ties in the order they were first seen.
func TestTallyReasonsCountsOncePerReleaseAndKeepsOrder(t *testing.T) {
	tests := []struct {
		name string
		in   [][]string
		pre  []filteredCount
		want []filteredCount
	}{
		{"nothing", nil, nil, nil},
		{"one release, one reason", [][]string{{"too small"}}, nil, []filteredCount{{"too small", 1}}},
		{"a reason named twice counts once", [][]string{{"too small", "too small"}}, nil, []filteredCount{{"too small", 1}}},
		{"empty reasons are ignored", [][]string{{"", "blocklisted"}}, nil, []filteredCount{{"blocklisted", 1}}},
		{"most frequent first", [][]string{{"a"}, {"b"}, {"b"}, {"c"}, {"c"}, {"c"}}, nil, []filteredCount{{"c", 3}, {"b", 2}, {"a", 1}}},
		{"ties keep the order first seen", [][]string{{"x"}, {"y"}, {"z"}}, nil, []filteredCount{{"x", 1}, {"y", 1}, {"z", 1}}},
		{"two reasons on one release count for both", [][]string{{"a", "b"}, {"b"}}, nil, []filteredCount{{"b", 2}, {"a", 1}}},
		{"earlier groups stay where they are", [][]string{{"a"}, {"a"}}, []filteredCount{{"not this title", 1}}, []filteredCount{{"not this title", 1}, {"a", 2}}},
		{"a reason that is also in an earlier group is counted apart", [][]string{{"same"}}, []filteredCount{{"same", 5}}, []filteredCount{{"same", 5}, {"same", 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pre := append([]filteredCount(nil), tt.pre...)
			got := tallyReasons(pre, tt.in)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			// Same input, same output, every time.
			again := tallyReasons(append([]filteredCount(nil), tt.pre...), tt.in)
			if fmt.Sprint(again) != fmt.Sprint(got) {
				t.Errorf("second run gave %v", again)
			}
		})
	}
}

func TestSearchedLineStatuses(t *testing.T) {
	boom := errors.New("connection refused")
	tests := []struct {
		name       string
		count      int
		outs       []indexers.Outcome
		wantStatus string
		wantCode   string
	}{
		{"answered with releases", 3, []indexers.Outcome{{IndexerName: "A", Query: "q"}}, searchFound, ""},
		{"answered with nothing", 0, []indexers.Outcome{{IndexerName: "A", Query: "q"}}, searchEmpty, ""},
		{"first query failed, second answered", 0, []indexers.Outcome{{IndexerName: "A", Err: boom}, {IndexerName: "A"}}, searchEmpty, ""},
		{"first query failed, second answered with releases", 2, []indexers.Outcome{{IndexerName: "A", Err: boom}, {IndexerName: "A"}}, searchFound, ""},
		{"every query failed", 0, []indexers.Outcome{{IndexerName: "A", Err: boom}, {IndexerName: "A", Err: boom}}, searchFailed, "unreachable"},
		{"timed out", 0, []indexers.Outcome{{IndexerName: "A", Err: context.DeadlineExceeded}}, searchFailed, "timeout"},
		{"paused", 0, []indexers.Outcome{{IndexerName: "A", Skipped: true}}, searchSkipped, "paused"},
		{"daily limit", 0, []indexers.Outcome{{IndexerName: "A", Skipped: true, LimitNote: "100 of 100 used"}}, searchSkipped, "daily_limit"},
		{"skipped but its releases are in the list", 4, []indexers.Outcome{{IndexerName: "A", Skipped: true}}, searchFound, ""},
		{"failed but its releases are in the list", 4, []indexers.Outcome{{IndexerName: "A", Err: boom}}, searchFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchedLine(searchedIndexer{Name: "A", Count: tt.count}, tt.outs)
			if got.Status != tt.wantStatus || got.Code != tt.wantCode {
				t.Errorf("status %q code %q, want %q %q", got.Status, got.Code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}
