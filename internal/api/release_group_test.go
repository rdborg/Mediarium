package api_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetMovieReleaseGroupRenamesTheFile(t *testing.T) {
	server, base, client := loginNewServer(t)
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "movieNameFormat": "{Movie CleanTitle} ({Release Year}){-Release Group}"}, http.StatusOK)
	root := server.TestMoviesRoot()
	old := filepath.Join(root, "Heat.1995.1080p.BluRay.x264-OLD.mkv")
	putFile(t, old, "video")
	putFile(t, filepath.Join(root, "Heat.1995.1080p.BluRay.x264-OLD.en.srt"), "subs")
	id := seedMovie(t, server.MovieRepo, 949, "Heat", 1995, old)

	url := base + "/api/movies/" + fmt.Sprint(id) + "/release-group"
	bad := putJSONStatus(t, client, url, map[string]any{"group": "no/slash"}, http.StatusBadRequest)
	if msg, _ := bad["error"].(string); !strings.Contains(msg, "release group") {
		t.Fatalf("bad group = %v", bad)
	}

	got := putJSONStatus(t, client, url, map[string]any{"group": "NEW"}, http.StatusOK)
	m, _ := server.MovieRepo.Get(id)
	if got["renamed"] != true || !strings.HasSuffix(m.FilePath, "Heat (1995)-NEW.mkv") {
		t.Fatalf("answer %v, file %s", got, m.FilePath)
	}
	if exists(old) || !exists(m.FilePath) {
		t.Fatalf("file not moved: old %v new %v", exists(old), exists(m.FilePath))
	}
	if !exists(strings.TrimSuffix(m.FilePath, ".mkv") + ".en.srt") {
		t.Error("the subtitle should follow the video")
	}

	// Emptying it removes the group from the name.
	putJSONStatus(t, client, url, map[string]any{"group": ""}, http.StatusOK)
	m, _ = server.MovieRepo.Get(id)
	if !strings.HasSuffix(m.FilePath, "Heat (1995).mkv") {
		t.Fatalf("after removing: %s", m.FilePath)
	}
}
