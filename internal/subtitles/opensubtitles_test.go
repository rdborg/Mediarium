package subtitles_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryanborg/mediarium/internal/subtitles"
)

func TestNoAPIKey(t *testing.T) {
	c := subtitles.New("")
	if c.HasAPIKey() {
		t.Fatal("expected HasAPIKey false for empty key")
	}
	if _, err := c.Search(context.Background(), "the matrix", "", "en"); err != subtitles.ErrNoAPIKey {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subtitles" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Api-Key") != "test-key" {
			t.Fatalf("missing/incorrect Api-Key header")
		}
		if r.URL.Query().Get("query") != "the matrix" {
			t.Fatalf("unexpected query param: %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("languages") != "en" {
			t.Fatalf("unexpected languages param: %s", r.URL.Query().Get("languages"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"attributes": map[string]any{
						"language":       "en",
						"release":        "The.Matrix.1999.1080p.BluRay.x264-GROUP",
						"download_count": 12345,
						"ratings":        9.5,
						"files":          []map[string]any{{"file_id": 42}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := subtitles.NewWithBaseURL("test-key", srv.URL)
	results, err := c.Search(context.Background(), "the matrix", "", "en")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.FileID != 42 || r.Language != "en" || r.Release != "The.Matrix.1999.1080p.BluRay.x264-GROUP" {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRequestDownloadAndDownloadFile(t *testing.T) {
	var fileSrv *httptest.Server
	fileSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello subtitle fixture\n"))
	}))
	defer fileSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["file_id"] != float64(42) {
			t.Fatalf("unexpected file_id: %v", body["file_id"])
		}
		json.NewEncoder(w).Encode(map[string]string{"link": fileSrv.URL})
	}))
	defer apiSrv.Close()

	c := subtitles.NewWithBaseURL("test-key", apiSrv.URL)
	link, err := c.RequestDownload(context.Background(), 42)
	if err != nil {
		t.Fatalf("request download: %v", err)
	}
	if link != fileSrv.URL {
		t.Fatalf("unexpected link: %s", link)
	}

	data, err := c.DownloadFile(context.Background(), link)
	if err != nil {
		t.Fatalf("download file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty subtitle file content")
	}
}
