package music

import (
	"testing"
)

func names(r []Ranked) []string {
	out := make([]string, len(r))
	for i, c := range r {
		out[i] = c.Name + " | " + c.Artist
	}
	return out
}

func TestRankSearch(t *testing.T) {
	tests := []struct {
		name  string
		query string
		cands []SearchCandidate
		// first is the Name of the candidate that must be on top; above lists
		// Names (with their position in cands) that must come before below.
		first string
		order [][2]string // [better, worse] by "Name | Artist"
	}{
		{
			name:  "misspelt and reordered artist name",
			query: "jain anuv",
			cands: []SearchCandidate{
				{Kind: SearchArtist, Name: "Jain", Score: 100, Disambiguation: "French singer"},
				{Kind: SearchArtist, Name: "Anuj Jain", Score: 90},
				{Kind: SearchArtist, Name: "Anuv Jain", Score: 95, Disambiguation: "Indian singer-songwriter"},
				{Kind: SearchArtist, Name: "Anuv Jain Tribute Band", Score: 80, Disambiguation: "tribute"},
			},
			first: "Anuv Jain",
			order: [][2]string{{"Anuv Jain | ", "Anuj Jain | "}, {"Anuj Jain | ", "Jain | "}},
		},
		{
			name:  "swapped letters in the artist name",
			query: "Anuv Jian",
			cands: []SearchCandidate{
				{Kind: SearchArtist, Name: "Anuj Jain", Score: 100},
				{Kind: SearchArtist, Name: "Anuv Jain", Score: 80},
			},
			first: "Anuv Jain",
		},
		{
			name:  "song and artist typed together beats the artist alone",
			query: "Gul Anuv Jain",
			cands: []SearchCandidate{
				{Kind: SearchArtist, Name: "Anuv Jain", Score: 100},
				{Kind: SearchSong, Name: "Baarishein", Artist: "Anuv Jain", Album: "Baarishein", PrimaryType: "Single", Score: 70, Releases: 2},
				{Kind: SearchSong, Name: "Gul", Artist: "Anuv Jain", Album: "Gul", PrimaryType: "Single", Score: 80, Releases: 2},
				{Kind: SearchSong, Name: "Gul", Artist: "Someone Else", Album: "Gul", PrimaryType: "Single", Score: 80, Releases: 1},
			},
			first: "Gul",
			order: [][2]string{{"Gul | Anuv Jain", "Gul | Someone Else"}, {"Anuv Jain | ", "Baarishein | Anuv Jain"}},
		},
		{
			name:  "a film title leads to its soundtrack above singles and namesakes",
			query: "Interstellar",
			cands: []SearchCandidate{
				{Kind: SearchAlbum, Name: "Interstellar", Artist: "Fleeting Echo", Score: 100, PrimaryType: "Single"},
				{Kind: SearchAlbum, Name: "Interstellar", Artist: "Dust Engine", Score: 100, PrimaryType: "Album"},
				{Kind: SearchArtist, Name: "Interstellar", Score: 100},
				{Kind: SearchAlbum, Name: "Interstellar (Original Motion Picture Soundtrack)", Artist: "Hans Zimmer", Score: 96, PrimaryType: "Album", SecondaryTypes: []string{"Soundtrack"}},
				{Kind: SearchAlbum, Name: "Interstellar Remixed", Artist: "DJ Void", Score: 90, PrimaryType: "Album", SecondaryTypes: []string{"Remix"}},
			},
			first: "Interstellar (Original Motion Picture Soundtrack)",
			order: [][2]string{
				{"Interstellar | Dust Engine", "Interstellar | Fleeting Echo"},
				{"Interstellar | Fleeting Echo", "Interstellar Remixed | DJ Void"},
			},
		},
		{
			name:  "the exact album name beats longer names",
			query: "Abbey Road",
			cands: []SearchCandidate{
				{Kind: SearchAlbum, Name: "Abbey Road Sessions", Artist: "Various Artists", Score: 100, PrimaryType: "Album"},
				{Kind: SearchAlbum, Name: "Abbey Road: The Complete Everest Recordings", Artist: "The Beatles", Score: 100, PrimaryType: "Album"},
				{Kind: SearchAlbum, Name: "Abbey Road", Artist: "The Beatles", Score: 95, PrimaryType: "Album"},
			},
			first: "Abbey Road",
		},
		{
			name:  "a live bootleg is below the studio release",
			query: "Dark Side of the Moon",
			cands: []SearchCandidate{
				{Kind: SearchAlbum, Name: "Dark Side of the Moon (Live at Wembley 1974)", Artist: "Pink Floyd", Score: 100, PrimaryType: "Album", SecondaryTypes: []string{"Live"}},
				{Kind: SearchAlbum, Name: "Dark Side of the Moon", Artist: "Pink Floyd Tribute", Score: 100, PrimaryType: "Album", SecondaryTypes: []string{"Compilation"}},
				{Kind: SearchAlbum, Name: "The Dark Side of the Moon", Artist: "Pink Floyd", Score: 98, PrimaryType: "Album"},
			},
			first: "The Dark Side of the Moon",
			order: [][2]string{
				{"The Dark Side of the Moon | Pink Floyd", "Dark Side of the Moon | Pink Floyd Tribute"},
				{"Dark Side of the Moon | Pink Floyd Tribute", "Dark Side of the Moon (Live at Wembley 1974) | Pink Floyd"},
			},
		},
		{
			name:  "asking for live keeps live albums up",
			query: "Nirvana unplugged live",
			cands: []SearchCandidate{
				{Kind: SearchAlbum, Name: "Nevermind", Artist: "Nirvana", Score: 80, PrimaryType: "Album"},
				{Kind: SearchAlbum, Name: "MTV Unplugged in New York", Artist: "Nirvana", Score: 90, PrimaryType: "Album", SecondaryTypes: []string{"Live"}},
			},
			first: "MTV Unplugged in New York",
		},
		{
			name:  "accents and case do not matter",
			query: "BEYONCE lemonade",
			cands: []SearchCandidate{
				{Kind: SearchAlbum, Name: "Lemonade Stand", Artist: "Kids", Score: 90, PrimaryType: "Album"},
				{Kind: SearchAlbum, Name: "Lemonade", Artist: "Beyoncé", Score: 90, PrimaryType: "Album"},
			},
			first: "Lemonade",
		},
		{
			name:  "a song found by its soundtrack album is below a song of that name",
			query: "Cornfield Chase",
			cands: []SearchCandidate{
				{Kind: SearchSong, Name: "Stay", Artist: "Hans Zimmer", Album: "Cornfield Chase Sessions", PrimaryType: "Album", Score: 60, Releases: 1},
				{Kind: SearchSong, Name: "Cornfield Chase", Artist: "Hans Zimmer", Album: "Interstellar", PrimaryType: "Album", SecondaryTypes: []string{"Soundtrack"}, Score: 100, Releases: 4},
			},
			first: "Cornfield Chase",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RankSearch(tc.query, tc.cands)
			if len(got) != len(tc.cands) {
				t.Fatalf("got %d results for %d candidates", len(got), len(tc.cands))
			}
			if got[0].Name != tc.first {
				t.Fatalf("first = %q, want %q\nall: %v", got[0].Name, tc.first, names(got))
			}
			pos := map[string]int{}
			for i, g := range got {
				pos[g.Name+" | "+g.Artist] = i
			}
			for _, pair := range tc.order {
				a, aok := pos[pair[0]]
				b, bok := pos[pair[1]]
				if !aok || !bok {
					t.Fatalf("test data has no %q or %q: %v", pair[0], pair[1], names(got))
				}
				if a > b {
					t.Errorf("%q should be above %q\nall: %v", pair[0], pair[1], names(got))
				}
			}
		})
	}
}

func TestRankSearchKeepsDatabaseOrderForTies(t *testing.T) {
	got := RankSearch("zzz", []SearchCandidate{
		{Kind: SearchAlbum, Name: "Alpha", Ref: 1},
		{Kind: SearchAlbum, Name: "Beta", Ref: 2},
		{Kind: SearchAlbum, Name: "Gamma", Ref: 3},
	})
	for i, g := range got {
		if g.Ref != i+1 {
			t.Fatalf("order changed: %v", got)
		}
	}
}

func TestTokenSimilarity(t *testing.T) {
	tests := []struct {
		q, f string
		min  float64
		max  float64
	}{
		{"jain", "jain", 1, 1},
		{"jian", "jain", 0.6, 0.8},
		{"gul", "gulzar", 0.9, 0.9},
		{"abc", "xyz", 0, 0},
		{"anuv", "anuj", 0.6, 0.8},
		{"ab", "ac", 0, 0},
	}
	for _, tc := range tests {
		got := tokenSimilarity(tc.q, tc.f)
		if got < tc.min || got > tc.max {
			t.Errorf("tokenSimilarity(%q, %q) = %v, want %v to %v", tc.q, tc.f, got, tc.min, tc.max)
		}
	}
}

func TestSearchTokens(t *testing.T) {
	got := searchTokens("Beyoncé & Jay-Z: Everything Is Love!")
	want := []string{"beyonce", "and", "jay", "z", "everything", "is", "love"}
	if !equalTokens(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
