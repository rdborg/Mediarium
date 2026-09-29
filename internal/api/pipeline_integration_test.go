package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/store"
)

// TestPhase1ExitCriteriaEndToEnd walks the exact flow that defines
// Phase 1 as "done": search -> grab -> download -> organize a movie start to
// finish, with zero separate containers. Every external dependency (the
// indexer, the Usenet server) is faked in-process so this runs without any
// live credentials, but the app code under test — HTTP API, indexer
// search, NNTP download, yEnc decode, PAR2/archive skip-if-absent,
// naming, hardlink import — is all real.
func TestPhase1ExitCriteriaEndToEnd(t *testing.T) {
	movieContent := []byte("Fixture movie bytes standing in for a real video file, repeated for size. " +
		strings.Repeat("padding-", 200))

	nntpSrv := newFakeNNTPServer(t, movieContent)
	indexerSrv := newFakeIndexerServer(t)

	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// --- Onboarding: create the admin account ---
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	// --- Settings: library path + naming preset ---
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]string{
		"moviesPath":   cfg.MoviesDir,
		"namingPreset": "minimal",
	}, http.StatusOK)

	// --- Add an indexer ---
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)

	// --- Add a download client ---
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": nntpSrv.addr, "port": nntpSrv.port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)

	// Seed the library directly (bypasses a live TMDB call — TMDB's own
	// client is unit tested separately in internal/metadata).
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	// --- Search ---
	searchResp := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/search?q=fixture")
	if len(searchResp) != 1 {
		t.Fatalf("expected 1 search result, got %d: %+v", len(searchResp), searchResp)
	}
	result := searchResp[0]
	if result["indexerName"] != "Fixture Indexer" {
		t.Fatalf("expected result tagged with indexer name, got %+v", result)
	}

	// --- Grab ---
	grabResp := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", httpSrv.URL, movie.ID), map[string]any{
		"releaseTitle": result["title"],
		"downloadUrl":  result["downloadUrl"],
		"sizeBytes":    int64(len(movieContent)),
	}, http.StatusAccepted)
	if grabResp["queueId"] == nil {
		t.Fatalf("expected queueId in grab response, got %+v", grabResp)
	}

	// --- Download + organize ---
	// Poll the queue until the pipeline finishes (it runs in the
	// background — see internal/api/pipeline.go).
	deadline := time.Now().Add(20 * time.Second)
	var finalStatus string
	for time.Now().Before(deadline) {
		queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) == 1 {
			finalStatus, _ = queueList[0]["status"].(string)
			if finalStatus == "completed" || finalStatus == "failed" {
				if finalStatus == "failed" {
					t.Fatalf("pipeline failed: %+v", queueList[0])
				}
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if finalStatus != "completed" {
		t.Fatalf("pipeline did not complete within deadline, last status=%q", finalStatus)
	}

	// Confirm the movie is now marked downloaded with a real file on disk
	// containing the content the fake NNTP server served — i.e. the file
	// that ends up in the library is actually the one that was
	// searched, grabbed, and downloaded, not just a status flag flip.
	finalMovie := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID))
	if finalMovie["status"] != "downloaded" {
		t.Fatalf("expected movie status downloaded, got %+v", finalMovie)
	}
	filePath, _ := finalMovie["filePath"].(string)
	if filePath == "" {
		t.Fatalf("expected a non-empty filePath, got %+v", finalMovie)
	}
	if !strings.HasPrefix(filePath, cfg.MoviesDir) {
		t.Fatalf("expected file to be organized under %s, got %s", cfg.MoviesDir, filePath)
	}
	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read organized file: %v", err)
	}
	if !bytes.Equal(got, movieContent) {
		t.Fatalf("organized file content does not match downloaded content (len want=%d got=%d)", len(movieContent), len(got))
	}
}

// --- test fixtures: a fake Newznab indexer and a fake NNTP server ---

func newFakeIndexerServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
<channel>
<item>
<title>The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE</title>
<guid>fixture-guid</guid>
<comments>%s/details/fixture</comments>
<enclosure url="%s/nzb/fixture.nzb" length="1000" type="application/x-nzb" />
<newznab:attr name="size" value="1000"/>
<newznab:attr name="category" value="2000"/>
</item>
</channel>
</rss>`, r.Host, "http://"+r.Host)
	})
	mux.HandleFunc("/nzb/fixture.nzb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
<file subject="[1/1] &quot;fixture-movie.mkv&quot; yEnc (1/1)">
<groups><group>alt.binaries.test</group></groups>
<segments><segment bytes="1000" number="1">fixture-segment@example</segment></segments>
</file>
</nzb>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

type fakeNNTPServer struct {
	addr string
	port int
}

// newFakeNNTPServer serves a single yEnc-encoded article containing
// movieContent under message-id "fixture-segment@example", matching the
// NZB fixture above — a minimal stand-in for internal/download's own more
// thorough NNTP protocol test server, kept local to this package to avoid
// coupling internal/api's tests to internal/download's test-only helpers.
func newFakeNNTPServer(t *testing.T, movieContent []byte) *fakeNNTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	body := encodeFixtureYenc("fixture-movie.mkv", movieContent)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeNNTPConn(conn, body)
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &fakeNNTPServer{addr: "127.0.0.1", port: port}
}

func handleFakeNNTPConn(conn net.Conn, body []byte) {
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	rw.WriteString("200 fake nntp ready\r\n")
	rw.Flush()
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "AUTHINFO USER"):
			rw.WriteString("381 password required\r\n")
		case strings.HasPrefix(upper, "AUTHINFO PASS"):
			rw.WriteString("281 ok\r\n")
		case strings.HasPrefix(upper, "GROUP"):
			rw.WriteString("211 0 0 0 group\r\n")
		case strings.HasPrefix(upper, "BODY"):
			rw.WriteString("222 body follows\r\n")
			for _, l := range strings.Split(string(body), "\n") {
				l = strings.TrimRight(l, "\r")
				if strings.HasPrefix(l, ".") {
					rw.WriteString(".")
				}
				rw.WriteString(l)
				rw.WriteString("\r\n")
			}
			rw.WriteString(".\r\n")
		case upper == "QUIT":
			rw.WriteString("205 bye\r\n")
			rw.Flush()
			return
		default:
			rw.WriteString("500 unrecognized\r\n")
		}
		rw.Flush()
	}
}

// encodeFixtureYenc is a trimmed-down single-part yEnc encoder mirroring
// internal/download's EncodeYenc, duplicated here (rather than exported
// cross-package) to keep this integration test from depending on
// internal/download's internals — it exercises the real decoder in
// internal/download through the HTTP+NNTP round trip instead.
func encodeFixtureYenc(name string, data []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "=ybegin line=128 size=%d name=%s\r\n", len(data), name)
	col := 0
	for _, b := range data {
		enc := b + 42
		switch enc {
		case 0x00, 0x0A, 0x0D, 0x3D:
			buf.WriteByte('=')
			buf.WriteByte(enc + 64)
			col += 2
		default:
			buf.WriteByte(enc)
			col++
		}
		if col >= 128 {
			buf.WriteString("\r\n")
			col = 0
		}
	}
	if col > 0 {
		buf.WriteString("\r\n")
	}
	fmt.Fprintf(&buf, "=yend size=%d crc32=%08x\r\n", len(data), crc32.ChecksumIEEE(data))
	return buf.Bytes()
}

// --- small HTTP JSON test helpers ---

func postJSON[T any](t *testing.T, client *http.Client, url string, body any, wantStatus int) T {
	t.Helper()
	return postJSONMethod[T](t, client, http.MethodPost, url, body, wantStatus)
}

func postJSONMethod[T any](t *testing.T, client *http.Client, method, url string, body any, wantStatus int) T {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out T
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: expected status %d, got %d: %+v", method, url, wantStatus, resp.StatusCode, out)
	}
	return out
}

func getJSON[T any](t *testing.T, client *http.Client, url string) T {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode GET %s response: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected status 200, got %d: %+v", url, resp.StatusCode, out)
	}
	return out
}
