package api_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBookSearchUsesHardcoverForMissingCovers(t *testing.T) {
	server, base, client := loginNewServer(t)
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("q"), "Only There") {
			io.WriteString(w, `{"docs":[]}`)
			return
		}
		io.WriteString(w, `{"docs":[{"key":"/works/OL9W","title":"Quiet Book","author_name":["Ilaria Leone"],"author_key":["OL1A"],"first_publish_year":2021}]}`)
	}))
	t.Cleanup(ol.Close)
	server.TestSetOpenLibrary(ol.URL)

	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 8, 12)), nil); err != nil {
		t.Fatal(err)
	}
	picture := jpegBytes.Bytes()
	hc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(picture)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good-token" {
			io.WriteString(w, `{"errors":[{"message":"Could not verify JWT"}]}`)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "me {"):
			io.WriteString(w, `{"data":{"me":[{"username":"ada"}]}}`)
		case strings.Contains(req.Query, "SearchBooks"):
			io.WriteString(w, `{"data":{"search":{"ids":[9,4],"results":{"hits":[
				{"document":{"id":"9","has_ebook":true}},
				{"document":{"id":4,"has_audiobook":true}}
			]}}}}`)
		case strings.Contains(req.Query, "BookCards"):
			io.WriteString(w, `{"data":{"books":[
				{"id":9,"title":"Quiet Book","release_year":2021,"slug":"quiet-book","description":"<p>A note.</p>","image":{"url":"`+hcURL(r)+`/cover.jpg"},"contributions":[{"author":{"name":"Ilaria Leone"}}]},
				{"id":4,"title":"Only There","release_year":2020,"slug":"only-there","description":"<p>Just here.</p>","image":{"url":"`+hcURL(r)+`/cover.jpg"},"contributions":[{"author":{"name":"Ada Writer"}}]}
			]}}`)
		default:
			io.WriteString(w, `{"data":{}}`)
		}
	}))
	t.Cleanup(hc.Close)
	server.TestSetHardcover(hc.URL)
	server.TestSetCoverLinkClient(http.DefaultClient)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)

	before := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=quiet")
	if len(before) != 1 || before[0]["key"] != "OL9W" || before[0]["coverUrl"] != nil {
		t.Fatalf("without a token, only Open Library and no cover: %v", before)
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": "good-token"}, http.StatusOK)
	found := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=quiet")
	var quiet, only map[string]any
	for _, row := range found {
		switch row["title"] {
		case "Quiet Book":
			quiet = row
		case "Only There":
			only = row
		}
	}
	if quiet == nil || quiet["key"] != "OL9W" || quiet["hardcoverId"] != float64(9) || quiet["coverUrl"] != "/api/book-covers/hc-9.jpg" || quiet["hasEbook"] != true {
		t.Fatalf("open library book should keep its key and take the hardcover cover: %v", quiet)
	}
	if only == nil || only["key"] != "hc-4" || only["coverUrl"] != "/api/book-covers/hc-4.jpg" || only["slug"] != "only-there" {
		t.Fatalf("hardcover-only book: %v", only)
	}

	resp, err := client.Get(base + "/api/book-covers/hc-9.jpg")
	if err != nil {
		t.Fatal(err)
	}
	coverBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(coverBody, []byte{0xff, 0xd8}) {
		t.Fatalf("cover: %d %d bytes", resp.StatusCode, len(coverBody))
	}

	work := getJSON[map[string]any](t, client, base+"/api/book-works/hc-4")
	if work["title"] != "Only There" || work["description"] != "Just here." || work["slug"] != "only-there" || work["coverUrl"] != "/api/book-covers/hc-4.jpg" {
		t.Fatalf("work page: %v", work)
	}

	added := postJSON[map[string]any](t, client, base+"/api/books", map[string]any{
		"olKey": "hc-4", "title": "Only There", "author": "Ada Writer", "year": 2020, "hardcoverId": 4, "ebook": true,
	}, http.StatusCreated)
	if added["description"] != "Just here." || added["olKey"] != "hc-4" || added["coverUrl"] != "/api/book-covers/hc-4.jpg" {
		t.Fatalf("added with hardcover details and cached cover: %v", added)
	}
}

func TestHardcoverSearchFallsBackWhenHardcoverDoesNotAnswer(t *testing.T) {
	for _, mode := range []string{"slow", "down", "limit", "token"} {
		t.Run(mode, func(t *testing.T) {
			server, base, client := loginNewServer(t)
			ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{"docs":[{"key":"/works/OL9W","title":"Quiet Book","author_name":["Ilaria Leone"],"first_publish_year":2021}]}`)
			}))
			t.Cleanup(ol.Close)
			server.TestSetOpenLibrary(ol.URL)
			hc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Query string `json:"query"`
				}
				json.NewDecoder(r.Body).Decode(&req)
				if strings.Contains(req.Query, "me {") {
					io.WriteString(w, `{"data":{"me":[{"username":"ada"}]}}`)
					return
				}
				switch mode {
				case "slow":
					select {
					case <-r.Context().Done():
					case <-time.After(6 * time.Second):
					}
				case "down":
					w.WriteHeader(http.StatusInternalServerError)
				case "limit":
					w.WriteHeader(http.StatusTooManyRequests)
				default:
					io.WriteString(w, `{"errors":[{"message":"Could not verify JWT"}]}`)
				}
			}))
			t.Cleanup(hc.Close)
			server.TestSetHardcover(hc.URL)
			postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
			postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": "good-token"}, http.StatusOK)

			start := time.Now()
			found := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=quiet")
			if elapsed := time.Since(start); elapsed > 5500*time.Millisecond {
				t.Fatalf("search waited %s", elapsed)
			}
			if len(found) != 1 || found[0]["key"] != "OL9W" || found[0]["hardcoverId"] != nil {
				t.Fatalf("open library only: %v", found)
			}
		})
	}
}

func TestHardcoverCoverRejectsUnseenAndBadNames(t *testing.T) {
	server, base, client := loginNewServer(t)
	var calls atomic.Int32
	ready := atomic.Bool{}
	hc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if ready.Load() {
			t.Errorf("hardcover was called for %s", r.URL.Path)
		}
		io.WriteString(w, `{"data":{"me":[{"username":"ada"}]}}`)
	}))
	t.Cleanup(hc.Close)
	server.TestSetHardcover(hc.URL)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": "good-token"}, http.StatusOK)
	ready.Store(true)
	before := calls.Load()

	for _, path := range []string{"/api/book-covers/hc-01.jpg", "/api/book-covers/hc-999.jpg"} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
	if calls.Load() != before {
		t.Fatalf("hardcover calls %d -> %d", before, calls.Load())
	}
}

func TestHardcoverCoverRemembersAFailedDownload(t *testing.T) {
	server, base, client := loginNewServer(t)
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"docs":[]}`)
	}))
	t.Cleanup(ol.Close)
	server.TestSetOpenLibrary(ol.URL)
	var hits atomic.Int32
	hc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.jpg" {
			hits.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var req struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "me {"):
			io.WriteString(w, `{"data":{"me":[{"username":"ada"}]}}`)
		case strings.Contains(req.Query, "SearchBooks"):
			io.WriteString(w, `{"data":{"search":{"ids":[77],"results":{"hits":[{"document":{"id":77}}]}}}}`)
		case strings.Contains(req.Query, "BookCards"):
			io.WriteString(w, `{"data":{"books":[{"id":77,"title":"Rate Limited","release_year":2020,"image":{"url":"`+hcURL(r)+`/cover.jpg"},"contributions":[{"author":{"name":"Ada Writer"}}]}]}}`)
		default:
			io.WriteString(w, `{"data":{}}`)
		}
	}))
	t.Cleanup(hc.Close)
	server.TestSetHardcover(hc.URL)
	server.TestSetCoverLinkClient(http.DefaultClient)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/hardcover", map[string]string{"token": "good-token"}, http.StatusOK)

	found := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=rate")
	if len(found) != 1 || found[0]["key"] != "hc-77" || found[0]["coverUrl"] != "/api/book-covers/hc-77.jpg" {
		t.Fatalf("search: %v", found)
	}
	deadline := time.Now().Add(3 * time.Second)
	var stable int32
	for time.Now().Before(deadline) {
		n := hits.Load()
		if n > 0 && n == stable {
			time.Sleep(40 * time.Millisecond)
			if hits.Load() == n {
				break
			}
		}
		stable = hits.Load()
		time.Sleep(20 * time.Millisecond)
	}
	before := hits.Load()
	if before == 0 {
		t.Fatal("the cover was never downloaded")
	}
	resp, err := client.Get(base + "/api/book-covers/hc-77.jpg")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if hits.Load() != before {
		t.Fatalf("failed download was tried again: %d -> %d", before, hits.Load())
	}
}

func hcURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
