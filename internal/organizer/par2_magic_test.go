package organizer

import (
	"os"
	"path/filepath"
	"testing"
)

// A PAR2 file with a random name and no ending is found by its first bytes and
// named like one; other files are left alone.
func TestDeobfuscateNamesPar2FilesByContent(t *testing.T) {
	dir := t.TempDir()
	par2 := append(append([]byte{}, par2Magic...), make([]byte, 100)...)
	if err := os.WriteFile(filepath.Join(dir, "x9Qk2LmPz"), par2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("not a par2 file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tiny"), []byte("PA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Deobfuscate(dir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"x9Qk2LmPz.par2": true, "x9Qk2LmPz": false, "movie.mkv": true, "movie.mkv.par2": false, "tiny": true, "tiny.par2": false} {
		_, err := os.Stat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", name, err == nil, want)
		}
	}
}
