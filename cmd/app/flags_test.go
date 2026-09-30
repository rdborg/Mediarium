package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

// The two flags the container entrypoint uses must print and exit without
// touching anything, and must not be confused with a normal start.
func TestProbeFlags(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "1.4.0"

	cases := []struct {
		name       string
		args       []string
		handled    bool
		code       int
		stdout     string
		stderrPart string
	}{
		{"no arguments is a normal start", nil, false, 0, "", ""},
		{"reset-password is not a probe", []string{"reset-password", "ryan"}, false, 0, "", ""},
		{"version check", []string{"--version-check"}, true, 0, "mediarium 1.4.0 " + runtime.GOOS + "/" + runtime.GOARCH + "\n", ""},
		{"a newer update is accepted", []string{"--accepts-update", "1.5.0"}, true, 0, "", ""},
		{"1.10 is newer than 1.4", []string{"--accepts-update", "1.10.0"}, true, 0, "", ""},
		{"the same version is accepted", []string{"--accepts-update", "1.4.0"}, true, 0, "", ""},
		{"an older update is refused", []string{"--accepts-update", "1.3.9"}, true, 1, "", "older"},
		{"a pre-release of this version is older", []string{"--accepts-update", "1.4.0-rc.1"}, true, 1, "", "older"},
		{"a leading v is fine", []string{"--accepts-update", "v2.0.0"}, true, 0, "", ""},
		{"not a version", []string{"--accepts-update", "banana"}, true, 2, "", "cannot compare"},
		{"no version given", []string{"--accepts-update"}, true, 2, "", "usage"},
		{"too many arguments", []string{"--accepts-update", "1.5.0", "x"}, true, 2, "", "usage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := handleProbeFlags(tc.args, &out, &errOut)
			if handled != tc.handled || code != tc.code || out.String() != tc.stdout || !strings.Contains(errOut.String(), tc.stderrPart) {
				t.Fatalf("handled=%v code=%d stdout=%q stderr=%q; want handled=%v code=%d stdout=%q stderr containing %q",
					handled, code, out.String(), errOut.String(), tc.handled, tc.code, tc.stdout, tc.stderrPart)
			}
		})
	}

	t.Run("a build with no version number cannot judge an update", func(t *testing.T) {
		version = "dev"
		var out, errOut bytes.Buffer
		if _, code := handleProbeFlags([]string{"--accepts-update", "1.5.0"}, &out, &errOut); code != 2 {
			t.Fatalf("code = %d, want 2", code)
		}
	})
}
