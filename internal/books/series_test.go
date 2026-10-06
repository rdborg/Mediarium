package books

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPositions(t *testing.T) {
	cases := []struct {
		in   string
		num  float64
		ok   bool
		main bool
	}{
		{"1", 1, true, true},
		{" 2 ", 2, true, true},
		{"#3", 3, true, true},
		{"Book 4", 4, true, true},
		{"2.5", 2.5, true, false},
		{"1-7", 0, false, false},
		{"", 0, false, false},
		{"prequel", 0, false, false},
	}
	for _, tc := range cases {
		n, ok := PositionNumber(tc.in)
		if ok != tc.ok || (ok && n != tc.num) || MainPosition(tc.in) != tc.main {
			t.Errorf("%q: %v %v main %v", tc.in, n, ok, MainPosition(tc.in))
		}
	}
}

func TestTidyEntriesKeepsOneBookPerPlace(t *testing.T) {
	got := tidyEntries([]SeriesEntry{
		{Title: "A Torre Negra", Position: "3", popularity: 2},
		{Title: "The Waste Lands", Position: "3", popularity: 40},
		{Title: "The Gunslinger", Position: "1", popularity: 90},
		{Title: "The Dark Tower Box Set", Position: "1-7", popularity: 5},
		{Title: "The Drawing of the Three", Position: "2.0", popularity: 50},
		{Title: "The Wind Through the Keyhole", Position: "4.5", popularity: 20},
		{Title: "No place", Position: "", popularity: 99},
	})
	var titles []string
	for _, e := range got {
		titles = append(titles, e.Position+":"+e.Title)
	}
	want := "1:The Gunslinger|2:The Drawing of the Three|3:The Waste Lands|4.5:The Wind Through the Keyhole"
	if strings.Join(titles, "|") != want {
		t.Fatalf("got %s\nwant %s", strings.Join(titles, "|"), want)
	}
}

func TestOpenLibrarySeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		switch {
		case q == "key:/works/OL82537W":
			io.WriteString(w, `{"docs":[{"key":"/works/OL82537W","title":"Chamber","series_key":["OL326110L"],"series_name":["Harry Potter"],"series_position":["2"]}]}`)
		case q == "key:/works/OL1W":
			io.WriteString(w, `{"docs":[{"key":"/works/OL1W","title":"Alone"}]}`)
		case q == "series_key:OL326110L":
			io.WriteString(w, `{"docs":[
				{"key":"/works/OL82537W","title":"Harry Potter and the Chamber of Secrets","author_name":["J. K. Rowling"],"author_key":["OL23919A"],"cover_i":10,"first_publish_year":1998,"edition_count":300,"series_key":["OL326110L"],"series_position":["2"]},
				{"key":"/works/OL82563W","title":"Harry Potter and the Philosopher's Stone","author_name":["J. K. Rowling"],"cover_i":11,"first_publish_year":1997,"edition_count":400,"format":["ebook"],"series_key":["OLX1L","OL326110L"],"series_position":["9","1"]},
				{"key":"/works/OL14981609W","title":"Harry Potter (series) 1-7","series_key":["OL326110L"],"series_position":["1-7"]},
				{"key":"/works/OL99W","title":"Harry Potter Box Set","series_key":["OL326110L"],"series_position":["3"]}
			]}`)
		default:
			http.Error(w, "unexpected "+q, http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}

	s, err := c.SeriesOf(context.Background(), "OL82537W")
	if err != nil || s == nil {
		t.Fatalf("series: %v %v", s, err)
	}
	if s.Source != SourceOpenLibrary || s.Key != "OL326110L" || s.Name != "Harry Potter" || len(s.Entries) != 2 {
		t.Fatalf("got %+v", s)
	}
	if e := s.Entries[0]; e.Key != "OL82563W" || e.Position != "1" || !e.HasEbook || e.CoverID != 11 {
		t.Fatalf("first entry %+v (the position must be the one for this series, not the other)", e)
	}
	if s.Entries[1].Position != "2" {
		t.Fatalf("second entry %+v", s.Entries[1])
	}
	none, err := c.SeriesOf(context.Background(), "OL1W")
	if err != nil || none != nil {
		t.Fatalf("a book in no series: %v %v", none, err)
	}
	if _, err := c.SeriesBooks(context.Background(), "../etc", ""); err == nil {
		t.Fatal("a bad series key was accepted")
	}
}

func TestHardcoverSeries(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "me {"):
			io.WriteString(w, `{"data":{"me":[{"username":"ryan"}]}}`)
		case strings.Contains(req.Query, "FindBook"):
			io.WriteString(w, `{"data":{"books":[
				{"id":5,"title":"The Final Empire","contributions":[{"author":{"name":"Someone Else"}}],"book_series":[{"position":1,"series":{"id":77,"name":"Wrong"}}]},
				{"id":6,"title":"The Final Empire","contributions":[{"author":{"name":"Brandon Sanderson"}}],"book_series":[{"position":1,"series":{"id":42,"name":"Mistborn"}}]}
			]}}`)
		case strings.Contains(req.Query, "SeriesBooks"):
			if req.Variables["id"] != float64(42) {
				http.Error(w, "wrong series", http.StatusBadRequest)
				return
			}
			io.WriteString(w, `{"data":{"series":[{"id":42,"name":"Mistborn","book_series":[
				{"position":1,"book":{"id":6,"title":"The Final Empire","release_date":"2006-07-17","contributions":[{"author":{"name":"Brandon Sanderson"}}]}},
				{"position":2,"book":{"id":7,"title":"The Well of Ascension","release_date":"2007-08-21","contributions":[{"author":{"name":"Brandon Sanderson"}}]}},
				{"position":8,"book":{"id":9,"title":"Book Eight","release_date":"2031-01-01T00:00:00","contributions":[{"author":{"name":"Brandon Sanderson"}}]}}
			]}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	h := &Hardcover{Base: srv.URL, Token: CleanHardcoverToken("Bearer abc.def"), HTTP: srv.Client()}

	if name, err := h.Me(context.Background()); err != nil || name != "ryan" {
		t.Fatalf("me: %q %v", name, err)
	}
	s, err := h.SeriesFor(context.Background(), "The Final Empire", "Brandon Sanderson")
	if err != nil || s == nil {
		t.Fatalf("series: %v %v", s, err)
	}
	if s.Source != SourceHardcover || s.Key != "42" || s.Name != "Mistborn" || len(s.Entries) != 3 {
		t.Fatalf("got %+v", s)
	}
	if e := s.Entries[2]; e.Position != "8" || e.ReleaseDate != "2031-01-01" {
		t.Fatalf("last entry %+v", e)
	}
	for _, a := range auth {
		if a != "Bearer abc.def" {
			t.Fatalf("authorization header %q", a)
		}
	}

	if _, err := (&Hardcover{Base: srv.URL, HTTP: srv.Client()}).Me(context.Background()); err != ErrHardcoverToken {
		t.Fatalf("no token: %v", err)
	}
}

func TestHardcoverRefusedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errors":[{"message":"Could not verify JWT: JWSError"}]}`)
	}))
	defer srv.Close()
	h := &Hardcover{Base: srv.URL, Token: "x", HTTP: srv.Client()}
	if _, err := h.Me(context.Background()); err != ErrHardcoverToken {
		t.Fatalf("got %v", err)
	}
}
