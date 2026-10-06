package ebookconv

import "testing"

func TestBase32(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"0000", 0},
		{"000A", 10},
		{"000v", 31},
		{"0010", 32},
		{"1VVVVVV", 2147483647}, // the largest position allowed
		{"2000000", -1},         // one more: too large to be a real position
		{"VVVVVVVVVV", -1},
		{"", -1},
		{"00x0", -1},
	}
	for _, tt := range tests {
		if got := base32(tt.in); got != tt.want {
			t.Errorf("base32(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
