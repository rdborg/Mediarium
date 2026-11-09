package musicbrainz

import "testing"

func TestSearchQueries(t *testing.T) {
	tests := []struct {
		name   string
		build  func(string, string) string
		field  string
		text   string
		expect string
	}{
		{"strict needs every word in the title or the artist", strictQuery, "releasegroup", "Gul Anuv Jain",
			"(releasegroup:Gul OR artist:Gul) AND (releasegroup:Anuv OR artist:Anuv) AND (releasegroup:Jain OR artist:Jain)"},
		{"strict leaves punctuation out", strictQuery, "recording", "AC/DC - Back in Black",
			"(recording:AC OR artist:AC) AND (recording:DC OR artist:DC) AND (recording:Back OR artist:Back) AND (recording:in OR artist:in) AND (recording:Black OR artist:Black)"},
		{"loose allows near spellings of longer words", looseQuery, "releasegroup", "Gul Anuv Jian",
			"releasegroup:Gul OR artist:Gul OR releasegroup:Anuv~ OR artist:Anuv~ OR releasegroup:Jian~ OR artist:Jian~"},
		{"only punctuation falls back to the escaped text", strictQuery, "releasegroup", "?!", `releasegroup:(\?\!)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.build(tc.field, tc.text); got != tc.expect {
				t.Errorf("got  %s\nwant %s", got, tc.expect)
			}
		})
	}
}

func TestMergeFoundKeepsTheFirstAndAddsNewOnes(t *testing.T) {
	a := []FoundAlbum{{ReleaseGroup: ReleaseGroup{ID: "1"}, Song: "x"}}
	b := []FoundAlbum{{ReleaseGroup: ReleaseGroup{ID: "1"}, Song: "x"}, {ReleaseGroup: ReleaseGroup{ID: "1"}, Song: "y"}, {ReleaseGroup: ReleaseGroup{ID: "2"}}}
	got := mergeFound(a, b, 10)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3: %+v", len(got), got)
	}
	if len(mergeFound(a, b, 1)) != 1 {
		t.Fatal("limit not respected")
	}
}
