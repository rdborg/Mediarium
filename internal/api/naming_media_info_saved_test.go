package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediainfo"
	"github.com/rdborg/mediarium/internal/settings"
)

// A saved row names the file; a file with no row, or one that changed since,
// falls back to the release name.
func TestDestPathUsesSavedMediaInfo(t *testing.T) {
	s := newAuditServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	for k, v := range map[string]string{
		settings.KeyMoviesPath: movies, settings.KeyNamingPreset: "custom",
		settings.KeyMovieNameFormat: "{Movie Title} ({Year}){ [MediaInfo VideoCodec]}{ [MediaInfo AudioChannels]}{ [MediaInfo VideoBitDepth]}{ [MediaInfo SubtitleLanguages]}",
	} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(t.TempDir(), "[Judas] Movie.mkv")
	if err := os.WriteFile(file, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := func() string {
		got, err := s.buildDestPath("", "Movie", 2026, 9, "Movie", file)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Base(got)
	}
	if got, want := name(), "Movie (2026).mkv"; got != want {
		t.Errorf("unread file = %q, want %q", got, want)
	}
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(mediainfo.Info{
		Video:     &mediainfo.Video{Codec: "HEVC", BitDepth: 10},
		Audio:     []mediainfo.Audio{{Codec: "AAC", Channels: 6, Lang: "ja"}},
		Subtitles: []mediainfo.Subtitle{{Codec: "ASS", Lang: "en"}},
	})
	row := library.MediaInfoRow{Path: file, Size: st.Size(), MTime: st.ModTime().UnixNano(), Info: string(b), ScannedAt: "2026-11-21T00:00:00Z"}
	if err := s.MovieRepo.SaveMediaInfo(row); err != nil {
		t.Fatal(err)
	}
	if got, want := name(), "Movie (2026) [x265] [5.1] [10] [English].mkv"; got != want {
		t.Errorf("read file = %q, want %q", got, want)
	}
	row.Size++ // the file changed since it was read
	if err := s.MovieRepo.SaveMediaInfo(row); err != nil {
		t.Fatal(err)
	}
	if got, want := name(), "Movie (2026).mkv"; got != want {
		t.Errorf("changed file = %q, want %q", got, want)
	}
}
