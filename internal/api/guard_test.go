package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/logbuf"
	"github.com/rdborg/mediarium/internal/store"
)

// newServerWithDB is a signed-in server whose database handle the test can
// also use (to hold its connections).
func newServerWithDB(t *testing.T) (db *sql.DB, base string, client *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "1.2.3-test")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)
	jar, _ := cookiejar.New(nil)
	client = &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	return db, httpSrv.URL, client
}

// holdConnections takes every connection out of the pool, as a query stuck
// behind a lock would, until the returned function is called.
func holdConnections(t *testing.T, db *sql.DB) (release func()) {
	t.Helper()
	var conns []*sql.Conn
	for i := 0; i < db.Stats().MaxOpenConnections; i++ {
		c, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		for _, c := range conns {
			c.Close()
		}
	}
	t.Cleanup(release)
	return release
}

func TestStuckDatabaseGivesAClearBusyAnswer(t *testing.T) {
	defer api.SetReadDeadlineForTest(400 * time.Millisecond)()
	db, base, client := newServerWithDB(t)

	release := holdConnections(t, db)
	for _, path := range []string{"/api/auth/me", "/api/queue", "/api/dashboard", "/api/activity", "/api/onboarding/status"} {
		start := time.Now()
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		took := time.Since(start)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET %s while the database is stuck: status %d, want 503 (%s)", path, resp.StatusCode, body)
		}
		var payload struct {
			Error string `json:"error"`
			Busy  bool   `json:"busy"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Error != "Mediarium is busy right now. Try again in a moment." || !payload.Busy {
			t.Fatalf("GET %s: unexpected busy body %q (%v)", path, body, err)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("GET %s: content type %q", path, ct)
		}
		if took > 3*time.Second {
			t.Errorf("GET %s took %v to give up, want about the deadline", path, took)
		}
	}
	// The version needs no database and keeps answering.
	if resp, err := client.Get(base + "/api/version"); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/version while the database is stuck: %v %v", resp, err)
	}

	release()
	if resp, err := client.Get(base + "/api/queue"); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/queue after the database recovered: %v %v", resp, err)
	}
}

// Pages that talk to other services are not cut short: only the ones that
// read the database have a deadline.
func TestRoutesThatReachOutsideAreNotGivenTheDeadline(t *testing.T) {
	defer api.SetReadDeadlineForTest(50 * time.Millisecond)()
	db, base, client := newServerWithDB(t)
	release := holdConnections(t, db)
	defer release()

	// /api/search would hang on the stuck database too, but is not answered
	// with the busy reply; give it a moment and then free the database.
	done := make(chan int, 1)
	go func() {
		resp, err := client.Get(base + "/api/search?q=heat")
		if err != nil {
			done <- -1
			return
		}
		resp.Body.Close()
		done <- resp.StatusCode
	}()
	select {
	case code := <-done:
		if code == http.StatusServiceUnavailable {
			t.Fatal("a search was cut off with the busy answer")
		}
	case <-time.After(400 * time.Millisecond):
		// still waiting, as it should be
	}
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the search never finished after the database recovered")
	}
}

func TestDiagnosticsAreForAdministratorsAndHoldNoSecrets(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)

	// Something that looks like a leaked secret reaches the log.
	logbuf.Default.Write([]byte("2026/09/29 18:00:00 test: sign in failed password=hunter2 for diagnostics-marker\n"))
	logbuf.Default.Write([]byte(`2026/09/29 18:00:01 test: fetching "https://indexer.example/api?apikey=SECRETKEY123&q=x" for diagnostics-marker` + "\n"))

	d := getJSON[map[string]any](t, admin, base+"/api/system/diagnostics")
	if d["version"] != "test" || d["safeMode"] != false {
		t.Fatalf("unexpected header fields: %+v", d)
	}
	db, _ := d["database"].(map[string]any)
	if db["journalMode"] != "wal" || db["maxOpen"].(float64) < 2 || db["sizeBytes"].(float64) <= 0 {
		t.Fatalf("database section: %+v", db)
	}
	for _, key := range []string{"open", "inUse", "idle", "waitCount", "waitSeconds"} {
		if _, ok := db[key]; !ok {
			t.Errorf("database section is missing %q", key)
		}
	}
	if _, ok := d["health"].([]any); !ok {
		t.Errorf("health section missing: %+v", d)
	}
	lines, _ := d["log"].([]any)
	found := 0
	for _, l := range lines {
		s := l.(string)
		if strings.Contains(s, "hunter2") || strings.Contains(s, "SECRETKEY123") {
			t.Fatalf("a secret is in the diagnostics: %q", s)
		}
		if strings.Contains(s, "diagnostics-marker") {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected both marker lines in the log, found %d among %d lines", found, len(lines))
	}

	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/system/diagnostics"); status != http.StatusForbidden {
		t.Errorf("a member reading diagnostics: status %d, want 403", status)
	}
	anon := &http.Client{Timeout: 10 * time.Second}
	if status, _ := doStatus(t, anon, http.MethodGet, base+"/api/system/diagnostics"); status != http.StatusUnauthorized {
		t.Errorf("signed-out diagnostics: status %d, want 401", status)
	}
}
