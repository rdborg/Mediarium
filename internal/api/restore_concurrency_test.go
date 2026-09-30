package api_test

import (
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".restore-upload-") || strings.Contains(n, ".tmp-") || strings.HasPrefix(n, ".restore-validate-") || strings.HasPrefix(n, ".backup-snapshot-") {
			out = append(out, n)
		}
	}
	return out
}

// While one restore upload is still arriving a second one is turned away, and
// an upload that is cut off halfway leaves nothing behind in the config folder.
func TestRestoreOneAtATimeAndACutUploadLeavesNothing(t *testing.T) {
	_, baseA, clientA := loginNewServer(t)
	goodZip := downloadBackup(t, clientA, baseA)

	serverB, baseB, clientB := loginNewServer(t)
	called := exitSpy(serverB)
	configDir := getJSON[map[string]any](t, clientB, baseB+"/api/system/info")["configDir"].(string)

	// The first upload sends half of the file and waits.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	firstDone := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, baseB+"/api/system/restore", pr)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := clientB.Do(req)
		if err != nil {
			firstDone <- -1
			return
		}
		resp.Body.Close()
		firstDone <- resp.StatusCode
	}()
	part, _ := mw.CreateFormFile("file", "backup.zip")
	if _, err := part.Write(goodZip[:len(goodZip)/2]); err != nil {
		t.Fatal(err)
	}

	// The second is refused while the first holds the restore.
	deadline := time.Now().Add(3 * time.Second)
	var code int
	for time.Now().Before(deadline) {
		code, _ = postRestore(t, clientB, baseB, "file", []byte("not a backup"))
		if code == http.StatusConflict {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code != http.StatusConflict {
		t.Fatalf("a second restore while one is arriving: status %d, want 409", code)
	}

	// Cut the first one off.
	pw.CloseWithError(io.ErrUnexpectedEOF)
	select {
	case got := <-firstDone:
		if got == http.StatusOK {
			t.Fatal("a cut-off upload must not be accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cut-off upload never finished")
	}
	neverExited(t, called)
	if left := leftovers(t, configDir); len(left) != 0 {
		t.Errorf("leftovers in the config folder: %v", left)
	}
	if _, err := os.Stat(filepath.Join(configDir, "restore-pending")); err == nil {
		t.Error("nothing should be staged")
	}

	// The lock is free again: a whole upload now works.
	if code, out := postRestore(t, clientB, baseB, "file", goodZip); code != http.StatusOK {
		t.Fatalf("restore after the failed one: %d %+v", code, out)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("the app should restart after a successful restore")
	}
}
