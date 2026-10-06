package mediaservers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlexWatched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var c map[string]any
		switch r.URL.Path {
		case "/library/sections":
			c = map[string]any{"Directory": []map[string]any{{"key": "1", "type": "movie", "title": "Films"}, {"key": "2", "type": "show", "title": "TV"}}}
		case "/library/sections/1/all":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "10", "type": "movie", "Guid": []map[string]any{{"id": "tmdb://603"}}, "viewCount": 2, "lastViewedAt": 1700000000},
				{"ratingKey": "11", "type": "movie", "Guid": []map[string]any{{"id": "tmdb://604"}}},
				{"ratingKey": "12", "type": "movie", "guid": "com.plexapp.agents.themoviedb://605?lang=en", "viewCount": 1, "lastViewedAt": 1600000000},
				{"ratingKey": "13", "type": "movie", "Guid": []map[string]any{{"id": "tmdb://606"}}, "viewCount": 1}, // watched, date unknown
			}}
		case "/library/sections/2/all":
			if r.URL.Query().Get("type") == "4" {
				c = map[string]any{"Metadata": []map[string]any{
					{"ratingKey": "100", "type": "episode", "grandparentRatingKey": "50", "parentIndex": 1, "index": 2, "viewCount": 1, "lastViewedAt": 1710000000},
					{"ratingKey": "101", "type": "episode", "grandparentRatingKey": "50", "parentIndex": 1, "index": 3},
					{"ratingKey": "102", "type": "episode", "grandparentRatingKey": "99", "parentIndex": 1, "index": 1, "viewCount": 4},
				}}
			} else {
				c = map[string]any{"Metadata": []map[string]any{{"ratingKey": "50", "type": "show", "Guid": []map[string]any{{"id": "tmdb://1399"}}}}}
			}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	defer srv.Close()
	c := NewClient("test")
	st, err := c.Watched(context.Background(), Server{Kind: KindPlex, BaseURL: srv.URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Movies) != 3 || st.Movies[603].Plays != 2 || !st.Movies[603].LastPlayed.Equal(time.Unix(1700000000, 0)) || st.Movies[605].Plays != 1 || !st.Movies[606].LastPlayed.IsZero() {
		t.Fatalf("movies: %+v", st.Movies)
	}
	if len(st.Episodes) != 1 || st.Episodes[EpisodeKey{1399, 1, 2}].Plays != 1 {
		t.Fatalf("episodes (unwatched ones and shows without a TMDB id left out): %+v", st.Episodes)
	}
}

func TestEmbyWatched(t *testing.T) {
	type item = map[string]any
	played := map[string]map[string][]item{
		"u1": {
			"Movie":   {{"Id": "m1", "Type": "Movie", "ProviderIds": map[string]string{"Tmdb": "603"}, "UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": "2024-01-02T03:04:05.0000000Z"}}},
			"Episode": {{"Id": "e1", "Type": "Episode", "SeriesId": "s1", "ParentIndexNumber": 2, "IndexNumber": 5, "UserData": map[string]any{"Played": true, "PlayCount": 0}}},
		},
		"u2": {
			"Movie": {{"Id": "m1", "Type": "Movie", "ProviderIds": map[string]string{"tmdb": "603"}, "UserData": map[string]any{"Played": true, "PlayCount": 3, "LastPlayedDate": "2025-06-01T00:00:00Z"}}},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/Users":
			_ = json.NewEncoder(w).Encode([]item{{"Id": "u1"}, {"Id": "u2"}, {"Id": "u3", "Policy": map[string]any{"IsDisabled": true}}})
		case r.URL.Path == "/Users/u1/Items/s1":
			_ = json.NewEncoder(w).Encode(item{"ProviderIds": map[string]string{"Tmdb": "1399"}})
		case r.URL.Query().Get("Filters") == "IsPlayed":
			user := r.URL.Path[len("/Users/") : len(r.URL.Path)-len("/Items")]
			if user == "u3" {
				t.Error("a disabled user was read")
			}
			_ = json.NewEncoder(w).Encode(item{"Items": played[user][r.URL.Query().Get("IncludeItemTypes")]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	st, err := NewClient("test").Watched(context.Background(), Server{Kind: KindJellyfin, BaseURL: srv.URL, Token: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m := st.Movies[603]
	if m.Plays != 4 || m.LastPlayed.Year() != 2025 {
		t.Fatalf("plays add up across users, the latest date wins: %+v", m)
	}
	if e := st.Episodes[EpisodeKey{1399, 2, 5}]; e.Plays != 1 {
		t.Fatalf("a played episode with no count counts once: %+v", st.Episodes)
	}
}
