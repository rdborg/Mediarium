package ytdlp

import (
	"strings"
	"testing"
)

// FuzzParseListing reads yt-dlp output that may be cut off or garbled.
func FuzzParseListing(f *testing.F) {
	f.Add(`{"id":"abcdefghijk","title":"x","duration":12.5}` + "\n" + `{"id":`)
	f.Add("not json\n{}\n[]\n")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseListing(strings.NewReader(s))
	})
}
