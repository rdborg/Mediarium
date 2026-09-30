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

// Any account may ask for a subtitle, so the refusal for a video that has gone
// from the disk does not name the folder it was in on the server.
func TestSubtitleDownloadDoesNotRevealServerFolders(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitlesEnabled": true}, http.StatusOK)
	dir := t.TempDir()
	video := filepath.Join(dir, "Gone Movie (2001).mkv")
	if err := os.WriteFile(video, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 11, Title: "Gone Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(video); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/movies/%d/subtitles/download", env.baseURL, m.ID)
	got := postJSONMethod[map[string]any](t, env.client, http.MethodPost, url, map[string]any{"fileId": 5, "language": "en"}, http.StatusPreconditionFailed)
	msg, _ := got["error"].(string)
	if !strings.Contains(msg, "missing") || strings.Contains(msg, dir) || strings.Contains(msg, "Gone Movie (2001).mkv") {
		t.Errorf("message = %q; want a plain sentence with no folder in it", msg)
	}
}
