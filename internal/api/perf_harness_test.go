package api_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// fillBigLibrary puts a big, plain library into an empty database with a few
// bulk statements: movies, shows with episodes, albums with tracks, books, a
// queue, a blocklist, activity and watch history.
func fillBigLibrary(tb testing.TB, db *sql.DB, movies, shows, episodes, albums, tracks, books, queue, blocklist int) {
	tb.Helper()
	run := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			tb.Fatalf("%v\n%s", err, q)
		}
	}
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO movies (tmdb_id, title, year, overview, poster_path, status, quality, file_path, monitored, release_date, genres, added_at)
		SELECT i, 'Movie ' || i, 1950 + i % 75, 'A long plot summary. A long plot summary. A long plot summary. A long plot summary.', '/p' || i || '.jpg',
		       CASE WHEN i % 5 = 0 THEN 'missing' ELSE 'downloaded' END, 'Bluray-1080p',
		       CASE WHEN i % 5 = 0 THEN NULL ELSE '/movies/Movie ' || i || ' (2020)/movie.mkv' END, i % 7 <> 0,
		       (1950 + i % 75) || '-0' || (1 + i % 9) || '-15', 'Drama,Action', strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || (i % 400) || ' days')
		FROM n`, movies)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO series (tmdb_id, title, year, overview, poster_path, first_air_date, monitored, genres, added_at)
		SELECT i, 'Show ' || i, 1990 + i % 35, 'A long plot summary. A long plot summary. A long plot summary.', '/s' || i || '.jpg',
		       (1990 + i % 35) || '-01-01', i % 9 <> 0, 'Drama', strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || (i % 400) || ' days')
		FROM n`, shows)
	per := episodes / shows
	run(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO episodes (series_id, season, episode, title, overview, air_date, status, quality, file_path, monitored)
		SELECT 1 + i / ?, 1 + (i % ?) / 10, 1 + (i % ?) % 10, 'Episode', 'Overview text of the episode.',
		       date('now', '-' || ((i * 7) % 3000 - 200) || ' days'),
		       CASE WHEN i % 6 = 0 THEN 'missing' ELSE 'downloaded' END, 'WEBDL-1080p',
		       CASE WHEN i % 6 = 0 THEN NULL ELSE '/tv/Show ' || (1 + i / ?) || '/S' || (1 + (i % ?) / 10) || 'E' || (i % ?) || '.mkv' END, i % 11 <> 0
		FROM n`, per*shows-1, per, per, per, per, per, per)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 200)
		INSERT INTO artists (mbid, name, sort_name) SELECT 'artist-' || i, 'Artist ' || i, 'Artist ' || i FROM n`)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO albums (artist_id, mbid, title, release_date, status, path)
		SELECT 1 + i % 200, 'album-' || i, 'Album ' || i, (1970 + i % 50) || '', CASE WHEN i % 4 = 0 THEN 'missing' ELSE 'downloaded' END, '/music/A' || i FROM n`, albums)
	run(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO tracks (album_id, disc, position, title, length_ms, file_path)
		SELECT 1 + i / ?, 1, 1 + i % ?, 'Track', 200000, '/music/A' || (1 + i / ?) || '/' || (i % ?) || '.flac' FROM n`,
		tracks-1, tracks/albums, tracks/albums, tracks/albums, tracks/albums)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO books (ol_key, title, author, author_key, year, want_ebook, ebook_status, ebook_path, series_name)
		SELECT 'OL' || i || 'W', 'Book ' || i, 'Author ' || i % 700, 'OLA' || i % 700, 1900 + i % 120, 1,
		       CASE WHEN i % 3 = 0 THEN 'missing' ELSE 'downloaded' END, CASE WHEN i % 3 = 0 THEN '' ELSE '/books/b' || i || '.epub' END,
		       CASE WHEN i % 10 = 0 THEN 'Series ' || i % 50 ELSE '' END FROM n`, books)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO download_queue (movie_id, nzb_url, release_title, status, progress_pct, size_bytes, protocol, added_at)
		SELECT i, 'http://x/' || i, 'Movie.' || i || '.2020.1080p.BluRay.x264-GRP', CASE WHEN i % 10 = 0 THEN 'downloading' WHEN i % 7 = 0 THEN 'failed' ELSE 'queued' END,
		       (i % 100), 4000000000, 'usenet', strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || i || ' minutes') FROM n`, queue)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO blocklist (release_title, title_key, protocol, reason, movie_id, created_at)
		SELECT 'Release.' || i || '.1080p', 'release.' || i || '.1080p', 'usenet', 'failed', 1 + i % ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || i || ' minutes') FROM n`, blocklist, movies)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 30000)
		INSERT INTO activity (movie_id, event_type, message, created_at)
		SELECT 1 + i % ?, CASE WHEN i % 3 = 0 THEN 'grabbed' WHEN i % 3 = 1 THEN 'imported' ELSE 'search' END, 'Something happened to movie ' || i,
		       strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || i || ' minutes') FROM n`, movies)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 365)
		INSERT INTO stats_daily (day, movies, movies_have, shows, episodes_have, albums_have, library_bytes, completed, failed, download_bytes)
		SELECT date('now', '-' || i || ' days'), 100 + i, 90 + i, 10, 1000 + i, 50, 1000000000 * i, i % 9, i % 3, 5000000 * i FROM n`)
	run(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 3000)
		INSERT OR IGNORE INTO watched (kind, title_id, season, episode, plays, last_played) SELECT 'movie', i, 0, 0, 1, '2026-10-01T00:00:00Z' FROM n`)
	// File details for every file, as the background reader leaves them.
	run(`INSERT INTO media_info (path, size, mtime, scanned_at, checked_at, failed, info, container, vcodec, hdr, res, acodecs, alangs, slangs, duration)
		SELECT file_path, 1000, 1, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', 0,
		       '{"container":"matroska","video":{"codec":"HEVC","width":1920,"height":1080},"audio":[{"codec":"EAC3","channels":6,"language":"eng"}],"subtitles":[{"language":"eng","codec":"subrip"}]}',
		       'matroska', 'HEVC', '', '1080p', 'EAC3', 'eng', 'eng', 7000 FROM movies WHERE file_path IS NOT NULL`)
	run(`INSERT OR IGNORE INTO media_info (path, size, mtime, scanned_at, checked_at, failed, info, container, vcodec, hdr, res, acodecs, alangs, slangs, duration)
		SELECT file_path, 1000, 1, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', 0,
		       '{"container":"matroska","video":{"codec":"HEVC","width":1920,"height":1080},"audio":[{"codec":"EAC3","channels":6,"language":"eng"}],"subtitles":[{"language":"eng","codec":"subrip"}]}',
		       'matroska', 'HEVC', '', '1080p', 'EAC3', 'eng', 'eng', 2700 FROM episodes WHERE file_path IS NOT NULL`)
	run(`ANALYZE`)
}

type perfEnv struct {
	db     *sql.DB
	client *http.Client
	base   string
	srv    *api.Server
}

func newPerfEnv(t *testing.T) *perfEnv {
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "dl"),
		DownloadsIncomplete: filepath.Join(dir, "dl", "i"), DownloadsComplete: filepath.Join(dir, "dl", "c"), MoviesDir: filepath.Join(dir, "movies")}
	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := api.OpenTestDB(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := api.New(db, cfg, box, "", "perf")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	postJSON[map[string]any](t, c, ts.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester"}, http.StatusCreated)
	putJSONStatus(t, c, ts.URL+"/api/modules", map[string]any{"movies": true, "tv": true, "music": true, "ebooks": true, "audiobooks": true}, http.StatusOK)
	return &perfEnv{db: db, client: c, base: ts.URL, srv: srv}
}

// TestPerfHandlers times the hot API answers on a big library. It only runs
// with MEDIARIUM_PERF=1 (it fills a database with tens of thousands of rows).
func TestPerfHandlers(t *testing.T) {
	if os.Getenv("MEDIARIUM_PERF") == "" {
		t.Skip("set MEDIARIUM_PERF=1")
	}
	env := newPerfEnv(t)
	st := time.Now()
	fillBigLibrary(t, env.db, 5000, 800, 30000, 1000, 20000, 5000, 500, 10000)
	t.Logf("fill: %v", time.Since(st))
	paths := []string{
		"/api/movies", "/api/movies?brief=1", "/api/series", "/api/series?brief=1", "/api/books", "/api/music/artists",
		"/api/dashboard", "/api/activity", "/api/activity?limit=500", "/api/activity?q=Something", "/api/queue", "/api/queue/count",
		"/api/calendar", "/api/stats/library", "/api/stats/history", "/api/wanted", "/api/wanted?kind=cutoff", "/api/wanted?group=1",
		"/api/music/wanted", "/api/library/suggest?q=Movie+12", "/api/library/suggest?q=sho", "/api/library/counts", "/api/library/owned",
		"/api/library/folders", "/api/blocklist", "/api/blocklist?limit=50&offset=0", "/api/watched", "/api/health", "/api/modules",
		"/api/subtitles/wanted", "/api/tags", "/api/requests", "/api/lifecycle/shelf", "/api/library/file-details", "/api/movies/1", "/api/series/1",
		"/api/music/albums/1", "/api/music/artists/1", "/api/book-authors",
	}
	type row struct {
		path        string
		cold, warm  time.Duration
		bytes, code int
	}
	var rows []row
	for _, p := range paths {
		var r row
		r.path = p
		for i := 0; i < 3; i++ {
			s := time.Now()
			resp, err := env.client.Get(env.base + p)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			d := time.Since(s)
			r.code, r.bytes = resp.StatusCode, len(b)
			if i == 0 {
				r.cold = d
			} else if r.warm == 0 || d < r.warm {
				r.warm = d
			}
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].cold > rows[j].cold })
	for _, r := range rows {
		t.Logf("%-42s %3d %9s cold %9s warm %9s", r.path, r.code, strconv.Itoa(r.bytes)+"B", r.cold.Round(time.Microsecond), r.warm.Round(time.Microsecond))
	}
}

// TestPerfOne hits one path over and over, to profile it: MEDIARIUM_PERF=1
// MEDIARIUM_PERF_PATH=/api/health go test -run TestPerfOne -cpuprofile cpu.out
func TestPerfOne(t *testing.T) {
	path := os.Getenv("MEDIARIUM_PERF_PATH")
	if os.Getenv("MEDIARIUM_PERF") == "" || path == "" {
		t.Skip("set MEDIARIUM_PERF=1 and MEDIARIUM_PERF_PATH")
	}
	env := newPerfEnv(t)
	fillBigLibrary(t, env.db, 5000, 800, 30000, 1000, 20000, 5000, 500, 10000)
	n := 20
	if v, err := strconv.Atoi(os.Getenv("MEDIARIUM_PERF_N")); err == nil {
		n = v
	}
	st := time.Now()
	for i := 0; i < n; i++ {
		resp, err := env.client.Get(env.base + path)
		if err != nil {
			t.Fatal(err)
		}
		nb, _ := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if i == 0 {
			t.Logf("%s: status %d, %d bytes", path, resp.StatusCode, nb)
		}
	}
	t.Logf("%s: %v per call", path, time.Since(st)/time.Duration(n))
}

// TestPerfDeletes times removing titles that other tables point at (the
// blocklist, the queue, playlists): without an index on the pointing column
// every delete reads the whole of those tables.
func TestPerfDeletes(t *testing.T) {
	if os.Getenv("MEDIARIUM_PERF") == "" {
		t.Skip("set MEDIARIUM_PERF=1")
	}
	env := newPerfEnv(t)
	fillBigLibrary(t, env.db, 5000, 800, 30000, 1000, 20000, 5000, 500, 10000)
	timeIt := func(name, q string, ids int) {
		st := time.Now()
		for i := 1; i <= ids; i++ {
			if _, err := env.db.Exec(q, 10000+i); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%-28s %d deletes: %v (%v each)", name, ids, time.Since(st).Round(time.Millisecond), (time.Since(st) / time.Duration(ids)).Round(time.Microsecond))
	}
	// Ids that no row has: the cost is the lookup of rows that point at them.
	// Real ids are used for movies below.
	for i := 1; i <= 100; i++ {
		env.db.Exec(`UPDATE movies SET id = id WHERE id = ?`, i)
	}
	st := time.Now()
	for i := 1; i <= 100; i++ {
		if _, err := env.db.Exec(`DELETE FROM movies WHERE id = ?`, 4000+i); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("delete movie: 100 in %v", time.Since(st).Round(time.Millisecond))
	st = time.Now()
	for i := 1; i <= 20; i++ {
		if _, err := env.db.Exec(`DELETE FROM series WHERE id = ?`, 700+i); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("delete show: 20 in %v", time.Since(st).Round(time.Millisecond))
	st = time.Now()
	for i := 1; i <= 20; i++ {
		if _, err := env.db.Exec(`DELETE FROM albums WHERE id = ?`, 900+i); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("delete album (20 tracks each): 20 in %v", time.Since(st).Round(time.Millisecond))
	st = time.Now()
	for i := 1; i <= 100; i++ {
		if _, err := env.db.Exec(`DELETE FROM books WHERE id = ?`, 4000+i); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("delete book: 100 in %v", time.Since(st).Round(time.Millisecond))
	_ = timeIt
}

// TestPerfStartup times a first start (every migration) and a later start on
// a big database (nothing to apply), and api.New on that database.
func TestPerfStartup(t *testing.T) {
	if os.Getenv("MEDIARIUM_PERF") == "" {
		t.Skip("set MEDIARIUM_PERF=1")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	st := time.Now()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first start, all migrations: %v", time.Since(st).Round(time.Millisecond))
	fillBigLibrary(t, db, 5000, 800, 30000, 1000, 20000, 5000, 500, 10000)
	db.Close()
	st = time.Now()
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("later start on a big database: %v", time.Since(st).Round(time.Millisecond))
	box, _ := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	cfg := config.Config{ConfigDir: dir, DownloadsDir: filepath.Join(dir, "dl"), DownloadsIncomplete: filepath.Join(dir, "dl", "i"),
		DownloadsComplete: filepath.Join(dir, "dl", "c"), MoviesDir: filepath.Join(dir, "movies")}
	st = time.Now()
	if _, err := api.New(db, cfg, box, "", "perf"); err != nil {
		t.Fatal(err)
	}
	t.Logf("api.New on a big database: %v", time.Since(st).Round(time.Millisecond))
	db.Close()
}

// TestPerfMakeDB writes a big library into a new config folder, to start a
// real server on it and measure the web app in a browser:
// MEDIARIUM_PERF_DB=<folder> MEDIARIUM_PERF_MOVIES=2000 go test ./internal/api -run TestPerfMakeDB
// No picture addresses are saved, so the browser never asks a picture host.
func TestPerfMakeDB(t *testing.T) {
	dir := os.Getenv("MEDIARIUM_PERF_DB")
	if dir == "" {
		t.Skip("set MEDIARIUM_PERF_DB to a new folder")
	}
	movies := 2000
	if n, err := strconv.Atoi(os.Getenv("MEDIARIUM_PERF_MOVIES")); err == nil {
		movies = n
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fillBigLibrary(t, db, movies, 300, 9000, 300, 6000, 1000, 50, 1000)
	for _, q := range []string{`UPDATE movies SET poster_path = ''`, `UPDATE series SET poster_path = ''`,
		`INSERT OR REPLACE INTO settings (key, value, encrypted) VALUES ('onboarding.done', '1', 0)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
