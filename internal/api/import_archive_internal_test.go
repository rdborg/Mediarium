package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/mediainfo/mediainfotest"
)

// A download whose "video" is an archive that was never unpacked is turned
// away as a bad release, with its own blocklist reason; a real video and a
// file that can't be read are not.
func TestImportTurnsAwayArchives(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rar := write("Show.S01E01.mkv", append([]byte("Rar!\x1a\x07\x01\x00"), make([]byte, 1024)...))
	video := write("Show.S01E02.mkv", mediainfotest.MKV(20*60))
	other := write("Show.S01E03.avi", []byte("not a container this reads, but not an archive either"))

	err := notAVideo(rar)
	if err == nil {
		t.Fatal("an archive named .mkv was let through")
	}
	if !strings.Contains(err.Error(), "RAR archive, not a video") {
		t.Errorf("message = %q", err.Error())
	}
	if got := blocklistReason(err); got != notVideoReason {
		t.Errorf("blocklist reason = %q, want %q", got, notVideoReason)
	}
	for _, p := range []string{video, other, filepath.Join(dir, "missing.mkv")} {
		if err := notAVideo(p); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
}
