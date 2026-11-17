package api

import "testing"

// The Activity line for a release a person blocklisted is one whole sentence
// for each scope, so the feed can translate it (the title-only note too).
func TestBlocklistedByYou(t *testing.T) {
	for _, c := range []struct {
		itemOnly bool
		want     string
	}{
		{false, `Blocklisted "Some.Movie.2001.1080p": by you`},
		{true, `Blocklisted "Some.Movie.2001.1080p": by you (only for this title)`},
	} {
		if got := blocklistedByYou("Some.Movie.2001.1080p", c.itemOnly); got != c.want {
			t.Errorf("itemOnly=%v: %q, want %q", c.itemOnly, got, c.want)
		}
	}
}
