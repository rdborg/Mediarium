package ebookconv

import (
	"io"
	"os"
	"testing"
	"time"
)

// FuzzConvert feeds mangled books to the converter: it may refuse them, but
// it must never hang or crash.
func FuzzConvert(f *testing.F) {
	for _, name := range []string{"testdata/test.mobi", "testdata/test.azw3", "testdata/test-both.mobi"} {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		start := time.Now()
		_, _ = Convert(data, io.Discard)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("a %d-byte input took %v", len(data), d)
		}
	})
}
