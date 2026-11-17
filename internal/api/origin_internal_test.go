package api

import (
	"context"
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
		want origin.Origin
	}{
		{"a release a person picked", grabPicked, anna, anna},
		{"picked, person unknown", grabPicked, origin.Origin{}, origin.Origin{Kind: origin.Manual}},
		{"a search a person started", grabSearchNow, annaSearch, annaSearch},
		{"a search started by hand, person unknown", grabSearchNow, origin.Origin{}, origin.Origin{Kind: origin.Search}},
		{"the scheduled search", grabAutomatic, origin.Origin{}, origin.Origin{Kind: origin.Auto, Name: origin.JobSearch}},
		{"the RSS check says so itself", grabAutomatic, rss, rss},
		{"a list's search is the list's", grabAutomatic, list, list},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			if got := s.grabOrigin(tc.kind, tc.by, ""); got != tc.want {
				t.Fatalf("grabOrigin = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Who is behind a run travels with that run: two runs for the same title at
// once each keep their own, one ending takes nothing from the other, and a
// scheduled search running meanwhile is not credited to either.
func TestGrabByIsPerRun(t *testing.T) {
	s := &Server{}
	listA := origin.Origin{Kind: origin.List, Name: "A"}
	anna := origin.Origin{Kind: origin.Search, UserID: 3, User: "Anna"}
	runA, cancelA := context.WithCancel(withGrabBy(context.Background(), listA))
	runAnna := withGrabBy(context.Background(), anna)
	scheduled := context.Background()
	cancelA() // the list's run is over
	tests := []struct {
		name string
		ctx  context.Context
		kind grabKind
		want origin.Origin
	}{
		{"the list's run", runA, grabAutomatic, listA},
		{"Anna's search for the same title", runAnna, grabSearchNow, anna},
		{"the scheduled search meanwhile", scheduled, grabAutomatic, origin.Origin{Kind: origin.Auto, Name: origin.JobSearch}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.grabOrigin(tc.kind, grabByFrom(tc.ctx), ""); got != tc.want {
				t.Fatalf("grabOrigin = %+v, want %+v", got, tc.want)
			}
		})
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
