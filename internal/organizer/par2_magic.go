package organizer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// namePar2ByContent gives a ".par2" ending to PAR2 files that were posted with
// a random name and no extension. A PAR2 file starts with the bytes "PAR2",
// 0, "PKT", so it can be told by what is inside it. Without the ending the
// real names in it were never read and par2 was never run on the release.
func namePar2ByContent(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasSuffix(strings.ToLower(e.Name()), ".par2") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		head := make([]byte, len(par2Magic))
		_, err = io.ReadFull(f, head)
		f.Close()
		if err != nil || !bytes.Equal(head, par2Magic) {
			continue
		}
		target := path + ".par2"
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		_ = os.Rename(path, target)
	}
}
