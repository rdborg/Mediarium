package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestCalendarFeedWorksWithoutSignInAndCanBeTurnedOff(t *testing.T) {
	server, base, client := loginNewServer(t)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix, Reloaded; Again", Year: 2003, Monitored: true, ReleaseDate: "2026-11-05"}); err != nil {
		t.Fatal(err)
	}

	type feed struct {
		On   bool   `json:"on"`
		Path string `json:"path"`
	}
	if f := getJSON[feed](t, client, base+"/api/calendar/feed"); f.On {
		t.Fatal("the feed should start off")
	}
	f := postJSON[feed](t, client, base+"/api/calendar/feed", nil, http.StatusOK)
	if !f.On || !strings.HasPrefix(f.Path, "/api/calendar/feed/") || !strings.HasSuffix(f.Path, ".ics") {
		t.Fatalf("feed: %+v", f)
	}

	// A calendar app has no cookie.
	resp, err := http.Get(base + f.Path)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	ics := string(body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ics, "BEGIN:VCALENDAR\r\n") || !strings.Contains(ics, "DTSTART;VALUE=DATE:20261105") || !strings.Contains(ics, `The Matrix\, Reloaded\; Again`) {
		t.Fatalf("status %d, feed:\n%s", resp.StatusCode, ics)
	}

	// A new link replaces the old one; turning it off stops it.
	g := postJSON[feed](t, client, base+"/api/calendar/feed", nil, http.StatusOK)
	for _, tc := range []struct {
		path string
		want int
	}{{f.Path, http.StatusNotFound}, {g.Path, http.StatusOK}, {"/api/calendar/feed/abc.ics", http.StatusNotFound}} {
		resp, err := http.Get(base + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
	}
	req, _ := http.NewRequest(http.MethodDelete, base+"/api/calendar/feed", nil)
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
	resp, err = http.Get(base + g.Path)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after turning it off: status %d", resp.StatusCode)
	}
}
