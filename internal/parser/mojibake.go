package parser

import (
	"strings"
	"unicode/utf8"
)

// cp1252High maps the characters Windows-1252 puts at 0x80-0x9F back to those
// byte values. The rest of the range (0x00-0xFF) is the same as Latin-1.
var cp1252High = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87,
	'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E,
	'‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// repairMojibake undoes the common mistake of reading UTF-8 text as Latin-1 or
// Windows-1252 ("Sams im GlÃ¼ck" for "Sams im Glück"). Some indexers hand out
// names like that, and the broken letters keep the title from matching. The
// name is only changed when it holds the telltale pair (Ã or Â followed by a
// character in the continuation range) and the bytes read back as valid UTF-8
// with fewer characters; anything else is left as it is.
func repairMojibake(s string) string {
	if !strings.ContainsAny(s, "ÃÂ") {
		return s
	}
	rs := []rune(s)
	telltale := false
	buf := make([]byte, 0, len(s))
	for i, r := range rs {
		var b byte
		switch {
		case r < 0x100:
			b = byte(r)
		default:
			c, ok := cp1252High[r]
			if !ok {
				return s // a character no single-byte reading has: not this mistake
			}
			b = c
		}
		if (r == 'Ã' || r == 'Â') && i+1 < len(rs) && isContinuation(rs[i+1]) {
			telltale = true
		}
		buf = append(buf, b)
	}
	if !telltale || !utf8.Valid(buf) {
		return s
	}
	fixed := string(buf)
	if utf8.RuneCountInString(fixed) >= len(rs) {
		return s
	}
	return fixed
}

// isContinuation reports whether r is what a UTF-8 continuation byte (0x80-0xBF)
// looks like when read as Latin-1 or Windows-1252.
func isContinuation(r rune) bool {
	if r >= 0x80 && r <= 0xBF {
		return true
	}
	b, ok := cp1252High[r]
	return ok && b >= 0x80 && b <= 0xBF
}
