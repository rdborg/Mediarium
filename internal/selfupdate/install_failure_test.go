package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// When writing the version or checksum file fails after the new program is in
// place, the installed update is put back exactly as it was.
func TestInstallFailureLeavesTheFolderAsItWas(t *testing.T) {
	sumOld := strings.Repeat("a", 64)
	sumNew := strings.Repeat("b", 64)

	for _, tc := range []struct {
		name     string
		blockTmp string // a folder with this name makes the write of that file fail
		first    bool   // no program installed before
	}{
		{"version write fails on an update", FileVersion + ".tmp", false},
		{"checksum write fails on an update", FileSum + ".tmp", false},
		{"version write fails on the first install", FileVersion + ".tmp", true},
		{"checksum write fails on the first install", FileSum + ".tmp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tc.first {
				write(t, filepath.Join(dir, "up0"), "program old")
				if err := Install(dir, filepath.Join(dir, "up0"), "1.2.0", sumOld); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(dir, tc.blockTmp), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, "up1"), "program new")
			if err := Install(dir, filepath.Join(dir, "up1"), "1.3.0", sumNew); err == nil {
				t.Fatal("Install should fail")
			}

			if tc.first {
				for _, name := range []string{FileApp, FileVersion, FileSum, FilePrevious} {
					if _, err := os.Stat(filepath.Join(dir, name)); err == nil && name != FileSum {
						t.Errorf("%s should not exist after a failed first install", name)
					}
				}
				return
			}
			if got := read(t, filepath.Join(dir, FileApp)); got != "program old" {
				t.Errorf("installed program = %q, want the old one", got)
			}
			if got := strings.TrimSpace(read(t, filepath.Join(dir, FileVersion))); got != "1.2.0" {
				t.Errorf("version = %q, want 1.2.0", got)
			}
			if got := read(t, filepath.Join(dir, FileSum)); !strings.HasPrefix(got, sumOld) {
				t.Errorf("checksum file = %q, want the old checksum", got)
			}
		})
	}
}
