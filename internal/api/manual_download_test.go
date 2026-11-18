package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// manualForm builds the multipart form the "add a download by hand" box sends.
type manualForm struct {
	fields   map[string]string
	fileName string
	file     []byte
}

func (f manualForm) body() (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range f.fields {
		w.WriteField(k, v)
	}
	if f.file != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, f.fileName))
		h.Set("Content-Type", "application/octet-stream")
		part, _ := w.CreatePart(h)
		part.Write(f.file)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

// sendManual posts the form and returns the status and decoded body.
func (e *checksEnv) sendManual(t *testing.T, f manualForm) (int, map[string]any) {
	t.Helper()
	body, ctype := f.body()
	req, err := http.NewRequest(http.MethodPost, e.base+"/api/downloads/manual", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ctype)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// holdTheLine keeps new downloads waiting in line (they may only start in two
// hours' time), so a test can look at what was queued.
func (e *checksEnv) holdTheLine(t *testing.T) {
	t.Helper()
	h := time.Now().Hour()
	e.put(t, map[string]any{"downloadHours": fmt.Sprintf("%d-%d", (h+2)%24, (h+3)%24)})
}

func (e *checksEnv) nzbText(path string) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return []byte(e.nzbs["/"+path+".nzb"])
}

// A tiny valid .torrent: one file of 5 bytes, one 20-byte piece hash.
func torrentFor(name string) []byte {
	const announce = "http://tracker.test/a"
	return []byte(fmt.Sprintf("d8:announce%d:%s4:infod6:lengthi5e4:name%d:%s12:piece lengthi16384e6:pieces20:AAAAAAAAAAAAAAAAAAAAee", len(announce), announce, len(name), name))
}

func torrentWithFiles(name string, files map[string]int) []byte {
	var b strings.Builder
	b.WriteString("d4:infod5:filesl")
	for n, size := range files {
		fmt.Fprintf(&b, "d6:lengthi%de4:pathl%d:%see", size, len(n), n)
	}
	fmt.Fprintf(&b, "e4:name%d:%s12:piece lengthi16384e6:pieces20:AAAAAAAAAAAAAAAAAAAAee", len(name), name)
	return []byte(b.String())
}

const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Some.Movie.2020.1080p"

func TestManualNZBUploadForAMovieDownloadsAndImports(t *testing.T) {
	env := newChecksEnv(t)
	env.release("fixture", "man", []fixtureFile{movieFile})
	status, body := env.sendManual(t, manualForm{
		fields:   map[string]string{"movieId": strconv.FormatInt(env.movie.ID, 10)},
		fileName: "The.Fixture.Movie.1999.1080p.WEB-DL.x264-HAND.nzb",
		file:     env.nzbText("fixture"),
	})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	if body["releaseTitle"] != "The.Fixture.Movie.1999.1080p.WEB-DL.x264-HAND" || body["protocol"] != "usenet" {
		t.Errorf("response = %v", body)
	}
	it := env.finish(t, int64(body["queueId"].(float64)))
	if it.Status != queue.StatusCompleted {
		t.Fatalf("status = %s (%s), want completed", it.Status, it.Error)
	}
	if !strings.HasPrefix(it.NZBURL, "upload:") {
		t.Errorf("the queue keeps %q; want a reference to the stored file", it.NZBURL)
	}
	if got := env.movieStatus(t); got != library.StatusDownloaded {
		t.Errorf("movie is %s, want downloaded", got)
	}
}

// A file called "m2.nzb" says nothing about the release. The files inside the NZB do,
// so the download goes by their name and the quality is read from it.
func TestManualNZBWithAnUnhelpfulFileNameTakesTheNameFromInside(t *testing.T) {
	env := newChecksEnv(t)
	env.release("fixture", "inside", []fixtureFile{movieFile})
	status, body := env.sendManual(t, manualForm{
		fields:   map[string]string{"movieId": strconv.FormatInt(env.movie.ID, 10)},
		fileName: "m2.nzb",
		file:     env.nzbText("fixture"),
	})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	if body["releaseTitle"] != "The.Fixture.Movie.1999.1080p" {
		t.Errorf("the name should come from the file inside the NZB, got %v", body["releaseTitle"])
	}
}

// A name the person typed still wins over everything, and a file name that is a real
// release name is not replaced by the one inside.
func TestManualNZBKeepsTheTypedNameAndAGoodFileName(t *testing.T) {
	env := newChecksEnv(t)
	env.release("fixture", "keep", []fixtureFile{movieFile})
	status, body := env.sendManual(t, manualForm{
		fields:   map[string]string{"movieId": strconv.FormatInt(env.movie.ID, 10), "name": "My.Own.Name.1999.720p.WEB-DL-ME"},
		fileName: "m2.nzb",
		file:     env.nzbText("fixture"),
	})
	if status != http.StatusAccepted || body["releaseTitle"] != "My.Own.Name.1999.720p.WEB-DL-ME" {
		t.Fatalf("typed name: status %d, %v", status, body)
	}
}

func TestManualNZBFromAnAddressIsReadOnceAndStored(t *testing.T) {
	env := newChecksEnv(t)
	url := env.release("The.Fixture.Movie.1999.720p.WEB-DL.x264-URL", "url", []fixtureFile{movieFile})
	status, body := env.sendManual(t, manualForm{fields: map[string]string{"movieId": strconv.FormatInt(env.movie.ID, 10), "url": url}})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	if body["releaseTitle"] != "The.Fixture.Movie.1999.720p.WEB-DL.x264-URL" {
		t.Errorf("the name should come from the address: %v", body["releaseTitle"])
	}
	// The address is not needed again: take the NZB away before the download.
	it := env.finish(t, int64(body["queueId"].(float64)))
	if it.Status != queue.StatusCompleted {
		t.Fatalf("status = %s (%s)", it.Status, it.Error)
	}
	if strings.Contains(it.NZBURL, "http") {
		t.Errorf("the queue keeps %q; the address (which may hold a key) should not be stored", it.NZBURL)
	}
}

func TestManualMagnetAndTorrentAreQueuedAsTorrents(t *testing.T) {
	env := newChecksEnv(t)
	env.holdTheLine(t)
	movie := strconv.FormatInt(env.movie.ID, 10)

	status, body := env.sendManual(t, manualForm{fields: map[string]string{"movieId": movie, "url": magnet}})
	if status != http.StatusAccepted {
		t.Fatalf("magnet: status %d, body %v", status, body)
	}
	if body["protocol"] != "torrent" || body["releaseTitle"] != "Some.Movie.2020.1080p" {
		t.Errorf("magnet response = %v", body)
	}
	it, err := env.server.QueueRepo.Get(int64(body["queueId"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	if it.NZBURL != magnet || it.Protocol != queue.ProtocolTorrent || it.Status != queue.StatusQueued || it.MovieID != env.movie.ID {
		t.Errorf("queued item = %+v", it)
	}
	// One download per title at a time, as for any grab by hand.
	status, _ = env.sendManual(t, manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.torrent", file: torrentFor("movie.mkv")})
	if status != http.StatusConflict {
		t.Errorf("second download for the same movie: status %d, want 409", status)
	}
	if err := env.server.QueueRepo.SetStatus(it.ID, queue.StatusStopped, ""); err != nil {
		t.Fatal(err)
	}

	status, body = env.sendManual(t, manualForm{fields: map[string]string{"movieId": movie}, fileName: "Another.Movie.2021.torrent", file: torrentFor("Name.In.Torrent.2021.mkv")})
	if status != http.StatusAccepted {
		t.Fatalf("torrent file: status %d, body %v", status, body)
	}
	if body["protocol"] != "torrent" || body["releaseTitle"] != "Name.In.Torrent.2021.mkv" || body["sizeBytes"] != float64(5) {
		t.Errorf("torrent response = %v", body)
	}
	it, _ = env.server.QueueRepo.Get(int64(body["queueId"].(float64)))
	if !strings.HasPrefix(it.NZBURL, "upload:") || !strings.HasSuffix(it.NZBURL, ".torrent") {
		t.Errorf("the queue keeps %q", it.NZBURL)
	}
}

func TestManualDownloadForAnEpisodeAndASeason(t *testing.T) {
	env := newChecksEnv(t)
	env.holdTheLine(t)
	tvRoot := filepath.Join(t.TempDir(), "tv")
	if err := os.MkdirAll(tvRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	env.put(t, map[string]any{"tvPath": tvRoot})
	series, err := env.server.MovieRepo.AddSeries(library.Series{TMDBID: 1399, Title: "Fixture Show", Year: 2011, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, Title: "One", Status: library.StatusMissing, Monitored: true},
		{Season: 1, Episode: 2, Title: "Two", Status: library.StatusMissing, Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	sid := strconv.FormatInt(series.ID, 10)
	nzb := func(name string) []byte {
		env.release(name, name, []fixtureFile{movieFile})
		return env.nzbText(name)
	}

	tests := []struct {
		name        string
		fields      map[string]string
		fileName    string
		wantStatus  int
		wantEpisode int
		wantMessage string
	}{
		{"an episode with no marker in the name", map[string]string{"seriesId": sid, "season": "1", "episode": "2"}, "randomname.nzb", http.StatusAccepted, 2, ""},
		{"the whole season with no marker", map[string]string{"seriesId": sid, "season": "1"}, "pack1.nzb", http.StatusAccepted, 0, ""},
		{"a name that agrees with the choice", map[string]string{"seriesId": sid, "season": "1", "episode": "1"}, "Fixture.Show.S01E01.1080p.WEB-DL.nzb", http.StatusAccepted, 1, ""},
		{"a name for another episode", map[string]string{"seriesId": sid, "season": "1", "episode": "1"}, "Fixture.Show.S01E02.1080p.WEB-DL.nzb", http.StatusBadRequest, 0, "says episode 2, but you chose episode 1"},
		{"a name for another season", map[string]string{"seriesId": sid, "season": "1", "episode": "1"}, "Fixture.Show.S02E01.1080p.WEB-DL.nzb", http.StatusBadRequest, 0, "says season 2, but you chose season 1"},
		{"a season pack chosen as an episode", map[string]string{"seriesId": sid, "season": "1", "episode": "1"}, "Fixture.Show.S01.1080p.WEB-DL.nzb", http.StatusBadRequest, 0, "whole season"},
		{"an episode chosen as the whole season", map[string]string{"seriesId": sid, "season": "1"}, "Fixture.Show.S01E01.1080p.WEB-DL.nzb", http.StatusBadRequest, 0, "an episode"},
		{"an episode the show doesn't list", map[string]string{"seriesId": sid, "season": "1", "episode": "9"}, "x.nzb", http.StatusNotFound, 0, "isn't listed"},
		{"no season", map[string]string{"seriesId": sid, "episode": "1"}, "x.nzb", http.StatusBadRequest, 0, "season"},
		{"a show that isn't there", map[string]string{"seriesId": "9999", "season": "1", "episode": "1"}, "x.nzb", http.StatusNotFound, 0, "isn't in your library"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case starts without a download waiting for the show.
			items, _ := env.server.QueueRepo.List()
			for _, it := range items {
				_ = env.server.QueueRepo.SetStatus(it.ID, queue.StatusStopped, "")
			}
			eps, _ := env.server.MovieRepo.ListEpisodes(series.ID)
			for _, ep := range eps {
				_ = env.server.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusMissing, "", "")
			}
			status, body := env.sendManual(t, manualForm{fields: tt.fields, fileName: tt.fileName, file: nzb(strings.TrimSuffix(tt.fileName, ".nzb"))})
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %v)", status, tt.wantStatus, body)
			}
			if tt.wantMessage != "" {
				if msg, _ := body["error"].(string); !strings.Contains(msg, tt.wantMessage) {
					t.Errorf("error = %q, want it to contain %q", msg, tt.wantMessage)
				}
				return
			}
			it, err := env.server.QueueRepo.Get(int64(body["queueId"].(float64)))
			if err != nil {
				t.Fatal(err)
			}
			if it.SeriesID != series.ID || it.Season != 1 || it.Episode != tt.wantEpisode {
				t.Errorf("queued for series %d season %d episode %d, want series %d season 1 episode %d", it.SeriesID, it.Season, it.Episode, series.ID, tt.wantEpisode)
			}
		})
	}
}

func TestManualDownloadRefusesBadInput(t *testing.T) {
	env := newChecksEnv(t)
	env.holdTheLine(t)
	movie := strconv.FormatInt(env.movie.ID, 10)
	good := func() []byte { env.release("good", "good", []fixtureFile{movieFile}); return env.nzbText("good") }
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("a.txt")
	w.Write([]byte("hi"))
	zw.Close()

	tests := []struct {
		name       string
		form       manualForm
		wantStatus int
		wantText   string
	}{
		{"nothing at all", manualForm{fields: map[string]string{"movieId": movie}}, 400, "Choose an NZB"},
		{"no title", manualForm{fileName: "a.nzb", file: good()}, 400, "Choose the movie"},
		{"a movie and a show", manualForm{fields: map[string]string{"movieId": movie, "seriesId": "1"}, fileName: "a.nzb", file: good()}, 400, "not both"},
		{"a movie that isn't there", manualForm{fields: map[string]string{"movieId": "9999"}, fileName: "a.nzb", file: good()}, 404, "isn't in your library"},
		{"a file and an address", manualForm{fields: map[string]string{"movieId": movie, "url": magnet}, fileName: "a.nzb", file: good()}, 400, "not both"},
		{"an empty file", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: []byte{}}, 400, "empty"},
		{"a web page", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: []byte("<html><body>Not found</body></html>")}, 400, "working NZB"},
		{"an NZB with no files", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: []byte(`<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"></nzb>`)}, 400, "doesn't list any files"},
		{"an NZB cut off half way", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: good()[:90]}, 400, "working NZB"},
		{"an NZB with a bad article id", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: []byte(`<?xml version="1.0"?><nzb><file subject="&quot;a.mkv&quot;"><groups><group>alt.test</group></groups><segments><segment bytes="1" number="1">bad id with spaces</segment></segments></file></nzb>`)}, 400, "working NZB"},
		{"a zip file", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: zbuf.Bytes()}, 400, "isn't an NZB or a .torrent"},
		{"a broken torrent", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.torrent", file: []byte("d4:infod4:name3:ab")}, 400, "working .torrent"},
		{"a torrent with nothing in it", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.torrent", file: []byte("d4:infod5:filesle4:name1:a12:piece lengthi16384e6:pieces0:ee")}, 400, ""},
		{"a magnet link with no hash", manualForm{fields: map[string]string{"movieId": movie, "url": "magnet:?dn=Something"}}, 400, "magnet link isn't valid"},
		{"a file address", manualForm{fields: map[string]string{"movieId": movie, "url": "file:///etc/passwd"}}, 400, "http://"},
		{"an ftp address", manualForm{fields: map[string]string{"movieId": movie, "url": "ftp://example.com/a.nzb"}}, 400, "http://"},
		{"not an address", manualForm{fields: map[string]string{"movieId": movie, "url": "hello there"}}, 400, "http://"},
		{"a cloud metadata address", manualForm{fields: map[string]string{"movieId": movie, "url": "http://169.254.169.254/latest/meta-data/"}}, 502, "Couldn't get a file"},
		{"an address that is not found", manualForm{fields: map[string]string{"movieId": movie, "url": env.srv.URL + "/nothing-here.nzb"}}, 502, "Couldn't get a file"},
		{"a program in the NZB", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.nzb", file: func() []byte {
			env.release("withexe", "exe", []fixtureFile{movieFile, playerExe})
			return env.nzbText("withexe")
		}()}, 422, "programs or scripts"},
		{"a program in the torrent", manualForm{fields: map[string]string{"movieId": movie}, fileName: "a.torrent", file: torrentWithFiles("Release", map[string]int{"movie.mkv": 100, "setup.exe": 5})}, 422, "setup.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := env.sendManual(t, tt.form)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %v)", status, tt.wantStatus, body)
			}
			if msg, _ := body["error"].(string); !strings.Contains(msg, tt.wantText) || msg == "" {
				t.Errorf("error = %q, want it to contain %q", msg, tt.wantText)
			}
		})
	}
	if items, _ := env.server.QueueRepo.List(); len(items) != 0 {
		t.Errorf("%d downloads were queued from input that should have been refused", len(items))
	}
	// A refused request stores nothing.
	if entries, _ := os.ReadDir(filepath.Join(env.server.TestConfigDir(), "uploads")); len(entries) != 0 {
		t.Errorf("%d files were stored from input that should have been refused", len(entries))
	}
}

func TestManualDownloadAllowsAProgramWhenTheCheckIsOff(t *testing.T) {
	env := newChecksEnv(t)
	env.holdTheLine(t)
	env.put(t, map[string]any{"rejectExecutables": false})
	env.release("withexe", "exe", []fixtureFile{movieFile, playerExe})
	status, body := env.sendManual(t, manualForm{fields: map[string]string{"movieId": strconv.FormatInt(env.movie.ID, 10)}, fileName: "a.nzb", file: env.nzbText("withexe")})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
}

func TestManualDownloadNeedsAUsenetServerForAnNZB(t *testing.T) {
	server, base, client := newControlServer(t, false)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	env := &checksEnv{server: server, base: base, client: client, movie: m, nzbs: map[string]string{}, bodies: map[string][]byte{}}
	env.srv = httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(env.srv.Close)
	env.release("x", "x", []fixtureFile{movieFile})
	status, body := env.sendManual(t, manualForm{fields: map[string]string{"movieId": strconv.FormatInt(m.ID, 10)}, fileName: "a.nzb", file: env.nzbText("x")})
	if status != http.StatusPreconditionFailed || !strings.Contains(fmt.Sprint(body["error"]), "Usenet provider") {
		t.Errorf("status = %d, body %v; want 412 asking for a Usenet provider", status, body)
	}
}

func TestManualDownloadSizeLimits(t *testing.T) {
	env := newChecksEnv(t)
	env.holdTheLine(t)
	movie := strconv.FormatInt(env.movie.ID, 10)
	big := bytes.Repeat([]byte("x"), 33<<20)
	status, body := env.sendManual(t, manualForm{fields: map[string]string{"movieId": movie}, fileName: "big.nzb", file: big})
	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("a 33 MB upload: status %d, body %v; want 413", status, body)
	}
}

func TestManualDownloadIsForAdministrators(t *testing.T) {
	env := newChecksEnv(t)
	// Not signed in at all.
	anon := &http.Client{}
	body, ctype := manualForm{fields: map[string]string{"movieId": "1"}, fileName: "a.nzb", file: []byte("<nzb/>")}.body()
	req, _ := http.NewRequest(http.MethodPost, env.base+"/api/downloads/manual", body)
	req.Header.Set("Content-Type", ctype)
	resp, err := anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", resp.StatusCode)
	}
}

func TestUploadedFilesNobodyUsesArePruned(t *testing.T) {
	env := newChecksEnv(t)
	env.holdTheLine(t)
	env.release("keep", "keep", []fixtureFile{movieFile})
	status, body := env.sendManual(t, manualForm{fields: map[string]string{"movieId": strconv.FormatInt(env.movie.ID, 10)}, fileName: "a.nzb", file: env.nzbText("keep")})
	if status != http.StatusAccepted {
		t.Fatalf("status %d, body %v", status, body)
	}
	dir := filepath.Join(env.server.TestConfigDir(), "uploads")
	stray := filepath.Join(dir, strings.Repeat("a", 32)+".nzb")
	fresh := filepath.Join(dir, strings.Repeat("b", 32)+".nzb")
	foreign := filepath.Join(dir, "notes.txt")
	for _, p := range []string{stray, fresh, foreign} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{stray, foreign} {
		os.Chtimes(p, old, old)
	}
	if n := env.server.TestPruneUploads(time.Now()); n != 1 {
		t.Errorf("pruned %d files, want only the unused old one", n)
	}
	for p, want := range map[string]bool{stray: false, fresh: true, foreign: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s present = %v, want %v", filepath.Base(p), err == nil, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 { // the one in use, the fresh one, the foreign one
		t.Errorf("%d files left, want 3", len(entries))
	}
}
