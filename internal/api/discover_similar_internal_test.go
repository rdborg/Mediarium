package api

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

var similarNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// cand builds a fake suggestion. Popularity is TMDB's own measure.
func cand(id, year int, pop float64, poster bool) similarCandidate {
	return similarCandidate{ID: id, Year: year, Popularity: pop, HasPoster: poster, Votes: 1, order: id}
}

func ids(list []similarCandidate) []int {
	out := make([]int, len(list))
	for i, c := range list {
		out[i] = c.ID
	}
	return out
}

func TestRankSimilar(t *testing.T) {
	tests := []struct {
		name  string
		cands []similarCandidate
		opt   similarOptions
		want  []int
	}{
		{
			name:  "recent beats old at the same popularity when older titles are on",
			cands: []similarCandidate{cand(1, 1972, 50, true), cand(2, 2024, 50, true), cand(3, 2005, 50, true)},
			opt:   similarOptions{Now: similarNow, Older: true},
			want:  []int{2, 3, 1},
		},
		{
			name:  "titles from before the last 15 years are left out by default",
			cands: []similarCandidate{cand(1, 1972, 500, true), cand(2, 2005, 500, true), cand(3, 2011, 20, true), cand(4, 2012, 20, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{4, 3},
		},
		{
			name:  "the oldest year allowed is this year minus 15",
			cands: []similarCandidate{cand(1, 2010, 10, true), cand(2, 2011, 10, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{2},
		},
		{
			name:  "more popular wins in the same year",
			cands: []similarCandidate{cand(1, 2022, 5, true), cand(2, 2022, 400, true), cand(3, 2022, 60, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{2, 3, 1},
		},
		{
			name:  "very popular older title can still beat an unknown new one",
			cands: []similarCandidate{cand(1, 2026, 1, true), cand(2, 2016, 900, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{2, 1},
		},
		{
			name:  "titles already in the library are never listed",
			cands: []similarCandidate{cand(1, 2024, 900, true), cand(2, 2024, 10, true)},
			opt:   similarOptions{Now: similarNow, Library: map[int]bool{1: true}},
			want:  []int{2},
		},
		{
			name:  "titles without a poster come after those with one",
			cands: []similarCandidate{cand(1, 2025, 900, false), cand(2, 2015, 3, true), cand(3, 2024, 40, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{3, 2, 1},
		},
		{
			name:  "no release year, no place",
			cands: []similarCandidate{cand(1, 0, 900, true), cand(2, 2020, 1, true)},
			opt:   similarOptions{Now: similarNow, Older: true},
			want:  []int{2},
		},
		{
			name:  "not out yet counts as new",
			cands: []similarCandidate{cand(1, 2027, 30, true), cand(2, 2020, 30, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{1, 2},
		},
		{
			name: "more library titles pointing to it lifts it",
			cands: []similarCandidate{
				cand(1, 2022, 30, true),
				{ID: 2, Year: 2022, Popularity: 30, HasPoster: true, Votes: 4, order: 2},
			},
			opt:  similarOptions{Now: similarNow},
			want: []int{2, 1},
		},
		{
			name:  "same score keeps the order they came in",
			cands: []similarCandidate{cand(5, 2022, 30, true), cand(3, 2022, 30, true), cand(4, 2022, 30, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{3, 4, 5},
		},
		{
			name:  "the limit cuts the list after ranking",
			cands: []similarCandidate{cand(1, 2020, 1, true), cand(2, 2024, 500, true), cand(3, 2023, 100, true)},
			opt:   similarOptions{Now: similarNow, Limit: 2},
			want:  []int{2, 3},
		},
		{
			name:  "nothing to show gives an empty list",
			cands: []similarCandidate{cand(1, 1990, 100, true)},
			opt:   similarOptions{Now: similarNow},
			want:  []int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(rankSimilar(tt.cands, tt.opt))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// With older titles on, a library that suggests a lot of 1972 to 2005 titles
// must still show the newer ones first.
func TestRankSimilarOlderSwitchKeepsRecentFirst(t *testing.T) {
	var cands []similarCandidate
	for i := 0; i < 20; i++ {
		cands = append(cands, cand(100+i, 1972+i*2, 60, true))
	}
	cands = append(cands, cand(1, 2023, 60, true), cand(2, 2021, 60, true))

	if got := rankSimilar(cands, similarOptions{Now: similarNow}); len(got) != 2 || got[0].ID != 1 {
		t.Fatalf("default should list only the two recent titles, newest first: %v", ids(got))
	}
	got := ids(rankSimilar(cands, similarOptions{Now: similarNow, Older: true}))
	if got[0] != 1 || got[1] != 2 {
		t.Fatalf("with older titles on, recent ones still come first: %v", got)
	}
	// And the older ones follow from newest to oldest.
	year := map[int]int{}
	for _, c := range cands {
		year[c.ID] = c.Year
	}
	for i := 3; i < len(got); i++ {
		if year[got[i]] > year[got[i-1]] {
			t.Fatalf("older titles out of order: %v", got)
		}
	}
}

func TestMergeSuggestions(t *testing.T) {
	a := func(id int) similarCandidate { return cand(id, 2020, 10, true) }
	tests := []struct {
		name      string
		perSeed   [][]similarCandidate
		wantIDs   []int
		wantVotes map[int]int
	}{
		{
			name:      "titles suggested by several seeds are listed once and add up",
			perSeed:   [][]similarCandidate{{a(1), a(2), a(3)}, {a(3), a(4)}, {a(3), a(2)}},
			wantIDs:   []int{1, 2, 3, 4},
			wantVotes: map[int]int{1: 1, 2: 2, 3: 3, 4: 1},
		},
		{
			name:      "one seed suggesting a title twice counts once",
			perSeed:   [][]similarCandidate{{a(1), a(1), a(2)}, {a(2)}},
			wantIDs:   []int{1, 2},
			wantVotes: map[int]int{1: 1, 2: 2},
		},
		{
			name:      "a seed that gave nothing changes nothing",
			perSeed:   [][]similarCandidate{nil, {a(7)}},
			wantIDs:   []int{7},
			wantVotes: map[int]int{7: 1},
		},
		{
			name:      "no id, no place",
			perSeed:   [][]similarCandidate{{a(0), a(9)}},
			wantIDs:   []int{9},
			wantVotes: map[int]int{9: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeSuggestions(tt.perSeed)
			if !reflect.DeepEqual(ids(got), tt.wantIDs) {
				t.Fatalf("ids: got %v, want %v", ids(got), tt.wantIDs)
			}
			for _, c := range got {
				if c.Votes != tt.wantVotes[c.ID] {
					t.Errorf("title %d: %d votes, want %d", c.ID, c.Votes, tt.wantVotes[c.ID])
				}
			}
		})
	}
}

func TestPickSeeds(t *testing.T) {
	many := make([]int, 100)
	for i := range many {
		many[i] = i + 1
	}
	tests := []struct {
		name    string
		ids     []int
		n       int
		wantLen int
	}{
		{"few titles are all used", []int{5, 6, 7}, 10, 3},
		{"exactly enough", many[:10], 10, 10},
		{"a big library gives n seeds", many, 10, 10},
		{"no titles", nil, 10, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickSeeds(tt.ids, tt.n, 20000)
			if len(got) != tt.wantLen {
				t.Fatalf("got %d seeds, want %d", len(got), tt.wantLen)
			}
			seen := map[int]bool{}
			for _, id := range got {
				if seen[id] {
					t.Fatalf("seed %d twice", id)
				}
				seen[id] = true
				if len(tt.ids) > similarSeedPool && id > similarSeedPool {
					t.Fatalf("seed %d is outside the %d newest", id, similarSeedPool)
				}
			}
		})
	}

	t.Run("same day, same seeds", func(t *testing.T) {
		if a, b := pickSeeds(many, 10, 20000), pickSeeds(many, 10, 20000); !reflect.DeepEqual(a, b) {
			t.Fatalf("%v != %v", a, b)
		}
	})
	t.Run("another day, other seeds", func(t *testing.T) {
		days := map[string]bool{}
		for d := 20000; d < 20010; d++ {
			days[fmt.Sprint(pickSeeds(many, 10, d))] = true
		}
		if len(days) < 2 {
			t.Fatalf("the seeds never change from one day to the next")
		}
	})
}
