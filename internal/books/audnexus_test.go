package books

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAudnexusLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/catalog":
			if r.URL.Query().Get("title") != "Project Hail Mary" || r.URL.Query().Get("author") != "Andy Weir" {
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			io.WriteString(w, `{"products":[{"asin":"B0CTZVXP8H"},{"asin":"bad"},{"asin":"B0DRAMA001"},{"asin":"B08G9PRS1K"}]}`)
		case r.URL.Path == "/books/B0DRAMA001":
			io.WriteString(w, `{"asin":"B0DRAMA001","title":"Project Hail Mary","formatType":"unabridged","narrators":[{"name":"A Full Cast"}],"runtimeLengthMin":274}`)
		case r.URL.Path == "/books/B0CTZVXP8H":
			io.WriteString(w, `{"asin":"B0CTZVXP8H","title":"Project Hail Mary (Italian edition)","narrators":[{"name":"Someone"}],"runtimeLengthMin":1025}`)
		case r.URL.Path == "/books/B08G9PRS1K":
			if r.URL.Query().Get("region") != "us" {
				http.Error(w, "region", http.StatusBadRequest)
				return
			}
			io.WriteString(w, `{"asin":"B08G9PRS1K","title":"Project Hail Mary","narrators":[{"name":"Ray Porter"},{"name":" "}],"runtimeLengthMin":970,"releaseDate":"2021-05-04T00:00:00.000Z","seriesPrimary":{"name":"Space","position":"1"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := &Audnexus{AudibleBase: srv.URL + "/catalog", AudnexusBase: srv.URL, HTTP: srv.Client()}
	d, err := a.Lookup(context.Background(), "Project Hail Mary", "Andy Weir")
	if err != nil {
		t.Fatal(err)
	}
	if d.ASIN != "B08G9PRS1K" || strings.Join(d.Narrators, ",") != "Ray Porter" || d.RuntimeMin != 970 || d.ReleaseDate != "2021-05-04" || d.SeriesName != "Space" || d.SeriesPosition != "1" {
		t.Fatalf("the translation and the short dramatisation are skipped, the full reading used: %+v", d)
	}
	if _, err := a.Lookup(context.Background(), "Nothing Like It", "Andy Weir"); err == nil {
		t.Fatal("a book Audible doesn't have was found")
	}
	if _, err := a.Details(context.Background(), "../etc"); err != ErrNotFound {
		t.Fatalf("a bad ASIN: %v", err)
	}
}

func TestTranslatedEditions(t *testing.T) {
	cases := map[string]bool{
		"Project Hail Mary (Italian edition)":   true,
		"Der Herr der Ringe (German edition)":   true,
		"The Hobbit (75th Anniversary Edition)": false,
		"Dune":                                  false,
		"The Way of Kings (Unabridged Edition)": false,
		"Le Petit Prince (French Edition)":      true,
	}
	for title, want := range cases {
		if translated(title) != want {
			t.Errorf("%q: %v", title, !want)
		}
	}
}
