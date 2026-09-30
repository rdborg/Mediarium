package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestPreferredLanguageSetting(t *testing.T) {
	_, base, client := loginNewServer(t)

	got := getJSON[map[string]any](t, client, base+"/api/settings")
	if got["qualityLanguage"] != "English" {
		t.Fatalf("the default language is %v, want English", got["qualityLanguage"])
	}

	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "Italian"}, http.StatusOK)
	got = getJSON[map[string]any](t, client, base+"/api/settings")
	if got["qualityLanguage"] != "Italian" {
		t.Fatalf("the language is %v after saving Italian", got["qualityLanguage"])
	}

	// A made-up language is refused, and the saved one stays.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "Klingon"}, http.StatusBadRequest)
	got = getJSON[map[string]any](t, client, base+"/api/settings")
	if got["qualityLanguage"] != "Italian" {
		t.Fatalf("the language is %v after a refused change", got["qualityLanguage"])
	}

	// Saving something else leaves the language alone.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"automationEnabled": true}, http.StatusOK)
	got = getJSON[map[string]any](t, client, base+"/api/settings")
	if got["qualityLanguage"] != "Italian" {
		t.Fatalf("the language changed to %v", got["qualityLanguage"])
	}
}

func TestAutomationSkipsReleasesInAnotherLanguage(t *testing.T) {
	const (
		plain   = "Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP"
		iTaEng  = "Fixture.Show.S01E01.iTA.ENG.1080p.WEB-DL.x264-GRP"
		italian = "Fixture.Show.S01E01.ITA.1080p.WEB-DL.x264-GRP"
		german  = "Fixture.Show.S01E01.GERMAN.1080p.WEB-DL.x264-GRP"
	)
	missing := []episodeSpec{{1, 1, "2020-01-01", library.StatusMissing, ""}}
	tests := []struct {
		name     string
		language string // "" leaves the default, English
		titles   []string
		want     []string
	}{
		{"only Italian and German: nothing is grabbed", "", []string{italian, german}, nil},
		{"plain English is taken over Italian and English", "", []string{iTaEng, plain, italian}, []string{plain}},
		{"Italian and English is taken when nothing plain exists", "", []string{italian, iTaEng}, []string{iTaEng}},
		{"Italian wanted: Italian is taken", "Italian", []string{german, italian}, []string{italian}},
		{"Italian wanted: German is still refused", "Italian", []string{german}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newTVAutoEnv(t, tc.titles, missing)
			if tc.language != "" {
				putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"qualityLanguage": tc.language}, http.StatusOK)
			}
			env.server.TestHunt(context.Background())
			requireTitles(t, env.grabbedTitles(t), tc.want...)
			env.finish(t)
		})
	}

	t.Run("the RSS sync applies the same rule", func(t *testing.T) {
		env := newTVAutoEnv(t, []string{italian, iTaEng, plain}, missing)
		env.server.TestRSSSync(context.Background())
		requireTitles(t, env.grabbedTitles(t), plain)
		env.finish(t)
	})
}

// The release list shows the languages of each release and warns about one
// that is clearly in another language, which can still be grabbed by hand.
func TestSearchResultsShowTheLanguage(t *testing.T) {
	const (
		plain   = "Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP"
		iTaEng  = "Fixture.Show.S01E01.iTA.ENG.1080p.WEB-DL.x264-GRP"
		italian = "Fixture.Show.S01E01.ITA.1080p.WEB-DL.x264-GRP"
		multi   = "Fixture.Show.S01E01.MULTi.1080p.WEB-DL.x264-GRP"
	)
	env := newTVAutoEnv(t, []string{plain, iTaEng, italian, multi}, []episodeSpec{{1, 1, "2020-01-01", library.StatusMissing, ""}})
	type row struct {
		Title       string   `json:"title"`
		Language    string   `json:"language"`
		LanguageFit string   `json:"languageFit"`
		Rejections  []string `json:"rejections"`
		DownloadURL string   `json:"downloadUrl"`
	}
	rows := getJSON[[]row](t, env.client, env.baseURL+"/api/series/"+itoa(env.seriesID)+"/search?season=1&episode=1")
	byTitle := map[string]row{}
	for _, r := range rows {
		byTitle[r.Title] = r
	}
	want := map[string]struct{ language, fit string }{
		plain:   {"", ""},
		iTaEng:  {"Italian + English", "mixed"},
		italian: {"Italian", "other"},
		multi:   {"Multi", "mixed"},
	}
	for title, w := range want {
		got, ok := byTitle[title]
		if !ok {
			t.Errorf("%s is missing from the list", title)
			continue
		}
		if got.Language != w.language || got.LanguageFit != w.fit {
			t.Errorf("%s: language %q fit %q, want %q %q", title, got.Language, got.LanguageFit, w.language, w.fit)
		}
	}
	if r := byTitle[italian]; len(r.Rejections) == 0 || r.Rejections[0] != "Italian audio, not English" {
		t.Errorf("the Italian release should say why automation skips it: %v", r.Rejections)
	}
	if r := byTitle[iTaEng]; len(r.Rejections) != 0 {
		t.Errorf("Italian and English is acceptable, got rejections %v", r.Rejections)
	}

	// A manual grab of the Italian release is still allowed.
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/series/"+itoa(env.seriesID)+"/grab", map[string]any{
		"releaseTitle": italian, "downloadUrl": byTitle[italian].DownloadURL, "protocol": "usenet", "season": 1, "episode": 1,
	}, http.StatusAccepted)
	env.finish(t)
}
