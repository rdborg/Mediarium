// Package semver reads and compares Semantic Versioning 2.0.0 numbers
// (semver.org), such as 1.10.0 or 1.2.0-rc.1. It is small on purpose: the
// update notice and the pushed-update checks both rely on it, and a wrong
// answer here would offer a downgrade or refuse an upgrade.
package semver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalid is wrapped by every parse error.
var ErrInvalid = errors.New("not a version number")

// Version is a parsed version. Build metadata (after a +) is dropped, since it
// takes no part in ordering.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string // pre-release identifiers, empty for a normal release
}

// maxLen keeps a hostile or garbled tag from being parsed at all.
const maxLen = 64

// Parse reads a version such as "1.2.3", "v1.2.3", "1.2.3-rc.1" or
// "1.2.3+build5". The leading v is allowed because release tags carry one.
// Anything that is not three whole numbers (1.2 and dev are refused) is an
// error.
func Parse(s string) (Version, error) {
	orig := s
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s) > maxLen {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalid, clip(orig))
	}
	if s[0] == 'v' || s[0] == 'V' {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		if !validIdentifiers(s[i+1:], false) {
			return Version{}, fmt.Errorf("%w: %q", ErrInvalid, clip(orig))
		}
		s = s[:i]
	}
	var pre string
	hasPre := false
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre, hasPre = s[i+1:], true
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalid, clip(orig))
	}
	var nums [3]uint64
	for i, p := range parts {
		n, ok := parseNumber(p)
		if !ok {
			return Version{}, fmt.Errorf("%w: %q", ErrInvalid, clip(orig))
		}
		nums[i] = n
	}
	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if hasPre {
		if !validIdentifiers(pre, true) {
			return Version{}, fmt.Errorf("%w: %q", ErrInvalid, clip(orig))
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, nil
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// parseNumber reads a whole number without a leading zero, at most 9 digits.
func parseNumber(s string) (uint64, bool) {
	if s == "" || len(s) > 9 || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

// validIdentifiers checks a dot-separated list of pre-release or build
// identifiers. Pre-release numbers may not have a leading zero.
func validIdentifiers(s string, pre bool) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		numeric := true
		for _, c := range id {
			switch {
			case c >= '0' && c <= '9':
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
				numeric = false
			default:
				return false
			}
		}
		if pre && numeric && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

// IsPrerelease reports whether v is a pre-release (1.2.0-rc.1).
func (v Version) IsPrerelease() bool { return len(v.Pre) > 0 }

// String is the canonical text, without a leading v or build metadata.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Compare returns -1, 0 or 1 as v is older than, the same as, or newer than
// o. A pre-release is older than the release it leads up to (1.2.0-rc.1 is
// older than 1.2.0).
func (v Version) Compare(o Version) int {
	for _, p := range [][2]uint64{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		switch {
		case p[0] < p[1]:
			return -1
		case p[0] > p[1]:
			return 1
		}
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		if c := compareIdentifier(v.Pre[i], o.Pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.Pre) < len(o.Pre):
		return -1
	case len(v.Pre) > len(o.Pre):
		return 1
	}
	return 0
}

// compareIdentifier orders two pre-release identifiers: numbers by value,
// words by their letters, and a number before a word.
func compareIdentifier(a, b string) int {
	an, aok := parseNumber(a)
	bn, bok := parseNumber(b)
	switch {
	case aok && bok:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aok:
		return -1
	case bok:
		return 1
	}
	return strings.Compare(a, b)
}

// Compare parses both versions and compares them.
func Compare(a, b string) (int, error) {
	va, err := Parse(a)
	if err != nil {
		return 0, err
	}
	vb, err := Parse(b)
	if err != nil {
		return 0, err
	}
	return va.Compare(vb), nil
}
