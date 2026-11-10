package api

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

// The words asked of Soulseek lose punctuation and bracketed tails, and an
// empty answer has wider questions to fall back on (#143).
func TestSoulseekQueries(t *testing.T) {
	cases := []struct {
		name          string
		artist, album string
		want          []string
	}{
		{"punctuation", "AC/DC", "Back in Black", []string{"AC DC Back in Black", "Back in Black"}},
		{"edition tail", "Radiohead", "OK Computer (Deluxe Edition)", []string{"Radiohead OK Computer", "OK Computer"}},
		{"square brackets", "Daft Punk", "Discovery [Remastered]", []string{"Daft Punk Discovery", "Discovery"}},
		{"apostrophe", "Guns N' Roses", "Appetite for Destruction", []string{"Guns N Roses Appetite for Destruction", "Appetite for Destruction"}},
		{"various artists", "Various Artists", "Neon Harbor: Original Motion Picture Soundtrack", []string{"Neon Harbor Original Motion Picture Soundtrack", "Neon Harbor Original"}},
		{"long title", "Mira Vale", "The Quiet Hours of a Long Summer", []string{"Mira Vale The Quiet Hours of a Long Summer", "The Quiet Hours of a Long Summer", "Mira Vale The Quiet Hours"}},
		{"title is all brackets", "Odd Band", "(Untitled)", []string{"Odd Band Untitled", "Untitled"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := soulseekQueries(music.Artist{Name: c.artist}, music.Album{Title: c.album})
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("queries = %q, want %q", got, c.want)
			}
		})
	}
}
