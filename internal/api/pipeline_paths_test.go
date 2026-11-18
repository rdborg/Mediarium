package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// These tests walk a grab through the real pipeline (fake indexer file, fake
// Usenet server, real unpacking, import, history) for the ways a download
// arrives packed or broken: a zip, an archive saved under a video's name, and
// a file that is already in the library.

// zipOf builds a zip holding the given files.
func zipOf(t *testing.T, files ...fixtureFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// packedVolume is the start of a RAR 5 volume, what a release that was never
// unpacked leaves behind under a video's name.
func packedVolume() []byte {
	return append([]byte("Rar!\x1a\x07\x01\x00"), make([]byte, 4096)...)
}

// A movie that comes in a zip is unpacked, filed in the library, and the
// movie's own history says it was imported.
func TestMovieInAZipIsUnpackedImportedAndLogged(t *testing.T) {
	env := newChecksEnv(t)
	url := env.release("zipped", "zipped", []fixtureFile{{"release.zip", zipOf(t, movieFile)}})
	it := env.finish(t, env.grab(t, "The.Fixture.Movie.1999.1080p.WEB-DL.x264-ZIPPED", url, 1000))
	if it.Status != queue.StatusCompleted {
		t.Fatalf("status = %s error = %q; want completed", it.Status, it.Error)
	}
	m, err := env.server.MovieRepo.Get(env.movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != library.StatusDownloaded || m.FilePath == "" {
		t.Fatalf("movie = %+v; want downloaded with a file", m)
	}
	got, err := os.ReadFile(m.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, movieFile.data) {
		t.Fatal("the filed movie is not what was inside the zip")
	}
	waitFor(t, "the movie's history to say it was imported", func() bool {
		return findEvent(movieEvents(t, env.client, env.base, env.movie.ID), "imported", "") != nil
	})
	if !env.activityHas(t, "imported", "") {
		t.Error("the activity feed has no import line")
	}
}

// An archive saved under a video's name is not a video: the download fails with
// that reason, the release is blocklisted and the movie is wanted again. Nothing
// is filed.
func TestMovieThatIsAnArchiveUnderAVideoNameIsTurnedAway(t *testing.T) {
	env := newChecksEnv(t)
	url := env.release("packed", "packed", []fixtureFile{{"The.Fixture.Movie.1999.1080p.mkv", packedVolume()}})
	const title = "The.Fixture.Movie.1999.1080p.WEB-DL.x264-PACKED"
	it := env.finish(t, env.grab(t, title, url, 1000))
	if it.Status != queue.StatusFailed || !strings.Contains(it.Error, "not a video") {
		t.Fatalf("status = %s error = %q; want failed as not a video", it.Status, it.Error)
	}
	waitFor(t, "the release to be on the blocklist", func() bool {
		keys, _ := env.server.Blocklist.Keys()
		return keys[blocklist.Key(title)]
	})
	waitFor(t, "the movie to be missing again", func() bool { return env.movieStatus(t) == library.StatusMissing })
	m, _ := env.server.MovieRepo.Get(env.movie.ID)
	if m.FilePath != "" {
		t.Fatalf("nothing should be filed, got %s", m.FilePath)
	}
	if e := findEvent(movieEvents(t, env.client, env.base, env.movie.ID), "failed", "not a video"); e == nil {
		t.Fatalf("the movie's history should say why it failed: %+v", movieEvents(t, env.client, env.base, env.movie.ID))
	}
}

func episodeEvents(t *testing.T, e *tvEnv) []map[string]any {
	t.Helper()
	return getJSON[[]map[string]any](t, e.client, fmt.Sprintf("%s/api/series/%d/events", e.httpSrv.URL, e.seriesID))
}

// A season pack inside a zip: every episode is unpacked and filed, and the
// show's history has the import line.
func TestSeasonPackInAZipImportsEveryEpisode(t *testing.T) {
	e1 := []byte("episode one " + strings.Repeat("a", 400))
	e2 := []byte("episode two " + strings.Repeat("b", 400))
	pack := zipOf(t,
		fixtureFile{"Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", e1},
		fixtureFile{"Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", e2},
	)
	env := newTVEnv(t, map[string]nntpArticle{"pack@example": {fileName: "Fixture.Show.S01.1080p.BluRay.x264-GRP.zip", content: pack}})
	env.grab(t, "Fixture.Show.S01.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("status=%q error=%q; want completed", status, errMsg)
	}
	for name, content := range map[string][]byte{"Fixture Show - S01E01 - Pilot.mkv": e1, "Fixture Show - S01E02 - Second.mkv": e2} {
		got, err := os.ReadFile(filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01", name))
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		if !bytes.Equal(got, content) {
			t.Fatalf("%s: content mismatch", name)
		}
	}
	if s := env.episodeStatuses(t); s["S01E01"] != "downloaded" || s["S01E02"] != "downloaded" || s["S02E01"] != "missing" {
		t.Fatalf("statuses = %v", s)
	}
	waitFor(t, "the show's history to say it was imported", func() bool {
		return findEvent(episodeEvents(t, env), "imported", "") != nil
	})
}

// A pack where one "episode" is an archive under a video's name: that file is
// left out, the real one is filed, and the history says what was skipped.
func TestSeasonPackLeavesOutAnArchiveSavedAsAVideo(t *testing.T) {
	e1 := []byte("episode one " + strings.Repeat("a", 400))
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: e1},
		"e2@example": {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: packedVolume()},
	})
	env.grab(t, "Fixture.Show.S01.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("status=%q error=%q; want completed", status, errMsg)
	}
	if s := env.episodeStatuses(t); s["S01E01"] != "downloaded" || s["S01E02"] != "missing" {
		t.Fatalf("statuses = %v; want E01 downloaded and E02 missing", s)
	}
	waitFor(t, "the history to say the archive was left out", func() bool {
		return findEvent(episodeEvents(t, env), "skipped", "not a video") != nil
	})
	if entries, _ := os.ReadDir(filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")); len(entries) != 1 {
		t.Fatalf("only the real episode should be filed, got %d files", len(entries))
	}
}

// A single episode that is an archive under a video's name fails the download
// (there is nothing else in it) and the episode goes back to missing.
func TestEpisodeThatIsAnArchiveUnderAVideoNameFails(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{"e2@example": {fileName: "release.mkv", content: packedVolume()}})
	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	status, errMsg := env.waitForTerminal(t)
	if status != "failed" || !strings.Contains(errMsg, "not a video") {
		t.Fatalf("status=%q error=%q; want failed as not a video", status, errMsg)
	}
	if s := env.episodeStatuses(t); s["S01E02"] != "missing" {
		t.Fatalf("statuses = %v; want E02 missing", s)
	}
}

// With "If a file already exists" on ask, a pack for a TV show cannot park for
// a decision, so it behaves like skip: the file in the library stays, the
// download finishes as a decision (not a failure) and the history says so.
func TestTVFileAlreadyThereIsKeptWhenTheChoiceIsToAsk(t *testing.T) {
	content := []byte("a different copy " + strings.Repeat("x", 500))
	env := newTVEnv(t, map[string]nntpArticle{"ep-two@example": {fileName: "release-file.mkv", content: content}})
	postJSONMethod[map[string]any](t, env.client, http.MethodPut, env.httpSrv.URL+"/api/settings", map[string]any{
		"importConflictPolicy": "ask",
	}, http.StatusOK)
	season := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(season, "Fixture Show - S01E02 - Second.mkv")
	if err := os.WriteFile(existing, []byte("the file already there"), 0o644); err != nil {
		t.Fatal(err)
	}

	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("status=%q error=%q; want completed (kept)", status, errMsg)
	}
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the file already there" {
		t.Fatalf("the file in the library was changed: %q", got)
	}
	waitFor(t, "the history to say the file was kept", func() bool {
		return findEvent(episodeEvents(t, env), "kept", "") != nil
	})
	// The show had no file for that episode: the one that was kept is its file
	// now, so no later search fetches the same release again.
	if s := env.episodeStatuses(t); s["S01E02"] != "downloaded" {
		t.Fatalf("the kept file should be the episode's file, statuses = %v", s)
	}
	ep, err := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 2)
	if err != nil || ep.FilePath != existing {
		t.Fatalf("episode file = %q (%v), want %q", ep.FilePath, err, existing)
	}
}

// An ebook that comes in a zip is unpacked and filed, and the activity feed
// has the grab and the import.
func TestEbookInAZipIsUnpackedImportedAndLogged(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	root := t.TempDir()
	ebooks, audiobooks := filepath.Join(root, "ebooks"), filepath.Join(root, "audiobooks")
	for _, d := range []string{ebooks, audiobooks} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks, "audiobooksPath": audiobooks}, http.StatusOK)
	book := postJSON[map[string]any](t, client, base+"/api/books", map[string]any{
		"olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "ebook": true,
	}, http.StatusCreated)

	epub := []byte("epub " + strings.Repeat("e", 300))
	articles := map[string]nntpArticle{
		"z1@x": {fileName: "The Hobbit.zip", content: zipOf(t, fixtureFile{"The Hobbit.epub", epub}, fixtureFile{"release.nfo", []byte("nfo")})},
	}
	host, port := newArticleNNTPServer(t, articles)
	idx := newMusicIndexer(t, []string{"J.R.R. Tolkien - The Hobbit (1937) [EPUB]"}, nzbFor(articles))
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "Books", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	bookURL := fmt.Sprintf("%s/api/books/%v", base, book["id"])
	if now := postJSON[map[string]any](t, client, bookURL+"/search?format=ebook", nil, http.StatusOK); now["grabbed"] != float64(1) {
		t.Fatalf("search now: %v", now)
	}
	waitBackground(t, server)

	want := filepath.Join(ebooks, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.epub")
	if got, err := os.ReadFile(want); err != nil || !bytes.Equal(got, epub) {
		t.Fatalf("ebook at %s: %q (%v)", want, got, err)
	}
	if e := getJSON[map[string]any](t, client, bookURL)["ebook"].(map[string]any); e["status"] != "downloaded" || e["format"] != "epub" {
		t.Fatalf("book after import: %v", e)
	}
	kinds := map[string]bool{}
	for _, a := range getJSON[[]map[string]any](t, client, base+"/api/activity") {
		kinds[fmt.Sprint(a["eventType"])] = true
	}
	if !kinds["grabbed"] || !kinds["imported"] {
		t.Fatalf("the activity feed should have the grab and the import, got %v", kinds)
	}
}

// A site that answers 500 is skipped: the search still returns what the other
// site found, and the site that failed is named as the problem.
func TestSearchSurvivesAnIndexerThatAnswers500(t *testing.T) {
	_, base, client := loginNewServer(t)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Broken Indexer", "definitionId": "fixture", "baseUrl": broken.URL, "apiKey": "k",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": newFakeIndexerServer(t).URL, "apiKey": "k",
	}, http.StatusCreated)

	results := getJSON[[]map[string]any](t, client, base+"/api/search?q=fixture")
	if len(results) != 1 || results[0]["indexerName"] != "Fixture Indexer" {
		t.Fatalf("results = %+v; want the working site's one release", results)
	}
}

// The NZB itself cannot be fetched (the site answers 500): the download fails
// with a reason that names the problem, nothing is blocklisted (the release is
// not at fault) and the movie is wanted again.
func TestGrabWhenTheNZBCannotBeFetchedFailsWithoutBlocklisting(t *testing.T) {
	env := newChecksEnv(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(down.Close)
	const title = "The.Fixture.Movie.1999.1080p.WEB-DL.x264-DOWN"
	it := env.finish(t, env.grab(t, title, down.URL+"/x.nzb", 1000))
	if it.Status != queue.StatusFailed || it.Error == "" {
		t.Fatalf("status = %s error = %q; want failed with a reason", it.Status, it.Error)
	}
	if !strings.Contains(it.Error, "500") {
		t.Errorf("the reason should say what the site answered: %q", it.Error)
	}
	waitFor(t, "the movie to be wanted again", func() bool { return env.movieStatus(t) == library.StatusMissing })
	if keys, _ := env.server.Blocklist.Keys(); keys[blocklist.Key(title)] {
		t.Error("a site that is down must not blocklist the release")
	}
}

// The switch "Extra languages don't change the ranking" reaches the ranking of
// the release list and of what an automatic search takes. With German and
// English wanted, a MULTi release that has German beats a plain German one
// even when its quality is lower. With the switch on the two count the same
// for the language, so the better quality is the best match.
func TestExtraLanguagesSwitchChangesWhichReleaseIsTheBestMatch(t *testing.T) {
	server, base, client := loginNewServer(t)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	const multi = "The.Matrix.1999.MULTi.GERMAN.1080p.WEB-DL.x264-GRP"
	const plain = "The.Matrix.1999.GERMAN.1080p.BluRay.x264-GRP"
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "apiKey": "k", "baseUrl": newTVIndexerWith(t, []string{multi, plain}).URL,
	}, http.StatusCreated)

	best := func() string {
		var found string
		for _, r := range getJSON[[]map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/search", base, m.ID)) {
			if r["best"] == true {
				if found != "" {
					t.Fatalf("two best matches: %s and %v", found, r["title"])
				}
				found = fmt.Sprint(r["title"])
			}
		}
		return found
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{
		"qualityLanguage": "German,English", "qualityLanguageIgnoreExtra": false,
	}, http.StatusOK)
	if got := best(); got != multi {
		t.Fatalf("switch off: best match = %q, want the MULTi release %q", got, multi)
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{
		"qualityLanguageIgnoreExtra": true,
	}, http.StatusOK)
	s := getJSON[map[string]any](t, client, base+"/api/settings")
	if s["qualityLanguageIgnoreExtra"] != true || s["qualityLanguage"] != "German,English" {
		t.Fatalf("the switch did not stick: %v %v", s["qualityLanguageIgnoreExtra"], s["qualityLanguage"])
	}
	if got := best(); got != plain {
		t.Fatalf("switch on: best match = %q, want the Bluray release %q", got, plain)
	}

	// An automatic search takes the same one.
	server.TestHunt(context.Background())
	items := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(items) != 1 || items[0]["releaseTitle"] != plain {
		t.Fatalf("the automatic search took %+v, want %s", items, plain)
	}
}

// A movie with no file on record, and a file already at its place: the
// download is kept as a decision and the file that is there becomes the
// movie's file. Before, the movie stayed wanted and the next search fetched
// the same release again.
func TestKeptFileBecomesTheFileOfAMovieThatHadNone(t *testing.T) {
	movieContent := []byte("a download that is not needed")
	server, client, httpSrv, movie, existingPath := newImportConflictEnv(t, movieContent)
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{
		"importConflictPolicy": "skip",
	}, http.StatusOK)

	item := grabFixtureAndWaitForEnd(t, client, httpSrv, movie, movieContent)
	server.TestWaitBackground()
	if item["status"] != "completed" || item["error"] != "Kept the file you already have." {
		t.Fatalf("keeping the file is a decision, got %+v", item)
	}
	if got, _ := os.ReadFile(existingPath); string(got) != "pre-existing file content" {
		t.Fatalf("the file that was there must stay as it is, got %q", got)
	}
	m, err := server.MovieRepo.Get(movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != library.StatusDownloaded || m.FilePath != existingPath {
		t.Fatalf("movie = %s %q; want downloaded with %q", m.Status, m.FilePath, existingPath)
	}
	if findEvent(movieEvents(t, client, httpSrv.URL, movie.ID), "imported", "linked it") == nil {
		t.Fatalf("the movie's history should say the file was linked: %+v", movieEvents(t, client, httpSrv.URL, movie.ID))
	}
}

// "Look for a better copy" on a movie that is set to leave what it has alone
// says that, not that nothing suitable was found.
func TestLookForABetterCopySaysWhenBetterVersionsIsOff(t *testing.T) {
	server, base, client := loginNewServer(t)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "apiKey": "k", "baseUrl": newTVIndexerWith(t, []string{"The.Matrix.1999.1080p.BluRay.x264-GRP"}).URL,
	}, http.StatusCreated)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true, NoUpgrade: true})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "The Matrix (1999).mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-720p", file); err != nil {
		t.Fatal(err)
	}
	r := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/search-now", base, m.ID), nil, http.StatusOK)
	if r["grabbed"] != nil && r["grabbed"] != float64(0) {
		t.Fatalf("a movie set to leave its file alone must not be searched again: %v", r)
	}
	if msg, _ := r["message"].(string); !strings.HasPrefix(msg, "Better versions is off for this movie") {
		t.Fatalf("message = %q; want it to say Better versions is off", msg)
	}
}
