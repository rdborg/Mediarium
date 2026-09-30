package config

import "testing"

func TestEnvBool(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false}, {"1", true}, {"true", true}, {"TRUE", true}, {" yes ", true}, {"on", true},
		{"0", false}, {"false", false}, {"no", false}, {"banana", false},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("BUNDLED_FLARESOLVERR", tc.value)
			if got := Load().BundledFlareSolverr; got != tc.want {
				t.Fatalf("BUNDLED_FLARESOLVERR=%q: got %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestFolderVariablesAndDefaults(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "nothing set uses the plain defaults",
			want: Config{
				DownloadsDir: "/downloads", MoviesDir: "/movies", TVDir: "/tv",
				MusicDir: "/music", EbooksDir: "/ebooks", AudiobooksDir: "/audiobooks",
			},
		},
		{
			name: "one parent folder for everything",
			env: map[string]string{
				"DOWNLOADS_DIR": "/data/downloads", "MOVIES_DIR": "/data/Movies", "TV_DIR": "/data/tv",
				"MUSIC_DIR": "/data/Music", "EBOOKS_DIR": "/data/Ebooks", "AUDIOBOOKS_DIR": "/data/Audiobooks",
			},
			want: Config{
				DownloadsDir: "/data/downloads", MoviesDir: "/data/Movies", TVDir: "/data/tv",
				MusicDir: "/data/Music", EbooksDir: "/data/Ebooks", AudiobooksDir: "/data/Audiobooks",
			},
		},
		{
			name: "only the ebooks folder moved",
			env:  map[string]string{"EBOOKS_DIR": "/books"},
			want: Config{
				DownloadsDir: "/downloads", MoviesDir: "/movies", TVDir: "/tv",
				MusicDir: "/music", EbooksDir: "/books", AudiobooksDir: "/audiobooks",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"DOWNLOADS_DIR", "MOVIES_DIR", "TV_DIR", "MUSIC_DIR", "EBOOKS_DIR", "AUDIOBOOKS_DIR"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := Load()
			if got.DownloadsDir != tc.want.DownloadsDir || got.MoviesDir != tc.want.MoviesDir || got.TVDir != tc.want.TVDir ||
				got.MusicDir != tc.want.MusicDir || got.EbooksDir != tc.want.EbooksDir || got.AudiobooksDir != tc.want.AudiobooksDir {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
