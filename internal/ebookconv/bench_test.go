package ebookconv

import (
	"io"
	"os"
	"testing"
)

func BenchmarkConvert(b *testing.B) {
	for _, name := range []string{"testdata/test.mobi", "testdata/test.azw3"} {
		data, _ := os.ReadFile(name)
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				_, _ = Convert(data, io.Discard)
			}
		})
	}
}
