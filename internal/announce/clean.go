// Package announce listens to the IRC channels where trackers announce new
// releases, reads each announcement with a template, and hands what it found
// to the caller to decide on. It is the engine behind Settings > Finding releases >
// Indexers & Search > IRC announce.
//
// The package knows nothing about the library: internal/api decides whether a
// release is wanted and grabs it. Everything that arrives from the network is
// treated as hostile text: control characters and IRC colour codes are removed
// before anything reads a line, line lengths and queues are bounded, and
// nothing taken from an announcement is ever sent back to the IRC server or
// used to build an address that carries a secret.
package announce

import (
	"strings"
	"unicode/utf8"
)

// MaxTextRunes is the longest announcement text that is read. Anything longer
// is cut: an IRC line is at most 512 bytes, so this only matters for a
// misbehaving server.
const MaxTextRunes = 1024

// Clean removes what must not reach a parser, a log or the screen from a line
// of IRC text: formatting codes (bold, colour, italics, underline, reverse,
// reset, monospace, strike-through), every other control character, the
// invisible Unicode characters that can reorder or hide text, and bytes that
// are not valid UTF-8. Tabs become spaces. The result is trimmed and cut to
// MaxTextRunes.
func Clean(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	n := 0
	for i := 0; i < len(s) && n < MaxTextRunes; {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++ // not UTF-8: dropped
			continue
		}
		i += size
		switch {
		case r == 0x03: // colour: ^C, up to two digits, optionally ,up to two digits
			fg := skipDigits(s[i:], 2, isDigit)
			i += fg
			if fg > 0 && i < len(s) && s[i] == ',' && i+1 < len(s) && isDigit(s[i+1]) {
				i++
				i += skipDigits(s[i:], 2, isDigit)
			}
			continue
		case r == 0x04: // hex colour: ^D, up to six hex digits, optionally ,six more
			fg := skipDigits(s[i:], 6, isHex)
			i += fg
			if fg > 0 && i < len(s) && s[i] == ',' && i+1 < len(s) && isHex(s[i+1]) {
				i++
				i += skipDigits(s[i:], 6, isHex)
			}
			continue
		case r == '\t':
			r = ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			continue
		case r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2060 && r <= 0x2064, r >= 0x2066 && r <= 0x2069, r == 0xfeff, r == 0x2028, r == 0x2029:
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// skipDigits counts how many of the first max bytes of s satisfy ok.
func skipDigits(s string, max int, ok func(byte) bool) int {
	n := 0
	for n < len(s) && n < max && ok(s[n]) {
		n++
	}
	return n
}

// safeParam makes a value safe to send as part of an IRC command: no line
// breaks, no NUL, no other control characters, and at most max bytes cut on a
// character boundary. Values that come from settings are checked when they are
// saved; this is the second lock on the door.
func safeParam(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError || r < 0x20 || r == 0x7f {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > max {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
