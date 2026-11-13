package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/slskd"
)

func TestSoulseekTriesPrefersTheBestTry(t *testing.T) {
	wanted := map[string]int64{"a": 1, "b": 1}
	var f, ok, going slskd.State
	if err := json.Unmarshal([]byte(`"Completed, Errored"`), &f); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`"Completed, Succeeded"`), &ok); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`"InProgress"`), &going); err != nil {
		t.Fatal(err)
	}
	got := soulseekTries([]slskd.Transfer{
		{Filename: "a", State: f}, {Filename: "a", State: ok}, {Filename: "a", State: f},
		{Filename: "b", State: f}, {Filename: "b", State: going},
		{Filename: "other", State: ok},
	}, wanted)
	if len(got) != 2 || !got["a"].State.Succeeded() || got["b"].State.Done() {
		t.Errorf("tries = %+v", got)
	}
}

// A song that fails is asked for again from the same user, and the release
// goes on instead of being given up on.
func TestSoulseekDownloadAsksAgainForAFailedSong(t *testing.T) {
	old := soulseekPoll
	soulseekPoll = 20 * time.Millisecond
	defer func() { soulseekPoll = old }()

	var mu sync.Mutex
	asked := map[string]int{}
	files := []slskd.Want{{Filename: `Share\Album\01 One.flac`, Size: 10}, {Filename: `Share\Album\02 Two.flac`, Size: 10}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/api/v0")
		switch {
		case p == "/transfers/downloads/batches":
			var b struct {
				Files []slskd.Want `json:"files"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			for _, f := range b.Files {
				asked[f.Filename]++
			}
			w.WriteHeader(http.StatusCreated)
		case p == "/transfers/downloads/carol":
			var list []map[string]any
			for _, f := range files {
				n := asked[f.Filename]
				if n == 0 {
					continue
				}
				state := "Completed, Succeeded"
				if f.Filename == files[1].Filename && n == 1 {
					state = "Completed, Errored" // the first try of the second song fails
				}
				list = append(list, map[string]any{"id": f.Filename, "filename": f.Filename, "size": f.Size, "state": state, "bytesTransferred": f.Size})
			}
			b, _ := json.Marshal(map[string]any{"username": "carol", "directories": []any{map[string]any{"directory": "Album", "files": list}}})
			_, _ = w.Write(b)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := newBareServer(t)
	folder := t.TempDir()
	for k, v := range map[string]string{settings.KeyModulesMusic: "1", settings.KeySoulseekURL: srv.URL, settings.KeySoulseekFolder: folder} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Settings.Set(settings.KeySoulseekAPIKey, "key", true); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(folder, "Album")
	if err := os.MkdirAll(saved, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(saved, slskd.BaseName(f.Filename)), make([]byte, f.Size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	grab := soulseekGrab{User: "carol", Files: files}
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Album", NZBURL: encodeSoulseek(grab), Protocol: queue.ProtocolSoulseek})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = s.downloadSoulseek(ctx, id, encodeSoulseek(grab))
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "privilege") {
		t.Skip("symlinks need extra rights on Windows")
	}
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if asked[files[0].Filename] != 1 || asked[files[1].Filename] != 2 {
		t.Errorf("asked %v: the first song once, the failed one twice", asked)
	}
}
