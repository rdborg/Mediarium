package subtitles

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	tests := []struct {
		name  string
		size  int
		first uint64 // first word of the file
		last  uint64 // last word of the file
		want  string
	}{
		{name: "too small", size: 2*hashChunk - 1, want: ""},
		{name: "zeros", size: 2 * hashChunk, want: "0000000000020000"},
		{name: "first and last words", size: 3 * hashChunk, first: 1, last: 2, want: "0000000000030003"},
		{name: "the sum wraps around", size: 2 * hashChunk, first: ^uint64(0), last: 0, want: "000000000001ffff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.size)
			if tt.size >= 16 {
				binary.LittleEndian.PutUint64(data, tt.first)
				binary.LittleEndian.PutUint64(data[tt.size-8:], tt.last)
			}
			path := filepath.Join(t.TempDir(), "video.mkv")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := FileHash(path)
			if err != nil || got != tt.want {
				t.Fatalf("FileHash = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	if _, err := FileHash(filepath.Join(t.TempDir(), "missing.mkv")); err == nil {
		t.Fatal("a missing file should be an error")
	}
}
