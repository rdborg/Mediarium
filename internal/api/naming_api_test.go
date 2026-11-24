package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNamingFormatsFromRadarrAndSonarr(t *testing.T) {
	_, base, client := loginNewServer(t)

	// A format copied from Radarr previews, saves and comes back.
	radarr := "{Movie CleanTitle} ({Release Year}) - {Custom Formats}{ - Edition Tags}"
	p := getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?format="+url.QueryEscape(radarr))
	if p["filename"] != "Example Movie (2024) - Extended.mkv" || p["problem"] != nil {
		t.Fatalf("movie preview = %v", p)
	}
	sonarr := "{Series TitleYear} - S{season:00}E{episode:00} - {Episode CleanTitle} [{Quality Full}]"
	p = getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?kind=tv&format="+url.QueryEscape(sonarr))
	if p["filename"] != "Example Show (2023) - S01E02 - The Second One [WEBDL-1080p].mkv" || p["folder"] != "Example Show (2023)/Season 01" {
		t.Fatalf("episode preview = %v", p)
	}
	saved := putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "movieNameFormat": radarr, "episodeNameFormat": sonarr}, http.StatusOK)
	if saved["movieNameFormat"] != radarr || saved["episodeNameFormat"] != sonarr {
		t.Fatalf("saved = %v", saved)
	}

	// The TRaSH format writes the codec and the channels as two groups sharing one
	// bracket; the sample has both, so the preview closes it.
	trash := "{Movie CleanTitle} ({Release Year}) {[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoCodec]}{-Release Group}"
	p = getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?format="+url.QueryEscape(trash))
	if p["filename"] != "Example Movie (2024) [Bluray-1080p][DTS 5.1][x265]-GROUP.mkv" {
		t.Fatalf("TRaSH movie preview = %v", p["filename"])
	}

	// An episode format that would give two episodes the same name is refused.
	bad := putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "episodeNameFormat": "{Series Title} - {Episode Title}"}, http.StatusBadRequest)
	if msg, _ := bad["error"].(string); !strings.Contains(msg, "{Season} and {Episode}") {
		t.Fatalf("bad episode format = %v", bad)
	}
	p = getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?format="+url.QueryEscape("{Movie Title} {Made Up}"))
	if msg, _ := p["problem"].(string); !strings.Contains(msg, "{Made Up}") {
		t.Fatalf("problem = %v", p)
	}

	// A draft saved while another style was picked is checked once Custom is
	// picked, even when the draft itself isn't sent again.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "plex", "movieNameFormat": "{Release Year} {Quality}"}, http.StatusOK)
	bad = putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom"}, http.StatusBadRequest)
	if msg, _ := bad["error"].(string); !strings.Contains(msg, "movie's title") {
		t.Fatalf("switching to a bad movie draft = %v", bad)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "plex", "movieNameFormat": radarr, "episodeNameFormat": "{Series Title} {Air-Date}"}, http.StatusOK)
	bad = putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom"}, http.StatusBadRequest)
	if msg, _ := bad["error"].(string); !strings.Contains(msg, "{Season} and {Episode}") {
		t.Fatalf("switching to a bad episode draft = %v", bad)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "episodeNameFormat": sonarr}, http.StatusOK)

	list := getJSON[map[string]any](t, client, base+"/api/settings/naming-tokens")
	tokens, _ := list["tokens"].([]any)
	schemes, _ := list["schemes"].([]any)
	if len(tokens) < 30 || len(schemes) < 4 {
		t.Fatalf("token list: %d tokens, %d schemes", len(tokens), len(schemes))
	}
}

// Every ready-made format must pass the same check a person's own format
// does, and save, for movies and for TV. The TRaSH formats are long, so this
// guards the length limit too.
func TestEveryNamingSchemeSaves(t *testing.T) {
	_, base, client := loginNewServer(t)
	list := getJSON[map[string]any](t, client, base+"/api/settings/naming-tokens")
	schemes, _ := list["schemes"].([]any)
	if len(schemes) == 0 {
		t.Fatal("no schemes")
	}
	for _, raw := range schemes {
		sc, _ := raw.(map[string]any)
		id, _ := sc["id"].(string)
		movie, _ := sc["movie"].(string)
		episode, _ := sc["episode"].(string)
		t.Run(id, func(t *testing.T) {
			saved := putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "movieNameFormat": movie, "episodeNameFormat": episode}, http.StatusOK)
			if saved["movieNameFormat"] != movie || saved["episodeNameFormat"] != episode {
				t.Fatalf("saved = %v", saved)
			}
		})
	}
	long := strings.Repeat("{Movie Title} ", 60)
	bad := putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "movieNameFormat": long}, http.StatusBadRequest)
	if msg, _ := bad["error"].(string); !strings.Contains(msg, "500 characters") {
		t.Fatalf("too long = %v", bad)
	}
}

// The folder formats save, preview in the sample's path, and are checked like
// the file name formats.
func TestFolderFormatsSavePreviewAndCheck(t *testing.T) {
	_, base, client := loginNewServer(t)
	list := getJSON[map[string]any](t, client, base+"/api/settings/naming-tokens")
	folderSchemes, _ := list["folderSchemes"].([]any)
	if len(folderSchemes) < 5 {
		t.Fatalf("folder schemes = %v", list["folderSchemes"])
	}
	for _, raw := range folderSchemes {
		sc, _ := raw.(map[string]any)
		id, _ := sc["id"].(string)
		movie, _ := sc["movie"].(string)
		series, _ := sc["series"].(string)
		saved := putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "custom", "movieFolderFormat": movie, "seriesFolderFormat": series}, http.StatusOK)
		if saved["movieFolderFormat"] != movie || saved["seriesFolderFormat"] != series {
			t.Fatalf("scheme %s saved = %v", id, saved)
		}
	}

	p := getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?format="+url.QueryEscape("{Movie Title} ({Year})")+"&folder="+url.QueryEscape("{Movie CleanTitle} ({Release Year}) [tmdbid-{TmdbId}]"))
	if p["folder"] != "Example Movie (2024) [tmdbid-12345]" || p["problem"] != nil {
		t.Fatalf("movie preview = %v", p)
	}
	p = getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?kind=tv&folder="+url.QueryEscape("{Series TitleYear} {tmdb-{TmdbId}}"))
	if p["folder"] != "Example Show (2023) {tmdb-67890}/Season 01" || p["problem"] != nil {
		t.Fatalf("show preview = %v", p)
	}
	// Without a folder format the preview shows the usual folder.
	p = getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?kind=tv")
	if p["folder"] != "Example Show (2023)/Season 01" {
		t.Fatalf("default show preview = %v", p)
	}
	p = getJSON[map[string]any](t, client, base+"/api/settings/naming-preview?folder="+url.QueryEscape("{Movie Title} [{Quality}]"))
	if msg, _ := p["problem"].(string); !strings.Contains(msg, "{Quality}") {
		t.Fatalf("quality in a folder = %v", p)
	}

	for _, bad := range []map[string]any{
		{"namingPreset": "custom", "movieFolderFormat": "{Release Year}"},
		{"namingPreset": "custom", "seriesFolderFormat": "{Series Title}/{Season}"},
		{"namingPreset": "custom", "seriesFolderFormat": "{Series Title} {Made Up}"},
		{"namingPreset": "custom", "movieFolderFormat": strings.Repeat("{Movie Title} ", 60)},
	} {
		putJSONStatus(t, client, base+"/api/settings", bad, http.StatusBadRequest)
	}
}
