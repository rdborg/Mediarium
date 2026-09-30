package download

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
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
// host OS (illegal character handling), independent of the
// user-facing library naming/token engine in internal/organizer.
func sanitizeFilename(name string) string {
	cleaned := illegalFilenameChars.ReplaceAllString(strings.ToValidUTF8(name, "_"), "_")
	// Delete and the marks that reorder text ("evil‮gpj.exe" reads as
	// "evilexe.jpg") have no place in a file name.
	cleaned = strings.Map(func(r rune) rune {
		if r == 0x7f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return '_'
		}
		return r
	}, cleaned)
	return strings.TrimSpace(cleaned)
}

// maxFileNameBytes is the longest name given to a downloaded file. Most
// filesystems refuse anything over 255 bytes; this leaves room for a
// ".2" style suffix and the temporary names other steps add.
const maxFileNameBytes = 200

// outputFileNames chooses the name each file of an NZB is saved under in the
// download folder. The names come from the posts' subjects, which anyone can
// write, so they are made safe here: a name that is empty or only dots (".."
// would be the folder above) gets a plain one, a name that would collide with
// the download's own bookkeeping files is replaced, a name too long for the
// filesystem is shortened, and two files that ask for the same name get
// different ones, so they never write into each other.
func outputFileNames(files []NZBFile) []string {
	names := make([]string, len(files))
	used := make(map[string]bool, len(files))
	for i, f := range files {
		name := sanitizeFilename(articleFilename(f.Subject, i))
		fallback := fmt.Sprintf("file_%d.bin", i)
		if strings.Trim(name, ". ") == "" || strings.HasPrefix(strings.ToLower(name), ".mediarium-") {
			name = fallback
		}
		name = shortenFileName(name, maxFileNameBytes)
		if strings.Trim(name, ". ") == "" {
			name = fallback
		}
		unique := name
		for n := 2; used[strings.ToLower(unique)]; n++ {
			unique = withSuffix(name, fmt.Sprintf(" (%d)", n))
		}
		used[strings.ToLower(unique)] = true
		names[i] = unique
	}
	return names
}

// shortenFileName cuts name to at most max bytes without splitting a
// character, keeping its extension.
func shortenFileName(name string, max int) string {
	if len(name) <= max {
		return name
	}
	ext := filepath.Ext(name)
	if len(ext) > 20 {
		ext = ""
	}
	stem := name[:len(name)-len(ext)]
	cut := max - len(ext)
	for cut > 0 && !utf8.RuneStart(stem[cut]) {
		cut--
	}
	return stem[:cut] + ext
}

// withSuffix puts suffix before the extension: "a.mkv" + " (2)" is "a (2).mkv".
func withSuffix(name, suffix string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + suffix + ext
}

func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}
