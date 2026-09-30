package api_test

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func TestCalendarMergesEpisodeAirsWithinWindow(t *testing.T) {
	day := func(offset int) string { return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02") }
	env := newTVAutoEnv(t, nil, []episodeSpec{
		{1, 1, day(-400), library.StatusDownloaded, "WEBDL-1080p"}, // long ago: outside the window
		{1, 2, day(-3), library.StatusMissing, ""},
		{1, 3, day(7), library.StatusMissing, ""},
		{1, 4, day(400), library.StatusMissing, ""}, // far future: outside the window
		{1, 5, "", library.StatusMissing, ""},       // unannounced
	})
	if _, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Movie A", Year: 2030, Monitored: true, ReleaseDate: day(3)}); err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	entries := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/calendar")
	var order []string
	for _, e := range entries {
		switch e["kind"] {
		case "movie":
			order = append(order, "movie:"+e["title"].(string))
		case "episode":
			order = append(order, "episode:"+e["subtitle"].(string))
		}
	}
	want := []string{"episode:S01E02", "movie:Movie A", "episode:S01E03"}
	if len(order) != len(want) {
		t.Fatalf("calendar = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("calendar = %v, want %v", order, want)
		}
	}
}
