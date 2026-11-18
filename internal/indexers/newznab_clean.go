package indexers

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

// declaredEncoding finds the encoding an XML declaration names.
var declaredEncoding = regexp.MustCompile(`(?i)^\s*<\?xml[^>]*\sencoding\s*=\s*["']([^"']+)["']`)

// errEmptyReply is what a reply with nothing in it comes to.
var errEmptyReply = errors.New("the reply was empty")

// cleanFeed makes a reply from an indexer something Go's strict XML reader
// accepts, so one odd byte does not lose a whole page of releases:
//   - a gzip body that was sent without saying so is unpacked (a proxy that
//     compresses and leaves the header off);
//   - a byte order mark is dropped;
//   - a feed that declares another character set (ISO-8859-1, windows-1252 ...)
//     is turned into UTF-8 and says so;
//   - bytes that are not valid UTF-8 become the replacement character, and the
//     control characters XML does not allow (backspace, null ...) are dropped.
//
// A reply with nothing in it gives errEmptyReply.
func cleanFeed(body []byte) ([]byte, error) {
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("the reply is compressed but could not be unpacked: %w", err)
		}
		defer zr.Close()
		plain, err := io.ReadAll(io.LimitReader(zr, maxNewznabBytes+1))
		if err != nil && len(plain) == 0 {
			return nil, fmt.Errorf("the reply is compressed but could not be unpacked: %w", err)
		}
		if len(plain) > maxNewznabBytes {
			return nil, fmt.Errorf("the reply is larger than %d MB once unpacked", maxNewznabBytes>>20)
		}
		body = plain
	}
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errEmptyReply
	}
	if m := declaredEncoding.FindSubmatchIndex(body[:min(len(body), 512)]); m != nil {
		label := strings.TrimSpace(string(body[m[2]:m[3]]))
		if enc := strings.ToLower(label); enc != "utf-8" && enc != "utf8" && enc != "us-ascii" && enc != "ascii" {
			if r, err := charset.NewReaderLabel(label, bytes.NewReader(body)); err == nil {
				if conv, err := io.ReadAll(r); err == nil {
					// The declaration would now lie: the text is UTF-8.
					if m2 := declaredEncoding.FindSubmatchIndex(conv[:min(len(conv), 512)]); m2 != nil {
						conv = append(append(append([]byte{}, conv[:m2[2]]...), "UTF-8"...), conv[m2[3]:]...)
					}
					body = conv
				}
			}
		}
	}
	if !utf8.Valid(body) {
		body = bytes.ToValidUTF8(body, []byte("�"))
	}
	for _, b := range body {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			out := make([]byte, 0, len(body))
			for _, c := range body {
				if c >= 0x20 || c == '\t' || c == '\n' || c == '\r' {
					out = append(out, c)
				}
			}
			return out, nil
		}
	}
	return body, nil
}
