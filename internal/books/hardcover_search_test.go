package books

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHardcoverKeysAndMerge(t *testing.T) {
	if HardcoverKey(0) != "" || HardcoverKey(12) != "hc-12" {
		t.Fatal(HardcoverKey(12))
	}
	if id, ok := ParseHardcoverKey("hc-12"); !ok || id != 12 {
		t.Fatalf("parse: %d %v", id, ok)
	}
	for _, bad := range []string{"OL1W", "hc-", "hc-0", "hc-01", "hc-12.jpg"} {
		if _, ok := ParseHardcoverKey(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if id, ok := ParseHardcoverCover("hc-12.jpg"); !ok || id != 12 {
		t.Fatalf("cover file: %d %v", id, ok)
	}

	ol := []Found{{Key: "OL1W", Title: "Quiet Book", Author: "Ilaria Leone", Year: 2021}}
	hc := []Found{
		{HardcoverID: 9, Title: "Quiet Book", Author: "Ilaria Leone", CoverImage: "https://cdn.example/q.jpg", Slug: "quiet-book", HasEbook: true},
		{HardcoverID: 4, Title: "Only There", Author: "Ada Writer", Year: 2020, CoverImage: "https://cdn.example/o.jpg", HasAudio: true},
	}
	got := MergeHardcover(ol, hc)
	if len(got) != 2 || got[0].Key != "OL1W" || got[0].HardcoverID != 9 || got[0].CoverImage == "" || got[0].CoverAddress("M") != "/api/book-covers/hc-9.jpg" {
		t.Fatalf("folded: %+v", got)
	}
	if got[1].Key != "hc-4" || !got[1].HasAudio || got[1].CoverAddress("M") != "/api/book-covers/hc-4.jpg" {
		t.Fatalf("hardcover only: %+v", got[1])
	}
	// An Open Library cover is kept.
	withCover := MergeHardcover([]Found{{Key: "OL1W", Title: "Quiet Book", Author: "Ilaria Leone", CoverID: 5}}, hc[:1])
	if withCover[0].CoverID != 5 || withCover[0].CoverAddress("M") != "/api/book-covers/5-M.jpg" {
		t.Fatalf("open library cover kept: %+v", withCover[0])
	}
	mixed := MergeHardcover(
		[]Found{{Key: "OL1W", Title: "Crescere", Author: "Ilaria Leone", Year: 2021}},
		[]Found{{HardcoverID: 3, Title: "Fiori sopra l'inferno", Author: "Ilaria Tuti", Year: 2018, CoverImage: "https://cdn.example/f.jpg"}},
	)
	PreferHardcover(mixed)
	if mixed[0].HardcoverID != 3 || mixed[1].Key != "OL1W" {
		t.Fatalf("hardcover results come first: %+v", mixed)
	}
}

func TestHardcoverSearchReadsBooksAndCovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		q := body.Query
		switch {
		case strings.Contains(q, "SearchBooks"):
			w.Write([]byte(`{"data":{"search":{"ids":[9,4],"results":{"hits":[
				{"document":{"id":"9","title":"Quiet Book","author_names":["Ilaria Leone"],"has_ebook":true}},
				{"document":{"id":4,"title":"Only There","author_names":["Ada Writer"],"has_audiobook":true}}
			]}}}}`))
		case strings.Contains(q, "BookCards"):
			ids, _ := body.Variables["ids"].([]any)
			if len(ids) == 1 && ids[0] == float64(99) {
				w.Write([]byte(`{"data":{"books":[]}}`))
				return
			}
			w.Write([]byte(`{"data":{"books":[
				{"id":9,"title":"Quiet Book","release_year":2021,"slug":"quiet-book","description":"<p>A short &amp; useful note.</p>","image":{"url":"https://cdn.example/q.jpg"},"contributions":[{"author":{"name":"Ilaria Leone"}}]},
				{"id":4,"title":"Only There","release_date":"2020-05-01","slug":"only-there","image":{"url":"https://cdn.example/o.jpg"},"contributions":[{"author":{"name":"Ada Writer"}}]}
			]}}`))
		default:
			t.Errorf("unexpected query: %s", q)
		}
	}))
	defer srv.Close()
	h := NewHardcover("good", "test")
	h.Base = srv.URL
	found, err := h.Search(context.Background(), "quiet", 10)
	if err != nil || len(found) != 2 {
		t.Fatalf("search: %v %v", err, found)
	}
	if found[0].Key != "hc-9" || found[0].Author != "Ilaria Leone" || found[0].Year != 2021 || !found[0].HasEbook || found[0].CoverImage != "https://cdn.example/q.jpg" {
		t.Fatalf("first: %+v", found[0])
	}
	if found[1].Year != 2020 || !found[1].HasAudio {
		t.Fatalf("second: %+v", found[1])
	}
	info, err := h.Book(context.Background(), 9)
	if err != nil || info.Description != "A short & useful note." || info.Slug != "quiet-book" || len(info.Subjects) != 0 {
		t.Fatalf("book: %+v %v", info, err)
	}
	if _, err := h.Book(context.Background(), 99); err != ErrNotFound {
		t.Fatalf("missing: %v", err)
	}
}
