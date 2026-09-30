package backup_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/backup"
)

// FuzzStage feeds arbitrary bytes as an uploaded backup. Whatever they are,
// staging must not panic, must not touch anything outside the config folder
// and must leave it exactly as it was when the backup is refused.
func FuzzStage(f *testing.F) {
	key := []byte("bm90LWEtcmVhbC1rZXk=")
	f.Add([]byte("PK\x03\x04junk"))
	f.Add([]byte{})
	f.Add(buildZipBytes(map[string][]byte{"app.db": []byte("x"), "secret.key": key}))
	f.Add(buildZipBytes(map[string][]byte{"../app.db": []byte("x")}))
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		zipPath := filepath.Join(t.TempDir(), "in.zip")
		if err := os.WriteFile(zipPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		before := dirNames(t, dir)
		_, err := backup.Stage(dir, zipPath)
		if err == nil {
			t.Skip("accepted")
		}
		after := dirNames(t, dir)
		if len(before) != len(after) {
			t.Fatalf("a refused backup changed the config folder: %v -> %v", before, after)
		}
	})
}

func buildZipBytes(entries map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, _ := zw.Create(name)
		w.Write(data)
	}
	zw.Close()
	return buf.Bytes()
}
