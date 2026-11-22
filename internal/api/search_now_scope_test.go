package api

import "testing"

// The "Find missing" button on a season sends that season's number; leaving
// it out searches the whole show, and 0 means the specials only.
func TestSearchNowScope(t *testing.T) {
	two, zero := 2, 0
	cases := []struct {
		name string
		req  seriesSearchNowRequest
		want tvScope
	}{
		{"whole show keeps the monitored boxes", seriesSearchNowRequest{}, tvScope{seriesID: 7, force: true, asMarked: true}},
		{"one season", seriesSearchNowRequest{Season: &two}, tvScope{seriesID: 7, season: 2, force: true}},
		{"one episode", seriesSearchNowRequest{Season: &two, Episode: 5}, tvScope{seriesID: 7, season: 2, episode: 5, force: true}},
		{"specials", seriesSearchNowRequest{Season: &zero}, tvScope{seriesID: 7, specials: true, force: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := searchNowScope(7, c.req); got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
