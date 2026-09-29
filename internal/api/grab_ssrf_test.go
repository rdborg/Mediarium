package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
)

// doJSONStatus sends body as JSON and returns the status, whatever it is.
func doJSONStatus(t *testing.T, client *http.Client, method, target string, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, target, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestMemberGrabOnlyFetchesOfferedURLs:a member's grab may only make the
// server fetch a download URL it handed out in search results, or one on a
// configured indexer's host; anything else (cloud metadata, the LAN) is
// refused before any request is made.
func TestMemberGrabOnlyFetchesOfferedURLs(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	indexer := newTVIndexerWith(t, []string{"Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP"})
	postJSON[map[string]any](t, admin, base+"/api/indexers", map[string]any{
		"name": "Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "k",
	}, http.StatusCreated)

	// A second indexer-like server the member never saw results from.
	other := newTVIndexerWith(t, nil)
	otherURL, _ := url.Parse(other.URL)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999})
	if err != nil {
		t.Fatal(err)
	}
	series, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 1399, Title: "Fixture Show", Year: 2011}, nil)
	if err != nil {
		t.Fatal(err)
	}

	grab := func(client *http.Client, path, downloadURL string) int {
		t.Helper()
		status, _ := doJSONStatus(t, client, http.MethodPost, base+path, map[string]any{
			"releaseTitle": "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP", "downloadUrl": downloadURL, "sizeBytes": 1000,
		})
		return status
	}
	endpoints := []string{
		"/api/search/grab",
		fmt.Sprintf("/api/movies/%d/grab", movie.ID),
		fmt.Sprintf("/api/series/%d/grab", series.ID),
	}

	refused := []string{
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/",
		"http://" + otherURL.Host + "/nzb/0.nzb",
		"http://localhost:8264/api/system/backup",
		"file:///etc/passwd",
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=http://10.0.0.1/announce",
	}
	for _, ep := range endpoints {
		for _, u := range refused {
			if status := grab(member, ep, u); status != http.StatusForbidden {
				t.Errorf("member %s with %s: want 403, got %d", ep, u, status)
			}
		}
	}

	// After a search, the result's URL is accepted (the request then goes on
	// to its usual checks: here the TMDB key is missing, so 412).
	results := getJSON[[]map[string]any](t, member, base+"/api/search?q=Fixture")
	if len(results) != 1 {
		t.Fatalf("search results: %+v", results)
	}
	offered := results[0]["downloadUrl"].(string)
	if status := grab(member, "/api/search/grab", offered); status == http.StatusForbidden {
		t.Fatalf("a URL from the search results should be allowed, got %d", status)
	}

	// A URL on a configured indexer's host is accepted without a search.
	if status := grab(member, "/api/search/grab", indexer.URL+"/getnzb/99.nzb"); status == http.StatusForbidden {
		t.Fatalf("a URL on the indexer's host should be allowed, got %d", status)
	}

	// Administrators are not limited (they can point an indexer anywhere anyway).
	if status := grab(admin, "/api/search/grab", "http://169.254.169.254/latest/"); status == http.StatusForbidden {
		t.Fatalf("an administrator's grab should not be refused by the URL check, got %d", status)
	}
}
