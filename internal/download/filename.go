package download

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var quotedFilenameRe = regexp.MustCompile(`"([^"]+)"`)

// articleFilename recovers the real filename from a Usenet post subject,
// which conventionally quotes it, e.g. `[1/50] "Movie.2024.mkv" yEnc (1/500)`.
// Falls back to a generic name derived from the file's position when the
// subject doesn't follow that convention.
func articleFilename(subject string, fileIndex int) string {
	if m := quotedFilenameRe.FindStringSubmatch(subject); m != nil {
		return m[1]
	}
	return fmt.Sprintf("file_%d.bin", fileIndex)
}

var illegalFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// sanitizeFilename strips characters that are illegal in a filename on the
// host OS (PRD.md §4.8 — illegal character handling), independent of the
// user-facing library naming/token engine in internal/organizer.
func sanitizeFilename(name string) string {
	cleaned := illegalFilenameChars.ReplaceAllString(name, "_")
	return strings.TrimSpace(cleaned)
}

func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}
