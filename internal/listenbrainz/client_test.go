package listenbrainz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/listenbrainz"
)

func newClient(t *testing.T, h http.HandlerFunc, opts ...listenbrainz.Option) (*listenbrainz.Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if ua := r.Header.Get("User-Agent"); ua != "Mediarium/9.9 ( https://github.com/rdborg/Mediarium )" {
			t.Errorf("User-Agent = %q", ua)
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return listenbrainz.New("9.9", append([]listenbrainz.Option{listenbrainz.WithBaseURL(srv.URL)}, opts...)...), &hits
}

func TestTopReleaseGroupsMergesReleasesOfOneGroup(t *testing.T) {
	c, hits := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/stats/sitewide/release-groups" || r.URL.Query().Get("range") != "year" || r.URL.Query().Get("offset") != "100" {
			t.Errorf("request: %s", r.URL)
		}
		w.Write([]byte(`{"payload":{"release_groups":[
			{"release_group_mbid":"a","release_group_name":"A","artist_name":"X","artist_mbids":["x1"],"listen_count":5},
			{"release_group_mbid":"b","release_group_name":"B","artist_name":"Y","artist_mbids":[],"listen_count":8},
			{"release_group_mbid":"a","release_group_name":"A","artist_name":"X","artist_mbids":["x1"],"listen_count":4},
			{"release_group_mbid":"","release_group_name":"no id","listen_count":99}]}}`))
	})
	got, err := c.TopReleaseGroups(context.Background(), "year", 100, 100)
	if err != nil || len(got) != 2 || got[0].MBID != "a" || got[0].ListenCount != 9 || got[1].MBID != "b" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := c.TopReleaseGroups(context.Background(), "year", 100, 100); err != nil || hits.Load() != 1 {
		t.Fatalf("second call must come from the cache: hits %d, err %v", hits.Load(), err)
	}
}

func TestBadRangeIsRefusedWithoutAskingTheServer(t *testing.T) {
	c, hits := newClient(t, func(http.ResponseWriter, *http.Request) {})
	if _, err := c.TopReleaseGroups(context.Background(), "decade", 10, 0); !errors.Is(err, listenbrainz.ErrRange) {
		t.Fatalf("release groups: %v", err)
	}
	if _, err := c.TopArtists(context.Background(), "", 10, 0); !errors.Is(err, listenbrainz.ErrRange) {
		t.Fatalf("artists: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server was asked %d times", hits.Load())
	}
}

func TestStatusesAndEmptyAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
		want    int
	}{
		{"no content means not calculated yet", http.StatusNoContent, "", false, 0},
		{"server error", http.StatusInternalServerError, "oops", true, 0},
		{"bad request", http.StatusBadRequest, `{"error":"Invalid range"}`, true, 0},
		{"not json", http.StatusOK, "<html>", true, 0},
		{"one artist", http.StatusOK, `{"payload":{"artists":[{"artist_mbid":"m","artist_name":"N","listen_count":3},{"artist_mbid":"","artist_name":"nameless"}]}}`, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			got, err := c.TopArtists(context.Background(), "week", 10, 0)
			if (err != nil) != tc.wantErr || len(got) != tc.want {
				t.Fatalf("got %+v, err %v", got, err)
			}
			if err != nil && (strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), "?")) {
				t.Fatalf("the error must not carry the address: %v", err)
			}
		})
	}
}

func TestFreshReleasesReadsBothAnswerShapes(t *testing.T) {
	release := `{"release_group_mbid":"rg","release_name":"T","artist_credit_name":"A","artist_mbids":["am"],"release_date":"2026-01-02","release_group_primary_type":"EP","release_tags":["x"]}`
	for name, body := range map[string]string{
		"wrapped": `{"payload":{"releases":[` + release + `,` + release + `],"total_count":2}}`,
		"array":   `[` + release + `]`,
		"empty":   `{"payload":{"releases":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("days") != "90" || q.Get("future") != "true" || q.Get("past") != "false" {
					t.Errorf("query: %s", r.URL.RawQuery)
				}
				w.Write([]byte(body))
			})
			got, err := c.FreshReleases(context.Background(), 90, true)
			if err != nil {
				t.Fatal(err)
			}
			if name == "empty" {
				if len(got) != 0 {
					t.Fatalf("got %+v", got)
				}
				return
			}
			// The same release group listed twice is one entry.
			if len(got) != 1 || got[0].MBID != "rg" || got[0].PrimaryType != "EP" || got[0].Date != "2026-01-02" || len(got[0].Tags) != 1 {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestReleaseGroupInfosBatchesAndPicksGenres(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		ids := strings.Split(r.URL.Query().Get("release_group_mbids"), ",")
		if len(ids) > 50 {
			t.Errorf("a batch of %d ids", len(ids))
		}
		w.Write([]byte(`{"g1":{"release_group":{"date":"2020","type":"EP"},"tag":{"artist":[{"count":1,"genre_mbid":"z","tag":"artist-genre"}],"release_group":[
			{"count":1,"genre_mbid":"a","tag":"low"},{"count":9,"genre_mbid":"b","tag":"top"},{"count":9,"tag":"not a genre"},{"count":3,"genre_mbid":"c","tag":"Mid"},
			{"count":2,"genre_mbid":"c","tag":"mid"},{"count":2,"genre_mbid":"d","tag":"four"},{"count":1,"genre_mbid":"e","tag":"five"}]}},
			"g2":{"release_group":{"type":"Album"},"tag":{"artist":[{"count":1,"genre_mbid":"z","tag":"artist-genre"}],"release_group":[]}}}`))
	})
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = "id" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	ids[0], ids[1] = "g1", "g2"
	got, err := c.ReleaseGroupInfos(context.Background(), ids)
	if err != nil || calls.Load() != 3 {
		t.Fatalf("calls %d, err %v", calls.Load(), err)
	}
	if g := got["g1"]; g.Type != "EP" || g.Date != "2020" || strings.Join(g.Genres, ",") != "top,Mid,four,low" {
		t.Fatalf("g1 = %+v", g)
	}
	if g := got["g2"]; g.Type != "Album" || strings.Join(g.Genres, ",") != "artist-genre" {
		t.Fatalf("g2 falls back to the genres of its artist: %+v", g)
	}
}

func TestCacheExpires(t *testing.T) {
	c, hits := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"payload":{"artists":[]}}`)) },
		listenbrainz.WithCacheTTL(20*time.Millisecond))
	for i := 0; i < 2; i++ {
		if _, err := c.TopArtists(context.Background(), "week", 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits %d, want 1 inside the TTL", hits.Load())
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := c.TopArtists(context.Background(), "week", 1, 0); err != nil || hits.Load() != 2 {
		t.Fatalf("hits %d after expiry, err %v", hits.Load(), err)
	}
}
