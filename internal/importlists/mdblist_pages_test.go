package importlists

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// MDBList's page limit can count movies and shows together. A full page of
// 120 movies and 80 shows must not be taken for the last one.
func TestMDBListMixedPagesAreAllRead(t *testing.T) {
	entry := func(prefix string, n int) mdbEntry {
		return mdbEntry{ID: n, Title: fmt.Sprintf("%s %d", prefix, n), IMDBID: fmt.Sprintf("tt%07d", n)}
	}
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		asked = append(asked, r.URL.Query().Get("offset"))
		var movies, shows []mdbEntry
		switch offset {
		case 0:
			for i := 0; i < 120; i++ {
				movies = append(movies, entry("Movie", i+1))
			}
			for i := 0; i < 80; i++ {
				shows = append(shows, entry("Show", 1000+i))
			}
		case mdbPageSize:
			for i := 0; i < 5; i++ {
				movies = append(movies, entry("Movie", 2000+i))
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"movies": movies, "shows": shows})
	}))
	defer srv.Close()
	items, err := NewMDBListClientAt(srv.URL).Items(context.Background(), "key", "someone", "mixed")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 205 {
		t.Fatalf("read %d titles over pages %v, want 205", len(items), asked)
	}
}

func TestRetryAfterIsKeptSane(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"30": 30 * time.Second, "": 0, "-5": 0, "soon": 0, "99999999999999": 24 * time.Hour,
	} {
		h := http.Header{}
		h.Set("Retry-After", in)
		if got := retryAfter(h); got != want {
			t.Errorf("Retry-After %q = %v, want %v", in, got, want)
		}
	}
}
