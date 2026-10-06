package subtitles

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// hashChunk is how much of each end of a video the OpenSubtitles hash reads.
const hashChunk = 64 * 1024

// FileHash is the OpenSubtitles hash of a video file: its size plus the
// 64-bit little-endian words of its first and last 64 KB. Subtitles that
// were timed against this exact file carry the same hash, so a match means
// the subtitle is in sync. Files under 128 KB have no hash ("").
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash video: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("hash video: %w", err)
	}
	size := info.Size()
	if size < 2*hashChunk {
		return "", nil
	}
	sum := uint64(size)
	buf := make([]byte, hashChunk)
	for _, at := range []int64{0, size - hashChunk} {
		if _, err := f.ReadAt(buf, at); err != nil && err != io.EOF {
			return "", fmt.Errorf("hash video: %w", err)
		}
		for i := 0; i < hashChunk; i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", sum), nil
}
