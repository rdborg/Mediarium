package api

import "testing"

func TestChooseDisplayFace(t *testing.T) {
	tests := []struct {
		name                                      string
		storedTitle, storedPoster, storedOverview string
		localTitle, localPoster, localOverview    string
		wantTitle, wantPoster, wantOverview       string
	}{
		{"uses the translated title and poster", "It Was Just an Accident", "/en.jpg", "A plot.", "Un semplice incidente", "/it.jpg", "Una trama.", "Un semplice incidente", "/it.jpg", "Una trama."},
		{"keeps the stored poster when the language has none", "Words of War", "/en.jpg", "A plot.", "Anna - Parole di guerra", "", "Una trama.", "Anna - Parole di guerra", "/en.jpg", "Una trama."},
		{"keeps the stored title when the translation is blank", "The Odyssey", "/en.jpg", "A plot.", "  ", "/it.jpg", "Una trama.", "The Odyssey", "/it.jpg", "Una trama."},
		{"keeps the stored plot when the translation is blank", "The Odyssey", "/en.jpg", "A plot.", "Odissea", "/it.jpg", "  ", "Odissea", "/it.jpg", "A plot."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseDisplayFace(tc.storedTitle, tc.storedPoster, tc.storedOverview, tc.localTitle, tc.localPoster, tc.localOverview)
			if got.title != tc.wantTitle || got.poster != tc.wantPoster || got.overview != tc.wantOverview {
				t.Fatalf("got %q %q %q, want %q %q %q", got.title, got.poster, got.overview, tc.wantTitle, tc.wantPoster, tc.wantOverview)
			}
		})
	}
}
