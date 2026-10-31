package api

import "testing"

func TestChooseDisplayFace(t *testing.T) {
	tests := []struct {
		name                      string
		storedTitle, storedPoster string
		localTitle, localPoster   string
		wantTitle, wantPoster     string
	}{
		{"uses the translated title and poster", "It Was Just an Accident", "/en.jpg", "Un semplice incidente", "/it.jpg", "Un semplice incidente", "/it.jpg"},
		{"keeps the stored poster when the language has none", "Words of War", "/en.jpg", "Anna - Parole di guerra", "", "Anna - Parole di guerra", "/en.jpg"},
		{"keeps the stored title when the translation is blank", "The Odyssey", "/en.jpg", "  ", "/it.jpg", "The Odyssey", "/it.jpg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseDisplayFace(tc.storedTitle, tc.storedPoster, tc.localTitle, tc.localPoster)
			if got.title != tc.wantTitle || got.poster != tc.wantPoster {
				t.Fatalf("got %q %q, want %q %q", got.title, got.poster, tc.wantTitle, tc.wantPoster)
			}
		})
	}
}
