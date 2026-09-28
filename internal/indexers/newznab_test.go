package indexers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/indexers"
)

const fixtureRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
<channel>
<item>
<title>The.Matrix.1999.1080p.BluRay.x264-GROUP</title>
<guid>abc123</guid>
<comments>https://fixture.test/details/abc123</comments>
<pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
<enclosure url="https://fixture.test/getnzb/abc123.nzb" length="4294967296" type="application/x-nzb" />
<newznab:attr name="size" value="4294967296"/>
<newznab:attr name="category" value="2000"/>
</item>
</channel>
</rss>`

func TestNewznabSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("t") != "search" {
			t.Fatalf("expected t=search, got %s", r.URL.Query().Get("t"))
		}
		if r.URL.Query().Get("apikey") != "fixture-key" {
			t.Fatalf("expected apikey=fixture-key, got %s", r.URL.Query().Get("apikey"))
		}
		if r.URL.Query().Get("q") != "the matrix" {
			t.Fatalf("expected q='the matrix', got %s", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("cat") != "2000" {
			t.Fatalf("expected cat=2000, got %s", r.URL.Query().Get("cat"))
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(fixtureRSS))
	}))
	defer srv.Close()

	client := indexers.NewNewznabClient("Fixture Indexer", srv.URL, "fixture-key")
	results, err := client.Search(context.Background(), "the matrix", []int{2000})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Title != "The.Matrix.1999.1080p.BluRay.x264-GROUP" {
		t.Errorf("unexpected title: %s", r.Title)
	}
	if r.IndexerName != "Fixture Indexer" {
		t.Errorf("unexpected indexer name: %s", r.IndexerName)
	}
	if r.DownloadURL != "https://fixture.test/getnzb/abc123.nzb" {
		t.Errorf("unexpected download url: %s", r.DownloadURL)
	}
	if r.SizeBytes != 4294967296 {
		t.Errorf("unexpected size: %d", r.SizeBytes)
	}
	if len(r.Categories) != 1 || r.Categories[0] != 2000 {
		t.Errorf("unexpected categories: %v", r.Categories)
	}
	wantDate := time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*3600))
	if !r.PublishDate.Equal(wantDate) {
		t.Errorf("unexpected publish date: %v", r.PublishDate)
	}
}

func TestNewznabSearchAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><error code="100" description="Incorrect API key"/></channel></rss>`))
	}))
	defer srv.Close()

	client := indexers.NewNewznabClient("Fixture Indexer", srv.URL, "bad-key")
	_, err := client.Search(context.Background(), "query", nil)
	if err == nil {
		t.Fatal("expected error for invalid api key response")
	}
}

func TestNewznabSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	client := indexers.NewNewznabClient("Fixture Indexer", srv.URL, "key")
	_, err := client.Search(context.Background(), "query", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestNewznabErrorsDoNotLeakTheAPIKeyOrHideTheReason(t *testing.T) {
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`))
	}))
	defer bare.Close()
	_, err := indexers.NewNewznabClient("Bare", bare.URL, "k").Search(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "Incorrect user credentials") {
		t.Fatalf("expected the indexer's own reason, got %v", err)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := dead.URL
	dead.Close()
	_, err = indexers.NewNewznabClient("Dead", url, "SUPERSECRETKEY").Search(context.Background(), "x", nil)
	if err == nil || strings.Contains(err.Error(), "SUPERSECRETKEY") {
		t.Fatalf("a failed request must not put the API key in its error: %v", err)
	}
}
