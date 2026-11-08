package omdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("apikey") == "bad":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"Response":"False","Error":"Invalid API key!"}`))
		case r.URL.Query().Get("apikey") == "spent":
			_, _ = w.Write([]byte(`{"Response":"False","Error":"Request limit reached!"}`))
		case r.URL.Query().Get("i") == "tt0111161":
			_, _ = w.Write([]byte(`{"Response":"True","imdbRating":"9.3","imdbVotes":"3,012,345","Metascore":"82",
				"Ratings":[{"Source":"Internet Movie Database","Value":"9.3/10"},{"Source":"Rotten Tomatoes","Value":"89%"},{"Source":"Metacritic","Value":"82/100"}]}`))
		case r.URL.Query().Get("i") == "tt0000002":
			_, _ = w.Write([]byte(`{"Response":"True","imdbRating":"N/A","imdbVotes":"N/A","Metascore":"N/A","Ratings":[]}`))
		default:
			_, _ = w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	got, err := NewAt(srv.URL+"/", "good").Get(ctx, "tt0111161")
	if err != nil || got != (Scores{IMDb: 9.3, IMDbVotes: 3012345, RottenTomatoes: 89, Metacritic: 82}) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if got, err := NewAt(srv.URL+"/", "good").Get(ctx, "tt0000002"); err != nil || got != (Scores{}) {
		t.Errorf("no scores = %+v, %v", got, err)
	}
	for _, c := range []struct {
		key, id string
		want    error
	}{
		{"bad", "tt0111161", ErrBadKey},
		{"spent", "tt0111161", ErrLimit},
		{"good", "tt0999999", ErrNotFound},
		{"good", "nm0000001", ErrNotFound}, // not a title id: never sent
	} {
		if _, err := NewAt(srv.URL+"/", c.key).Get(ctx, c.id); !errors.Is(err, c.want) {
			t.Errorf("%s %s: err = %v, want %v", c.key, c.id, err, c.want)
		}
	}
}

// The key is in the address, so a failed request must not repeat it.
func TestErrorsLeaveTheKeyOut(t *testing.T) {
	_, err := NewAt("http://127.0.0.1:1/", "secret-key-123").Get(context.Background(), "tt0111161")
	if err == nil || strings.Contains(err.Error(), "secret-key-123") {
		t.Fatalf("err = %v", err)
	}
}
