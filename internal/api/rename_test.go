package api_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameMovesFilesToTheNamingSetting(t *testing.T) {
	server, base, client := loginNewServer(t)
	root := server.TestMoviesRoot()
	old := filepath.Join(root, "heat.1995.1080p.bluray.mkv")
	putFile(t, old, "video")
	putFile(t, filepath.Join(root, "heat.1995.1080p.bluray.en.srt"), "subs")
	id := seedMovie(t, server.MovieRepo, 949, "Heat", 1995, old)

	type item struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	plan := getJSON[[]item](t, client, base+"/api/rename?kind=movie")
	if len(plan) != 1 || plan[0].From != old || !strings.Contains(plan[0].To, "Heat (1995)") {
		t.Fatalf("preview: %+v", plan)
	}
	res := postJSON[struct {
		Renamed int      `json:"renamed"`
		Failed  []string `json:"failed"`
	}](t, client, base+"/api/rename", map[string]any{"items": []map[string]any{{"kind": "movie", "id": id}}}, http.StatusOK)
	if res.Renamed != 1 || len(res.Failed) != 0 {
		t.Fatalf("rename: %+v", res)
	}
	if exists(old) || !exists(plan[0].To) {
		t.Fatalf("file not moved: old %v new %v", exists(old), exists(plan[0].To))
	}
	srt := strings.TrimSuffix(plan[0].To, filepath.Ext(plan[0].To)) + ".en.srt"
	if !exists(srt) {
		t.Errorf("the subtitle should move with the video to %s", srt)
	}
	if m, _ := server.MovieRepo.Get(id); m.FilePath != plan[0].To {
		t.Errorf("library still points at %s", m.FilePath)
	}
	if again := getJSON[[]item](t, client, base+"/api/rename?kind=movie"); len(again) != 0 {
		t.Errorf("nothing should be left to rename: %+v", again)
	}
}
