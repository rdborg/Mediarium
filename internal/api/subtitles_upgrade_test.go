package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// newMatchingOpenSubtitles serves one English subtitle picked by name (file
// 7) and, once matchReady is set and the search carries the video's hash,
// one made for that exact file (file 9). Each file's text says its id.
func newMatchingOpenSubtitles(t *testing.T, matchReady *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/subtitles", func(w http.ResponseWriter, r *http.Request) {
		data := []map[string]any{
			{"attributes": map[string]any{"language": "en", "release": "Some.Movie.2001.1080p.BluRay.x264-GRP", "download_count": 100, "files": []map[string]any{{"file_id": 7}}}},
		}
		if matchReady.Load() && r.URL.Query().Get("moviehash") != "" {
			data = append(data, map[string]any{"attributes": map[string]any{"language": "en", "release": "other", "moviehash_match": true, "files": []map[string]any{{"file_id": 9}}}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FileID int `json:"file_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(map[string]any{"link": fmt.Sprintf("http://%s/file/%d", r.Host, req.FileID)})
	})
	mux.HandleFunc("/file/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "1\n00:00:01,000 --> 00:00:02,000\nfile %s\n", r.PathValue("id"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSubtitleUpgrades(t *testing.T) {
	tests := []struct {
		name     string
		byHand   bool                           // the first subtitle was picked by hand
		meddle   func(t *testing.T, sub string) // what happens to it before the upgrade looks
		wantFile string
	}{
		{name: "swapped when one made for the file turns up", wantFile: "file 9"},
		{name: "left alone when picked by hand", byHand: true, wantFile: "file 7"},
		{
			name: "left alone after it was edited",
			meddle: func(t *testing.T, sub string) {
				if err := os.WriteFile(sub, []byte("1\n00:00:02,000 --> 00:00:03,000\nfile 7, moved\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantFile: "file 7, moved",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTVAutoEnv(t, nil, nil)
			var ready atomic.Bool
			srv := newMatchingOpenSubtitles(t, &ready)
			env.server.TestSetSubtitlesBaseURL("fixture-key", srv.URL)
			putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleLanguages": []string{"en"}, "subtitleAutoDownload": true}, http.StatusOK)

			dir := t.TempDir()
			video := filepath.Join(dir, "Some Movie (2001).mkv")
			if err := os.WriteFile(video, make([]byte, 200*1024), 0o644); err != nil {
				t.Fatal(err)
			}
			m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
			if err != nil {
				t.Fatal(err)
			}
			_ = env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video)
			sub := filepath.Join(dir, "Some Movie (2001).en.srt")

			if tt.byHand {
				postJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/subtitles/download", env.baseURL, m.ID), map[string]any{"fileId": 7, "language": "en"}, http.StatusOK)
			} else {
				env.server.TestSubtitleSweepJob(context.Background())
			}
			if b, _ := os.ReadFile(sub); !strings.Contains(string(b), "file 7") {
				t.Fatalf("first subtitle: %q", b)
			}
			if tt.meddle != nil {
				tt.meddle(t, sub)
			}

			// One made for the file turns up a few days later.
			ready.Store(true)
			env.server.TestAgeSubtitleFiles(4 * 24 * time.Hour)
			env.server.TestSubtitleSweepJob(context.Background())
			if b, _ := os.ReadFile(sub); !strings.Contains(string(b), tt.wantFile) {
				t.Fatalf("subtitle now %q, want %q", b, tt.wantFile)
			}
		})
	}
}

func TestSubtitleUpgradesWaitAndStop(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	var ready atomic.Bool
	srv := newMatchingOpenSubtitles(t, &ready)
	env.server.TestSetSubtitlesBaseURL("fixture-key", srv.URL)
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleLanguages": []string{"en"}, "subtitleAutoDownload": true}, http.StatusOK)
	dir := t.TempDir()
	video := filepath.Join(dir, "Some Movie (2001).mkv")
	_ = os.WriteFile(video, make([]byte, 200*1024), 0o644)
	m, _ := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
	_ = env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video)
	sub := filepath.Join(dir, "Some Movie (2001).en.srt")
	env.server.TestSubtitleSweepJob(context.Background())
	ready.Store(true)

	steps := []struct {
		name     string
		age      time.Duration
		upgrade  bool // the "look for better ones" switch
		wantFile string
	}{
		{name: "not looked at again within three days", age: 24 * time.Hour, upgrade: true, wantFile: "file 7"},
		{name: "not while switched off", age: 4 * 24 * time.Hour, upgrade: false, wantFile: "file 7"},
		{name: "not after a month", age: 31 * 24 * time.Hour, upgrade: true, wantFile: "file 7"},
	}
	for _, st := range steps {
		putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleUpgrade": st.upgrade}, http.StatusOK)
		env.server.TestAgeSubtitleFiles(st.age)
		env.server.TestSubtitleSweepJob(context.Background())
		if b, _ := os.ReadFile(sub); !strings.Contains(string(b), st.wantFile) {
			t.Fatalf("%s: subtitle now %q", st.name, b)
		}
	}
}
