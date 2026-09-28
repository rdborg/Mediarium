package download

import (
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// buildMultipartYenc constructs a multi-part yEnc article body covering
// byte range [begin, end] (1-based, inclusive) of a file whose full
// content is fullData — mirroring what a real Usenet poster's client
// would produce when splitting a file across articles.
func buildMultipartYenc(name string, fullData []byte, begin, end int) []byte {
	partData := fullData[begin-1 : end]
	header := fmt.Sprintf("=ybegin part=1 line=128 size=%d name=%s\r\n=ypart begin=%d end=%d\r\n", len(fullData), name, begin, end)
	footer := fmt.Sprintf("=yend size=%d pcrc32=%08x\r\n", len(partData), crc32.ChecksumIEEE(partData))
	return []byte(header + encodeBodyOnly(partData) + footer)
}

func TestDownloadAssemblesMultiSegmentFile(t *testing.T) {
	fullData := []byte("The quick brown fox jumps over the lazy dog. This is fixture movie content.")
	mid := len(fullData) / 2

	seg1 := buildMultipartYenc("movie.mkv", fullData, 1, mid)
	seg2 := buildMultipartYenc("movie.mkv", fullData, mid+1, len(fullData))

	srv := newFakeNNTPServer(t, map[string][]byte{
		"seg1@example": seg1,
		"seg2@example": seg2,
	})

	nzb := &NZB{
		Files: []NZBFile{
			{
				Subject: `[1/1] "movie.mkv" yEnc (1/2)`,
				Groups:  []string{"alt.binaries.test"},
				Segments: []NZBSegment{
					{Number: 1, Bytes: int64(mid), MessageID: "seg1@example"},
					{Number: 2, Bytes: int64(len(fullData) - mid), MessageID: "seg2@example"},
				},
			},
		},
	}

	destDir := t.TempDir()
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, UseSSL: false, Username: "user", Password: "pass", Connections: 2}

	var lastDone, lastTotal int64
	paths, err := Download(context.Background(), cfg, nzb, destDir, func(done, total int64) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 output file, got %d", len(paths))
	}
	if filepath.Base(paths[0]) != "movie.mkv" {
		t.Fatalf("unexpected output filename: %s", paths[0])
	}

	got, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read assembled file: %v", err)
	}
	if string(got) != string(fullData) {
		t.Fatalf("assembled file mismatch:\nwant %q\ngot  %q", fullData, got)
	}
	if lastTotal != nzb.TotalBytes() {
		t.Fatalf("expected progress total %d, got %d", nzb.TotalBytes(), lastTotal)
	}
	if lastDone != lastTotal {
		t.Fatalf("expected progress done==total at completion, got done=%d total=%d", lastDone, lastTotal)
	}
}

func TestDownloadFailsOnMissingSegment(t *testing.T) {
	srv := newFakeNNTPServer(t, map[string][]byte{}) // no bodies registered
	nzb := &NZB{
		Files: []NZBFile{
			{
				Subject:  `[1/1] "movie.mkv" yEnc (1/1)`,
				Groups:   []string{"alt.binaries.test"},
				Segments: []NZBSegment{{Number: 1, Bytes: 100, MessageID: "missing@example"}},
			},
		},
	}
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Connections: 1}
	_, err := Download(context.Background(), cfg, nzb, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error when a segment can't be fetched")
	}
}
