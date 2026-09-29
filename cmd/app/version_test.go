package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// semver is the Semantic Versioning 2.0.0 grammar (semver.org), without a
// leading "v": MAJOR.MINOR.PATCH, an optional -pre.release and +build.
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func TestSemverPattern(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"1.0.0", true},
		{"1.1.0-beta.1", true},
		{"2.0.0-rc.1+build.5", true},
		{"10.20.30", true},
		{"v1.0.0", false}, // no leading v in VERSION; the git tag adds it
		{"1.0", false},
		{"01.0.0", false},
		{"1.0.0-", false},
		{"dev", false},
	}
	for _, tc := range cases {
		if got := semver.MatchString(tc.in); got != tc.ok {
			t.Errorf("semver(%q) = %v, want %v", tc.in, got, tc.ok)
		}
	}
}

// The VERSION file at the repository root is the single source of the
// release number (the Dockerfile and the release workflow read it), so it
// must hold exactly one semantic version.
func TestVersionFileIsSemver(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	v := strings.TrimSpace(string(raw))
	if !semver.MatchString(v) {
		t.Fatalf("VERSION holds %q, which is not a semantic version like 1.2.3 or 1.3.0-beta.1", v)
	}
	if strings.Count(string(raw), "\n") > 1 {
		t.Fatalf("VERSION must be a single line, got %q", raw)
	}
}
