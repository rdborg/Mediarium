package api

import "testing"

func TestSpeedLimitHours(t *testing.T) {
	cases := []struct {
		in       string
		from, to int
		ok       bool
	}{
		{"8-23", 8, 23, true},
		{" 22-6 ", 22, 6, true},
		{"", 0, 0, false},
		{"8", 0, 0, false},
		{"8-8", 0, 0, false},
		{"24-3", 0, 0, false},
		{"a-b", 0, 0, false},
	}
	for _, tc := range cases {
		f, to, ok := parseHours(tc.in)
		if f != tc.from || to != tc.to || ok != tc.ok {
			t.Errorf("parseHours(%q) = %d,%d,%v want %d,%d,%v", tc.in, f, to, ok, tc.from, tc.to, tc.ok)
		}
	}
	window := []struct {
		hour, from, to int
		in             bool
	}{
		{8, 8, 23, true}, {22, 8, 23, true}, {23, 8, 23, false}, {7, 8, 23, false},
		{23, 22, 6, true}, {3, 22, 6, true}, {6, 22, 6, false}, {12, 22, 6, false},
	}
	for _, w := range window {
		if got := inHours(w.hour, w.from, w.to); got != w.in {
			t.Errorf("inHours(%d, %d-%d) = %v", w.hour, w.from, w.to, got)
		}
	}
}
