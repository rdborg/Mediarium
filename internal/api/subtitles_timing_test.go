package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func cueFile(offsetMs int) string {
	var b strings.Builder
	at := 5000
	for i := 0; i < 200; i++ {
		s := at + offsetMs
		fmt.Fprintf(&b, "%d\n%02d:%02d:%02d,%03d --> %02d:%02d:%02d,%03d\nLine %d\n\n", i+1, s/3600000, s/60000%60, s/1000%60, s%1000, (s+1500)/3600000, (s+1500)/60000%60, (s+1500)/1000%60, (s+1500)%1000, i+1)
		at += 1800 + (i*7919)%4000
	}
	return b.String()
}

func TestSubtitleTiming(t *testing.T) {
	server, base, client := loginNewServer(t)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(server.TestMoviesRoot(), "The Matrix (1999)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "The Matrix (1999).mkv")
	files := map[string]string{
		video: "video",
		filepath.Join(dir, "The Matrix (1999).en.srt"): cueFile(0),
		filepath.Join(dir, "The Matrix (1999).fr.srt"): cueFile(3200), // 3.2 seconds late
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video); err != nil {
		t.Fatal(err)
	}
	call := func(body map[string]any, want int) map[string]any {
		t.Helper()
		body["kind"], body["id"] = "movie", m.ID
		return postJSON[map[string]any](t, client, base+"/api/subtitles/timing", body, want)
	}
	fr := filepath.Join(dir, "The Matrix (1999).fr.srt")

	call(map[string]any{"file": "../../etc/passwd.srt", "shiftMs": 1000}, http.StatusBadRequest)
	call(map[string]any{"file": "The Matrix (1999).mkv", "shiftMs": 1000}, http.StatusBadRequest)
	call(map[string]any{"file": "The Matrix (1999).fr.srt"}, http.StatusBadRequest)

	// Line the French one up with the English one.
	res := call(map[string]any{"file": "The Matrix (1999).fr.srt", "reference": "The Matrix (1999).en.srt"}, http.StatusOK)
	if res["offsetMs"].(float64) > -3150 || res["offsetMs"].(float64) < -3250 || res["matched"].(float64) < 0.95 {
		t.Fatalf("align: %v", res)
	}
	got, _ := os.ReadFile(fr)
	if !strings.HasPrefix(string(got), "1\n00:00:05,0") {
		t.Fatalf("first cue after lining up: %q", string(got)[:40])
	}
	if _, err := os.Stat(fr + ".bak"); err != nil {
		t.Fatal("the original wasn't kept")
	}

	// Shift by hand, then put the original back.
	call(map[string]any{"file": "The Matrix (1999).fr.srt", "shiftMs": -500}, http.StatusOK)
	call(map[string]any{"file": "The Matrix (1999).fr.srt", "undo": true}, http.StatusOK)
	got, _ = os.ReadFile(fr)
	if string(got) != cueFile(3200) {
		t.Fatal("undo didn't bring the original back")
	}
	call(map[string]any{"file": "The Matrix (1999).en.srt", "undo": true}, http.StatusNotFound)
}
