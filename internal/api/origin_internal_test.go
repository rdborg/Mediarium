package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/origin"
)

func TestGrabOrigin(t *testing.T) {
	anna := origin.Origin{Kind: origin.Manual, UserID: 3, User: "Anna"}
	annaSearch := origin.Origin{Kind: origin.Search, UserID: 3, User: "Anna"}
	list := origin.Origin{Kind: origin.List, Name: "Trending"}
	rss := origin.Origin{Kind: origin.Auto, Name: origin.JobRSS}
	tests := []struct {
		name string
		kind grabKind
		by   origin.Origin
		hint *origin.Origin // set for movie 7 while the grab runs
		want origin.Origin
	}{
		{"a release a person picked", grabPicked, anna, nil, anna},
		{"picked, person unknown", grabPicked, origin.Origin{}, nil, origin.Origin{Kind: origin.Manual}},
		{"a search a person started", grabSearchNow, annaSearch, nil, annaSearch},
		{"a search started by hand, person unknown", grabSearchNow, origin.Origin{}, nil, origin.Origin{Kind: origin.Search}},
		{"the scheduled search", grabAutomatic, origin.Origin{}, nil, origin.Origin{Kind: origin.Auto, Name: origin.JobSearch}},
		{"the RSS check says so itself", grabAutomatic, rss, nil, rss},
		{"a list's search is the list's", grabAutomatic, origin.Origin{}, &list, list},
		{"a person's search beats the schedule", grabSearchNow, origin.Origin{}, &annaSearch, annaSearch},
		{"what the caller says beats a hint", grabAutomatic, rss, &list, rss},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			if tc.hint != nil {
				defer s.hintGrabs(false, 7, *tc.hint)()
			}
			if got := s.grabOrigin(tc.kind, tc.by, false, 7, ""); got != tc.want {
				t.Fatalf("grabOrigin = %+v, want %+v", got, tc.want)
			}
			// A hint for a movie is not a hint for a show with the same id.
			if tc.hint != nil && !tc.by.Known() {
				if got := s.grabOrigin(tc.kind, tc.by, true, 7, ""); got == *tc.hint {
					t.Fatalf("a movie's hint reached a show: %+v", got)
				}
			}
		})
	}
}

func TestHintGrabsEnds(t *testing.T) {
	s := &Server{}
	done := s.hintGrabs(true, 4, origin.Origin{Kind: origin.List, Name: "A"})
	if got := s.grabOrigin(grabAutomatic, origin.Origin{}, true, 4, ""); got.Kind != origin.List {
		t.Fatalf("during the search: %+v", got)
	}
	done()
	if got := s.grabOrigin(grabAutomatic, origin.Origin{}, true, 4, ""); got.Kind != origin.Auto {
		t.Fatalf("after the search: %+v", got)
	}
}

func TestOriginOut(t *testing.T) {
	if originOut(origin.Origin{}, &userRef{ID: 1, Name: "x"}) != nil {
		t.Fatal("no origin recorded must give nothing")
	}
	v := originOut(origin.Origin{Kind: origin.Manual, UserID: 5, User: "Gone"}, nil)
	if v == nil || v.User == nil || v.User.Name != "Gone" {
		t.Fatalf("a removed account still shows its name on a download: %+v", v)
	}
	v = originOut(origin.Origin{Kind: origin.Manual}, &userRef{ID: 2, Name: "Anna"})
	if v == nil || v.User == nil || v.User.Name != "Anna" {
		t.Fatalf("a title's person comes from its account: %+v", v)
	}
}
