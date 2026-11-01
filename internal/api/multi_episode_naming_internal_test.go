package api

import (
	"slices"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestMultiEpisodeNaming(t *testing.T) {
	first, more, title := multiEpisodeNaming([]library.Episode{
		{Season: 1, Episode: 3, Title: "Three"},
		{Season: 1, Episode: 2, Title: "Two"},
		{Season: 1, Episode: 2, Title: "Two"},
	})
	if first.Episode != 2 || !slices.Equal(more, []int{3}) || title != "Two + Three" {
		t.Errorf("got first %d, more %v, title %q", first.Episode, more, title)
	}
	first, more, title = multiEpisodeNaming([]library.Episode{{Season: 2, Episode: 5, Title: "Five"}})
	if first.Episode != 5 || more != nil || title != "Five" {
		t.Errorf("one episode: got first %d, more %v, title %q", first.Episode, more, title)
	}
	_, _, title = multiEpisodeNaming([]library.Episode{{Season: 1, Episode: 1}, {Season: 1, Episode: 2, Title: "Two"}})
	if title != "Two" {
		t.Errorf("a missing first title: got %q", title)
	}
}
