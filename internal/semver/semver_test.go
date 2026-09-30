package semver

import (
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want string // canonical text; empty means an error is expected
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{" 1.2.3\n", "1.2.3"},
		{"1.10.0", "1.10.0"},
		{"0.0.0", "0.0.0"},
		{"1.2.3-rc.1", "1.2.3-rc.1"},
		{"1.2.3-rc.1+build.5", "1.2.3-rc.1"},
		{"1.2.3+build5", "1.2.3"},
		{"1.2.3-0", "1.2.3-0"},
		{"1.2.3-x-y.z", "1.2.3-x-y.z"},
		{"", ""},
		{"dev", ""},
		{"latest", ""},
		{"1", ""},
		{"1.2", ""},
		{"1.2.3.4", ""},
		{"01.2.3", ""},
		{"1.02.3", ""},
		{"1.2.-3", ""},
		{"1.2.x", ""},
		{"1.2.3-", ""},
		{"1.2.3-rc..1", ""},
		{"1.2.3-01", ""},
		{"1.2.3+", ""},
		{"1.2.3-rc_1", ""},
		{"vv1.2.3", ""},
		{"1.2.3 beta", ""},
		{"9999999999.0.0", ""},
		{strings.Repeat("1", 80) + ".0.0", ""},
		{"<script>", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			v, err := Parse(tc.in)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want an error", tc.in, v)
				}
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("error %v does not wrap ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			if v.String() != tc.want {
				t.Fatalf("Parse(%q) = %q, want %q", tc.in, v.String(), tc.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.9.0", 1}, // numbers, not text: 10 is more than 9
		{"1.9.0", "1.10.0", -1},
		{"1.2.10", "1.2.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0+abc", "1.2.0+xyz", 0}, // build details are ignored
		// Pre-releases.
		{"1.2.0-rc.1", "1.2.0", -1},
		{"1.2.0", "1.2.0-rc.1", 1},
		{"1.2.0-rc.1", "1.1.9", 1},
		{"1.2.0-alpha", "1.2.0-beta", -1},
		{"1.2.0-beta.2", "1.2.0-beta.11", -1}, // numbers by value
		{"1.2.0-beta.11", "1.2.0-beta.2", 1},
		{"1.2.0-1", "1.2.0-alpha", -1}, // a number is older than a word
		{"1.2.0-alpha", "1.2.0-alpha.1", -1},
		{"1.2.0-alpha.1", "1.2.0-alpha", 1},
		{"1.2.0-rc.1", "1.2.0-rc.1", 0},
		// The examples from semver.org, in order.
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-beta.11", "1.0.0-rc.1", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
	}
	for _, tc := range cases {
		t.Run(tc.a+" vs "+tc.b, func(t *testing.T) {
			got, err := Compare(tc.a, tc.b)
			if err != nil {
				t.Fatalf("Compare(%q, %q): %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			// The opposite order must give the opposite answer.
			back, _ := Compare(tc.b, tc.a)
			if back != -tc.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tc.b, tc.a, back, -tc.want)
			}
		})
	}
}

func TestCompareGarbage(t *testing.T) {
	for _, pair := range [][2]string{{"dev", "1.0.0"}, {"1.0.0", "dev"}, {"", ""}, {"nightly", "latest"}} {
		if _, err := Compare(pair[0], pair[1]); err == nil {
			t.Errorf("Compare(%q, %q) gave no error", pair[0], pair[1])
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	for in, want := range map[string]bool{"1.0.0": false, "1.0.0-rc.1": true, "1.0.0+build": false, "v2.0.0-beta": true} {
		v, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if v.IsPrerelease() != want {
			t.Errorf("IsPrerelease(%q) = %v, want %v", in, v.IsPrerelease(), want)
		}
	}
}
