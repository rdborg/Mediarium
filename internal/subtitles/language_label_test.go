package subtitles

import "testing"

func TestLanguageLabel(t *testing.T) {
	tests := []struct{ code, want string }{
		{"en", "English"},
		{"EN", "English"},
		{" fr ", "French"},
		{"es", "Spanish"},
		{"id", "Indonesian"},
		{"xx", "XX"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			if got := LanguageLabel(tc.code); got != tc.want {
				t.Fatalf("LanguageLabel(%q) = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}
