package books

import (
	"errors"
	"strings"
	"testing"
)

func TestNewManualKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  error
	}{
		{"9781234567897", "manual:9781234567897", nil},
		{" B0ABC123 ", "manual:B0ABC123", nil},
		{"OL45883W", "manual:OL45883W", nil},
		{"a b", "", ErrBadExternalID},
		{"../x", "", ErrBadExternalID},
		{"-lead", "", ErrBadExternalID},
		{strings.Repeat("a", 65), "", ErrBadExternalID},
	}
	for _, tc := range cases {
		got, err := NewManualKey(tc.in)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("NewManualKey(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.err)
		}
	}
	a, _ := NewManualKey("")
	b, _ := NewManualKey("")
	if !IsManualKey(a) || a == b || len(a) != len(ManualKeyPrefix)+16 {
		t.Errorf("random keys: %q %q", a, b)
	}
}

func TestIsManualKey(t *testing.T) {
	for in, want := range map[string]bool{"manual:abc": true, " manual:abc": true, "OL1W": false, "hc-12": false, "gb-x": false, "audnexus:B0": false, "": false} {
		if got := IsManualKey(in); got != want {
			t.Errorf("IsManualKey(%q) = %v", in, got)
		}
	}
	if !(Book{OLKey: "manual:1"}).Manual() || (Book{OLKey: "OL1W"}).Manual() {
		t.Error("Book.Manual")
	}
}
