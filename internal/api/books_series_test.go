package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSeriesLibrary is an Open Library with one three-book series, "Saga".
func fakeSeriesLibrary(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		switch {
		case q == "key:/works/OL2W":
			io.WriteString(w, `{"docs":[{"key":"/works/OL2W","title":"Saga Two","series_key":["OL5L"],"series_name":["Saga"],"series_position":["2"]}]}`)
		case q == "series_key:OL5L":
			io.WriteString(w, `{"docs":[
				{"key":"/works/OL1W","title":"Saga One","author_name":["Ann Writer"],"author_key":["OL9A"],"first_publish_year":2001,"cover_i":1,"series_key":["OL5L"],"series_position":["1"]},
				{"key":"/works/OL2W","title":"Saga Two","author_name":["Ann Writer"],"author_key":["OL9A"],"first_publish_year":2003,"cover_i":2,"series_key":["OL5L"],"series_position":["2"]},
				{"key":"/works/OL25W","title":"A Saga Novella","author_name":["Ann Writer"],"first_publish_year":2004,"series_key":["OL5L"],"series_position":["2.5"]},
				{"key":"/works/OL3W","title":"Saga Three","author_name":["Ann Writer"],"author_key":["OL9A"],"first_publish_year":2005,"cover_i":3,"series_key":["OL5L"],"series_position":["3"]},
				{"key":"/works/OL9W","title":"The Saga Box Set","author_name":["Ann Writer"],"series_key":["OL5L"],"series_position":["1-3"]}
			]}`)
		case strings.Contains(q, "Saga One"):
			io.WriteString(w, `{"docs":[{"key":"/works/OL1W","title":"Saga One","author_name":["Ann Writer"],"author_key":["OL9A"],"cover_i":1}]}`)
		case strings.Contains(q, "Saga Two"):
			io.WriteString(w, `{"docs":[{"key":"/works/OL2W","title":"Saga Two","author_name":["Ann Writer"],"author_key":["OL9A"],"cover_i":2}]}`)
		case strings.Contains(q, "Saga Four"):
			io.WriteString(w, `{"docs":[{"key":"/works/OL4W","title":"Saga Four","author_name":["Ann Writer"],"author_key":["OL9A"],"cover_i":4}]}`)
		default:
			io.WriteString(w, `{"docs":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFollowASeries(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(fakeSeriesLibrary(t).URL)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)

	got := getJSON[map[string]any](t, client, base+"/api/book-works/OL2W/series?title=Saga%20Two&author=Ann%20Writer")
	series, _ := got["series"].(map[string]any)
	if series == nil || series["name"] != "Saga" || series["source"] != "openlibrary" || series["position"] != "2" || series["followed"] != false {
		t.Fatalf("series: %v", got)
	}
	if entries := series["entries"].([]any); len(entries) != 4 {
		t.Fatalf("one book per place, without the box set: %v", entries)
	}
	if none := getJSON[map[string]any](t, client, base+"/api/book-works/OL77W/series"); none["series"] != nil {
		t.Fatalf("a book in no series: %v", none)
	}

	postJSONMethod[any](t, client, http.MethodPut, base+"/api/book-series/openlibrary/OL5L/follow", map[string]any{"name": "Saga", "audiobook": true}, http.StatusConflict)
	postJSONMethod[any](t, client, http.MethodPut, base+"/api/book-series/openlibrary/../follow", map[string]any{"name": "Saga", "ebook": true}, http.StatusNotFound)
	res := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/book-series/openlibrary/OL5L/follow", map[string]any{"name": "Saga", "ebook": true}, http.StatusOK)
	if res["added"] != float64(3) {
		t.Fatalf("the three whole books are added, not the novella: %v", res)
	}
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 3 {
		t.Fatalf("books: %v", list)
	}
	for _, b := range list {
		if b["seriesName"] != "Saga" || b["seriesPosition"] == "" {
			t.Fatalf("a series book without its place: %v", b)
		}
	}
	if again := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/book-series/openlibrary/OL5L/follow", map[string]any{"name": "Saga", "ebook": true}, http.StatusOK); again["added"] != float64(0) {
		t.Fatalf("following again adds nothing: %v", again)
	}
	followed := getJSON[map[string]any](t, client, base+"/api/book-works/OL2W/series")["series"].(map[string]any)
	if followed["followed"] != true || followed["ebook"] != true {
		t.Fatalf("followed state: %v", followed)
	}
	if fl := getJSON[[]map[string]any](t, client, base+"/api/book-series"); len(fl) != 1 {
		t.Fatalf("followed list: %v", fl)
	}
	postJSONMethod[any](t, client, http.MethodDelete, base+"/api/book-series/openlibrary/OL5L/follow", nil, http.StatusOK)
	if fl := getJSON[[]map[string]any](t, client, base+"/api/book-series"); len(fl) != 0 {
		t.Fatalf("unfollowed: %v", fl)
	}
	if list := getJSON[[]map[string]any](t, client, base+"/api/books"); len(list) != 3 {
		t.Fatalf("unfollowing keeps the books: %d", len(list))
	}
}

func TestSeriesFromHardcover(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(fakeSeriesLibrary(t).URL)
	hc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			io.WriteString(w, `{"errors":[{"message":"Could not verify JWT"}]}`)
			return
		}
		var req struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "me {"):
			io.WriteString(w, `{"data":{"me":[{"username":"ryan"}]}}`)
		case strings.Contains(req.Query, "FindBook"):
			io.WriteString(w, `{"data":{"books":[{"id":2,"title":"Saga Two","contributions":[{"author":{"name":"Ann Writer"}}],"book_series":[{"position":2,"series":{"id":55,"name":"The Saga"}}]}]}}`)
		case strings.Contains(req.Query, "SeriesBooks"):
			io.WriteString(w, `{"data":{"series":[{"id":55,"name":"The Saga","book_series":[
				{"position":1,"book":{"id":1,"title":"Saga One","release_date":"2001-01-01","contributions":[{"author":{"name":"Ann Writer"}}]}},
				{"position":2,"book":{"id":2,"title":"Saga Two","release_date":"2003-01-01","contributions":[{"author":{"name":"Ann Writer"}}]}},
				{"position":4,"book":{"id":4,"title":"Saga Four","release_date":"2099-03-01","contributions":[{"author":{"name":"Ann Writer"}}]}}
			]}]}}`)
		}
	}))
	defer hc.Close()
	server.TestSetHardcover(hc.URL)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)

	postJSONMethod[any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": "bad"}, http.StatusBadRequest)
	saved := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": "Bearer good-token"}, http.StatusOK)
	if saved["set"] != true || saved["username"] != "ryan" {
		t.Fatalf("saved: %v", saved)
	}
	if st := getJSON[map[string]any](t, client, base+"/api/settings/hardcover"); st["set"] != true {
		t.Fatalf("state: %v", st)
	}

	got := getJSON[map[string]any](t, client, base+"/api/book-works/OL2W/series?title=Saga%20Two&author=Ann%20Writer")["series"].(map[string]any)
	if got["source"] != "hardcover" || got["key"] != "55" || got["position"] != "2" {
		t.Fatalf("hardcover series: %v", got)
	}
	res := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/book-series/hardcover/55/follow", map[string]any{"name": "The Saga", "ebook": true}, http.StatusOK)
	if res["added"] != float64(3) {
		t.Fatalf("added: %v", res)
	}
	waitBackground(t, server)
	var four map[string]any
	for _, b := range getJSON[[]map[string]any](t, client, base+"/api/books") {
		if b["title"] == "Saga Four" {
			four = b
		}
	}
	if four == nil || four["releaseDate"] != "2099-03-01" || four["olKey"] != "OL4W" {
		t.Fatalf("the coming book is added with its date and Open Library entry: %v", four)
	}
	server.TestCheckFollowedSeries(context.Background())

	// Removing the token goes back to Open Library.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": ""}, http.StatusOK)
	if got := getJSON[map[string]any](t, client, base+"/api/book-works/OL2W/series")["series"].(map[string]any); got["source"] != "openlibrary" {
		t.Fatalf("without a token: %v", got)
	}
}
