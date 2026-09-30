package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// providerNames must never appear in the JSON error of a failed outside
// call: the person can do nothing about them but wait.
var providerNames = []string{"MusicBrainz", "ListenBrainz", "Cover Art", "TMDB", "Trakt", "127.0.0.1", "musicbrainz", "tmdb"}

func assertPlainError(t *testing.T, what string, status int, body map[string]any, wantStatus int, wantPart string) {
	t.Helper()
	msg := fmt.Sprint(body["error"])
	if status != wantStatus || !strings.Contains(msg, wantPart) {
		t.Errorf("%s: %d %q, want %d containing %q", what, status, msg, wantStatus, wantPart)
	}
	for _, name := range providerNames {
		if strings.Contains(msg, name) {
			t.Errorf("%s: the message names %q: %q", what, name, msg)
		}
	}
}

func TestMusicErrorsDoNotNameTheirProvider(t *testing.T) {
	e := newMusicEnv(t)

	// The music database cannot be reached.
	e.server.TestSetMusicBrainz("http://127.0.0.1:1")
	status, body := doStatus(t, e.client, http.MethodGet, e.base+"/api/music/search?q=anything")
	assertPlainError(t, "search while the service is down", status, body, http.StatusBadGateway, "music database")

	// An id it does not know.
	mb := newFakeMusicBrainz(t)
	e.server.TestSetMusicBrainz(mb.URL)
	body = postJSON[map[string]any](t, e.client, e.base+"/api/music/artists", map[string]any{"mbid": "99999999-9999-4999-8999-999999999999"}, http.StatusNotFound)
	assertPlainError(t, "add an unknown artist", http.StatusNotFound, body, http.StatusNotFound, "music database")
}

func TestMovieDatabaseErrorsDoNotNameTheirProvider(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", "http://127.0.0.1:1")
	status, body := doStatus(t, client, http.MethodGet, base+"/api/discover/genres?kind=movie")
	assertPlainError(t, "genres while the service is down", status, body, http.StatusBadGateway, "Try again in a moment")
}
