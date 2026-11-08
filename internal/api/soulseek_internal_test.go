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

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/slskd"
)

// A search answer with two users: alice has the whole album in FLAC with a
// free slot, bob has two tracks in MP3 and a queue.
const soulseekAnswer = `[
 {"username":"bob","hasFreeUploadSlot":false,"queueLength":12,"uploadSpeed":100000,"files":[
  {"filename":"music\\Low Roar\\0\\01 Breathe In.mp3","size":9000000,"bitRate":320},
  {"filename":"music\\Low Roar\\0\\02 Easy Way Out.mp3","size":9100000,"bitRate":320}]},
 {"username":"alice","hasFreeUploadSlot":true,"queueLength":0,"uploadSpeed":2000000,"files":[
  {"filename":"Share\\Low Roar\\Low Roar - 0 (2014)\\01 Breathe In.flac","size":30000000,"bitDepth":16},
  {"filename":"Share\\Low Roar\\Low Roar - 0 (2014)\\02 Easy Way Out.flac","size":31000000,"bitDepth":16},
  {"filename":"Share\\Low Roar\\Low Roar - 0 (2014)\\03 Bones.flac","size":32000000,"bitDepth":16},
  {"filename":"Share\\Low Roar\\Low Roar - 0 (2014)\\cover.jpg","size":100000}]}
]`

type fakeSlskd struct {
	mu     sync.Mutex
	queued []slskd.Want
}

func (f *fakeSlskd) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/api/v0")
	switch {
	case p == "/application/version":
		_, _ = w.Write([]byte(`"0.22.5"`))
	case p == "/searches" && r.Method == http.MethodPost:
		_, _ = w.Write([]byte(`{"id":"s","isComplete":true}`))
	case strings.HasSuffix(p, "/responses"):
		_, _ = w.Write([]byte(soulseekAnswer))
	case strings.HasPrefix(p, "/searches/"):
		_, _ = w.Write([]byte(`{"id":"s","isComplete":true}`))
	case p == "/transfers/downloads/batches":
		var b struct {
			Files []slskd.Want `json:"files"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.queued = append(f.queued, b.Files...)
		w.WriteHeader(http.StatusCreated)
	case p == "/transfers/downloads/alice":
		var files []map[string]any
		for _, q := range f.queued {
			files = append(files, map[string]any{"id": q.Filename, "filename": q.Filename, "size": q.Size, "state": "Completed, Succeeded", "bytesTransferred": q.Size})
		}
		b, _ := json.Marshal(map[string]any{"username": "alice", "directories": []any{map[string]any{"directory": "Low Roar - 0 (2014)", "files": files}}})
		_, _ = w.Write(b)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestSoulseekAlbum(t *testing.T) {
	s := newBareServer(t)
	f := &fakeSlskd{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	folder := t.TempDir()
	for k, v := range map[string]string{settings.KeyModulesMusic: "1", settings.KeySoulseekURL: srv.URL, settings.KeySoulseekFolder: folder} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Settings.Set(settings.KeySoulseekAPIKey, "key", true); err != nil {
		t.Fatal(err)
	}
	artist := music.Artist{Name: "Low Roar"}
	album := music.Album{ID: 0, Title: "0", ReleaseDate: "2014-11-04"}

	got, err := s.soulseekAlbumReleases(context.Background(), artist, album, music.Profile{})
	if err != nil || len(got) != 2 {
		t.Fatalf("releases = %+v, %v", got, err)
	}
	if got[0].IndexerName != "Soulseek · alice" || got[0].Protocol != "soulseek" || !strings.Contains(got[0].Title, "[FLAC]") || got[0].SizeBytes != 93000000 {
		t.Errorf("first = %+v", got[0])
	}
	if !strings.Contains(got[1].Title, "Low Roar - 0 [MP3 320]") {
		t.Errorf("a folder named only \"0\" gets its parent's name: %q", got[1].Title)
	}
	if !strings.Contains(strings.Join(got[1].Rejections, " "), "upload slots are busy") {
		t.Errorf("bob's busy slots aren't said: %v", got[1].Rejections)
	}

	// The download: slskd is asked for alice's audio files, which then sit in
	// its folder as slskd saves them; they are linked into the download's folder.
	grab, err := decodeSoulseek(s.offered.resolve(got[0].DownloadURL))
	if err != nil || grab.User != "alice" || len(grab.Files) != 3 {
		t.Fatalf("grab = %+v, %v", grab, err)
	}
	saved := filepath.Join(folder, "Low Roar - 0 (2014)")
	if err := os.MkdirAll(saved, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, w := range grab.Files {
		if err := os.WriteFile(filepath.Join(saved, slskd.BaseName(w.Filename)), make([]byte, w.Size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: got[0].Title, NZBURL: encodeSoulseek(grab), Protocol: queue.ProtocolSoulseek})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := s.downloadSoulseek(context.Background(), id, encodeSoulseek(grab))
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "privilege") {
		t.Skip("symlinks need extra rights on Windows")
	}
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(f.queued) != 3 {
		t.Errorf("slskd was asked for %d files", len(f.queued))
	}
	files, err := music.FindAudioFiles(dir)
	if err != nil || len(files) != 3 || !isClientView(dir) {
		t.Fatalf("view = %v, %v (client view %v)", files, err, isClientView(dir))
	}
	target, err := s.clientFileBehind(files[0])
	if err != nil || !strings.HasPrefix(target, folder) {
		t.Errorf("a link leads to %q, %v", target, err)
	}

	// Asked again (Resume), the files already queued aren't asked for twice.
	if _, err := s.downloadSoulseek(context.Background(), id, encodeSoulseek(grab)); err != nil || len(f.queued) != 3 {
		t.Errorf("again: %v, %d queued", err, len(f.queued))
	}
}
