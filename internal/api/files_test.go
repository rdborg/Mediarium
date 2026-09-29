package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/settings"
)

func writeBody(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTitleFilesAndStreaming(t *testing.T) {
	server, base, _, member, _ := familyServer(t)
	dir := t.TempDir()
	movies, tv := filepath.Join(dir, "movies"), filepath.Join(dir, "tv")
	secret := filepath.Join(dir, "config", "secret.mp4")
	writeBody(t, secret, "do not serve")
	for k, v := range map[string]string{settings.KeyMoviesPath: movies, settings.KeyTVPath: tv} {
		if err := server.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}

	movieFile := filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).mp4")
	writeBody(t, movieFile, "0123456789")
	writeBody(t, filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).en.srt"), "subs")
	writeBody(t, filepath.Join(movies, "The Matrix (1999)", "page.html"), "<script>alert(1)</script>")
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", movieFile); err != nil {
		t.Fatal(err)
	}
	missing, _ := server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "Not Yet"})
	outside, _ := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "Outside"})
	if err := server.MovieRepo.SetStatus(outside.ID, library.StatusDownloaded, "", secret); err != nil {
		t.Fatal(err)
	}

	ep1 := filepath.Join(tv, "Show (2011)", "Season 01", "Show - S01E01.mkv")
	writeBody(t, ep1, "episode")
	writeBody(t, filepath.Join(tv, "Show (2011)", "Season 01", "Show - S01E02.webm"), "episode 2")
	sr, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 1399, Title: "Show", Year: 2011},
		[]library.Episode{{Season: 1, Episode: 1, Title: "Pilot"}, {Season: 1, Episode: 2, Title: "Two"}})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(sr.ID)
	if err := server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "", ep1); err != nil {
		t.Fatal(err)
	}

	// Listing.
	files := getJSON[map[string]any](t, member, base+"/api/movies/"+itoa(m.ID)+"/files")
	if files["folder"] != "The Matrix (1999)" {
		t.Fatalf("folder: %+v", files)
	}
	list := files["files"].([]any)
	if len(list) != 3 {
		t.Fatalf("files: %+v", list)
	}
	video := list[1].(map[string]any) // sorted by path: .en.srt, .mp4, page.html
	if video["path"] != "The Matrix (1999).mp4" || video["kind"] != "video" || video["size"] != float64(10) ||
		video["main"] != true || video["modified"] == "" {
		t.Fatalf("movie file: %+v", video)
	}
	if strings.Contains(anyString(files), dir) {
		t.Fatalf("the listing must not reveal server paths: %+v", files)
	}
	empty := getJSON[map[string]any](t, member, base+"/api/movies/"+itoa(missing.ID)+"/files")
	if empty["folder"] != "" || len(empty["files"].([]any)) != 0 {
		t.Fatalf("a title with nothing on disk: %+v", empty)
	}
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/movies/"+itoa(outside.ID)+"/files"); status != http.StatusForbidden {
		t.Fatalf("a file outside the library folders: want 403, got %d", status)
	}

	showFiles := getJSON[map[string]any](t, member, base+"/api/series/"+itoa(sr.ID)+"/files")
	sl := showFiles["files"].([]any)
	if showFiles["folder"] != "Show (2011)" || len(sl) != 2 {
		t.Fatalf("show files: %+v", showFiles)
	}
	first := sl[0].(map[string]any)
	if first["path"] != "Season 01/Show - S01E01.mkv" || first["episodeId"] != float64(eps[0].ID) || first["main"] != true {
		t.Fatalf("episode file: %+v", first)
	}
	if second := sl[1].(map[string]any); second["episodeId"] != nil || second["main"] != nil {
		t.Fatalf("an untracked file: %+v", second)
	}

	// Streaming, with a range request as a <video> element makes.
	stream := func(query string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/api/files/stream?"+query, nil)
		req.Header.Set("Range", "bytes=2-5")
		resp, err := member.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	resp := stream("movie=" + itoa(m.ID) + "&path=" + url.QueryEscape("The Matrix (1999).mp4"))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(body) != "2345" || resp.Header.Get("Content-Type") != "video/mp4" ||
		resp.Header.Get("Content-Range") != "bytes 2-5/10" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("range stream: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	resp = stream("series=" + itoa(sr.ID) + "&path=" + url.QueryEscape("Season 01/Show - S01E02.webm"))
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "video/webm" {
		t.Fatalf("series stream: %d %v", resp.StatusCode, resp.Header)
	}
	resp = stream("series=" + itoa(sr.ID) + "&path=" + url.QueryEscape("Season 01/Show - S01E01.mkv"))
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "video/x-matroska" {
		t.Fatalf("mkv stream: %d %v", resp.StatusCode, resp.Header)
	}

	// Pictures and text files open for preview, with headers that stop the
	// browser from treating them as anything else.
	previews := []struct {
		path, ct, body string
	}{
		{"poster.jpg", "image/jpeg", "\xff\xd8\xff jpeg"},
		{"folder.png", "image/png", "\x89PNG png"},
		{"thumb.webp", "image/webp", "RIFF webp"},
		{"anim.gif", "image/gif", "GIF89a gif"},
		{"The Matrix (1999).en.srt", "text/plain; charset=utf-8", "subs"},
		{"movie.nfo", "text/plain; charset=utf-8", "<html><script>alert(1)</script></html>"},
		{"notes.txt", "text/plain; charset=utf-8", "plain notes"},
	}
	movieDir := filepath.Join(movies, "The Matrix (1999)")
	for _, p := range previews {
		if p.path != "The Matrix (1999).en.srt" {
			writeBody(t, filepath.Join(movieDir, p.path), p.body)
		}
	}
	writeBody(t, filepath.Join(movieDir, "logo.svg"), `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	writeBody(t, filepath.Join(movieDir, "app.js"), "alert(1)")
	writeBody(t, filepath.Join(movieDir, "huge.txt"), strings.Repeat("x", 2<<20+1))
	for _, p := range previews {
		req, _ := http.NewRequest(http.MethodGet, base+"/api/files/stream?movie="+itoa(m.ID)+"&path="+url.QueryEscape(p.path), nil)
		resp, err := member.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		h := resp.Header
		if resp.StatusCode != http.StatusOK || string(body) != p.body || h.Get("Content-Type") != p.ct ||
			h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != "sandbox" ||
			!strings.HasPrefix(h.Get("Content-Disposition"), "inline") {
			t.Errorf("preview %s: %d %q %v", p.path, resp.StatusCode, body, h)
		}
	}

	// Symlinks out of the folder, if this system has them.
	symlinked := os.Symlink(secret, filepath.Join(movies, "The Matrix (1999)", "escape.mp4")) == nil

	bad := []struct {
		query string
		want  int
	}{
		{"movie=" + itoa(m.ID) + "&path=" + url.QueryEscape("../../config/secret.mp4"), http.StatusBadRequest},
		{"movie=" + itoa(m.ID) + "&path=" + url.QueryEscape("../The Matrix (1999)/The Matrix (1999).mp4"), http.StatusBadRequest},
		{"movie=" + itoa(m.ID) + "&path=" + url.QueryEscape(secret), http.StatusBadRequest},
		{"movie=" + itoa(m.ID) + "&path=page.html", http.StatusUnsupportedMediaType},
		{"movie=" + itoa(m.ID) + "&path=logo.svg", http.StatusUnsupportedMediaType},
		{"movie=" + itoa(m.ID) + "&path=app.js", http.StatusUnsupportedMediaType},
		{"movie=" + itoa(m.ID) + "&path=huge.txt", http.StatusRequestEntityTooLarge},
		{"movie=" + itoa(m.ID) + "&path=nope.mp4", http.StatusNotFound},
		{"movie=" + itoa(outside.ID) + "&path=secret.mp4", http.StatusForbidden},
		{"movie=" + itoa(missing.ID) + "&path=x.mp4", http.StatusNotFound},
		{"movie=99999&path=x.mp4", http.StatusNotFound},
		{"series=" + itoa(sr.ID) + "&path=" + url.QueryEscape("../../config/secret.mp4"), http.StatusBadRequest},
		{"path=x.mp4", http.StatusBadRequest},
		{"movie=1&series=1&path=x.mp4", http.StatusBadRequest},
		{"movie=" + itoa(m.ID), http.StatusBadRequest},
	}
	if symlinked {
		bad = append(bad, struct {
			query string
			want  int
		}{"movie=" + itoa(m.ID) + "&path=escape.mp4", http.StatusBadRequest})
	}
	for _, tc := range bad {
		if status, _ := doStatus(t, member, http.MethodGet, base+"/api/files/stream?"+tc.query); status != tc.want {
			t.Errorf("stream?%s: want %d, got %d", tc.query, tc.want, status)
		}
	}
	if symlinked {
		files := getJSON[map[string]any](t, member, base+"/api/movies/"+itoa(m.ID)+"/files")
		if strings.Contains(anyString(files), "escape.mp4") {
			t.Fatalf("an escaping symlink must not be listed: %+v", files)
		}
	}

	// Signed-out requests are refused.
	resp, err = http.Get(base + "/api/files/stream?movie=" + itoa(m.ID) + "&path=" + url.QueryEscape("The Matrix (1999).mp4"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed out: want 401, got %d", resp.StatusCode)
	}
}

func anyString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
