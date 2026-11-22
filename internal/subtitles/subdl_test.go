package subtitles

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const srtBody = "1\n00:00:01,000 --> 00:00:02,000\nHello\n"

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

// The answer below follows the shape in SubDL's API documentation.
const subdlEpisodeAnswer = `{"status":true,"results":[{"tmdb_id":1399,"type":"tv","name":"Show"}],"subtitles":[
 {"release_name":"Show.S02E05.1080p.WEB.h264-GRP","name":"a.zip","url":"/subtitle/100-200.zip","season":2,"episode":5,"releases":["Show.S02E05.720p.HDTV.x264-OTHER"]},
 {"release_name":"Show.S02E06.1080p.WEB.h264-GRP","name":"b.zip","url":"/subtitle/101-201.zip","season":2,"episode":6},
 {"release_name":"Show Season 2","name":"c.zip","url":"/subtitle/102-202.zip","season":2,"episode":null,"full_season":true},
 {"release_name":"Show.S02E01-E10","name":"d.zip","url":"/subtitle/103-203.zip","season":"2","episode":"1","episode_from":1,"episode_end":10},
 {"release_name":"bad link","name":"e.zip","url":"https://elsewhere.example/x.zip","season":2,"episode":5}
]}`

func TestSubDLFind(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		got = append(got, q.Encode())
		switch {
		case q.Get("api_key") != "good-key":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"status":false,"statusCode":403,"error":"not_authorized","message":"Not Authorized"}`))
		case q.Get("tmdb_id") == "1399":
			w.Write([]byte(subdlEpisodeAnswer))
		default:
			w.Write([]byte(`{"status":false,"error":"can't find movie or tv"}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	c := NewSubDLWithBaseURL("good-key", srv.URL, srv.URL)
	res, err := c.Find(ctx, Query{ParentTMDBID: 1399, Season: 2, Episode: 5, Type: "episode", Language: "pt-BR"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Link != "/subtitle/100-200.zip" || res[0].Source != SourceSubDL || res[0].Language != "pt-BR" {
		t.Fatalf("results: %+v", res)
	}
	if q := got[0]; !strings.Contains(q, "languages=BR_PT") || !strings.Contains(q, "season_number=2") || !strings.Contains(q, "episode_number=5") || !strings.Contains(q, "type=tv") {
		t.Fatalf("query: %s", q)
	}
	// The other releases the subtitle fits count when scoring.
	if Score(res[0], "Show.S02E05.720p.HDTV.x264-OTHER.mkv") < 150 {
		t.Fatalf("score with a matching listed release: %v", Score(res[0], "Show.S02E05.720p.HDTV.x264-OTHER.mkv"))
	}

	// A film SubDL doesn't have: no results, no error.
	if res, err := c.Find(ctx, Query{TMDBID: 5, Language: "en"}); err != nil || len(res) != 0 {
		t.Fatalf("unknown film: %v %v", res, err)
	}
	if _, err := NewSubDLWithBaseURL("bad-key", srv.URL, srv.URL).Find(ctx, Query{TMDBID: 5}); !errors.Is(err, ErrSubDLKey) {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := NewSubDLWithBaseURL("", srv.URL, srv.URL).Find(ctx, Query{TMDBID: 5}); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("no key: %v", err)
	}
	if _, err := NewSubDLWithBaseURL("bad-key", srv.URL, srv.URL).Test(ctx); !errors.Is(err, ErrSubDLKey) {
		t.Fatalf("test with a bad key: %v", err)
	}
}

func TestSubDLDownload(t *testing.T) {
	files := map[string][]byte{
		"/subtitle/1-1.zip": zipOf(t, map[string]string{"Film.2020.1080p.srt": srtBody, "readme.txt": "hi"}),
		"/subtitle/2-2.zip": zipOf(t, map[string]string{
			"Show.S02E04.srt": srtBody + "four", "Show.S02E05.srt": srtBody + "five", "Show.S02E06.srt": srtBody + "six, the longest one",
		}),
		"/subtitle/3-3.zip": zipOf(t, map[string]string{"Film.ass": "[Script Info]"}),
		"/subtitle/4/file9": []byte(srtBody),
		"/subtitle/5-5.zip": []byte("PK not really a zip"),
		"/subtitle/6/file1": []byte("<html>an error page</html>"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(b)
	}))
	defer srv.Close()
	c := NewSubDLWithBaseURL("k", srv.URL, srv.URL)
	ctx := context.Background()

	tests := []struct {
		name, link      string
		season, episode int
		want            string // "" = an error
	}{
		{"one srt in the zip", "/subtitle/1-1.zip", 0, 0, srtBody},
		{"the episode out of a pack", "/subtitle/2-2.zip", 2, 5, srtBody + "five"},
		{"a film from a pack: the biggest", "/subtitle/2-2.zip", 0, 0, srtBody + "six, the longest one"},
		{"no srt inside", "/subtitle/3-3.zip", 0, 0, ""},
		{"a single file", "/subtitle/4/file9", 0, 0, srtBody},
		{"broken zip", "/subtitle/5-5.zip", 0, 0, ""},
		{"not a subtitle", "/subtitle/6/file1", 0, 0, ""},
		{"missing", "/subtitle/7-7.zip", 0, 0, ""},
		{"not a SubDL link", "https://elsewhere.example/x.zip", 0, 0, ""},
		{"a path trying to leave", "/subtitle/../../etc/passwd", 0, 0, ""},
	}
	for _, tt := range tests {
		got, err := c.Download(ctx, tt.link, tt.season, tt.episode)
		if tt.want == "" {
			if err == nil {
				t.Errorf("%s: no error, got %q", tt.name, got)
			}
			continue
		}
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: %q %v", tt.name, got, err)
		}
	}
}

func TestSubDLLanguageCodes(t *testing.T) {
	for in, want := range map[string]string{"en": "EN", "pt-BR": "BR_PT", "pt-PT": "PT", "zh-TW": "ZH", "fr": "FR", "sr": "SR", "fa": "FA"} {
		if got := subdlLanguage(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func TestSubDLSameLanguage(t *testing.T) {
	for _, c := range []struct {
		wanted, got string
		want        bool
	}{
		{"en", "EN", true},
		{"en", "en", true},
		{"en", "english", true},
		{"en", "English", true},
		{"en", "FR", false},
		{"en", "french", false},
		{"en", "", true},
		{"en", "Klingon", true}, // not recognised: shown, not hidden
		{"pt-BR", "BR_PT", true},
		{"pt-BR", "PT", true},
		{"pt-PT", "brazilian_portuguese", true},
		{"pt-BR", "ES", false},
		{"zh-CN", "ZH", true},
	} {
		if got := sameLanguage(c.wanted, c.got); got != c.want {
			t.Errorf("sameLanguage(%q, %q) = %v, want %v", c.wanted, c.got, got, c.want)
		}
	}
}

// SubDL answers "status true" with nothing for the TMDB id, with other
// languages for the IMDb id, and has the English ones under the title. Every
// lookup is tried, and each one is explained.
func TestSubDLTriesTheNextLookupAndExplainsEachOne(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		got = append(got, r.URL.RawQuery)
		switch {
		case q.Get("tmdb_id") != "":
			w.Write([]byte(`{"status":true,"results":[],"subtitles":[]}`))
		case q.Get("imdb_id") != "":
			w.Write([]byte(`{"status":true,"results":[{"sd_id":1,"type":"movie","name":"The Sixth Sense","imdb_id":"tt0167404","tmdb_id":661,"year":1999}],"subtitles":[
			 {"release_name":"a","name":"a.zip","lang":"french","language":"FR","url":"/subtitle/1-1.zip"},
			 {"release_name":"b","name":"b.zip","lang":"german","language":"DE","url":"/subtitle/1-2.zip"}]}`))
		default:
			w.Write([]byte(`{"status":true,"results":[],"subtitles":[
			 {"release_name":"The.Sixth.Sense.1999.1080p.BluRay.x264-YIFY","name":"a.zip","lang":"english","language":"EN","url":"/subtitle/10-20.zip","hi":false},
			 {"release_name":"The.Sixth.Sense.1999.SDH","name":"b.zip","lang":"english","language":"EN","url":"/subtitle/10-21.zip","hi":true},
			 {"release_name":"lowercase spelling","name":"c.zip","lang":"English","url":"/subtitle/10-22.zip"},
			 {"release_name":"full url","name":"d.zip","language":"EN","url":"https://dl.subdl.com/subtitle/10-23.zip"},
			 {"release_name":"not a link","name":"e.zip","language":"EN","url":"https://elsewhere.example/x.zip"},
			 {"release_name":"portuguese","name":"f.zip","language":"PT","url":"/subtitle/10-24.zip"}]}`))
		}
	}))
	defer srv.Close()

	ctx, diag := WithDiagnostics(context.Background())
	c := NewSubDLWithBaseURL("secret-key", srv.URL, srv.URL)
	res, err := c.Find(ctx, Query{TMDBID: 661, IMDBID: "tt0167404", Title: "The Sixth Sense", Year: 1999, Language: "en", Type: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	var links []string
	for _, r := range res {
		links = append(links, r.Link)
	}
	if want := "/subtitle/10-20.zip /subtitle/10-21.zip /subtitle/10-22.zip /subtitle/10-23.zip"; strings.Join(links, " ") != want {
		t.Fatalf("links: %v", links)
	}
	if !res[1].HearingImpaired || res[0].HearingImpaired {
		t.Fatalf("hearing impaired flags: %+v", res)
	}
	if Score(res[1], "x.mkv") >= Score(res[0], "x.mkv") {
		t.Fatalf("hearing-impaired should rank below the plain one: %v vs %v", Score(res[1], "x.mkv"), Score(res[0], "x.mkv"))
	}

	lines := diag.List()
	if len(lines) != 3 {
		t.Fatalf("lines: %+v", lines)
	}
	if l := lines[0]; l.Source != "SubDL" || l.Lookup != "tmdb" || l.Value != "661" || l.Count != 0 || l.Found != 0 || l.Note != "" {
		t.Fatalf("tmdb line: %+v", l)
	}
	if l := lines[1]; l.Lookup != "imdb" || l.Found != 2 || l.Count != 0 || l.Note != "wrong-language" || l.Detail != "DE, FR" {
		t.Fatalf("imdb line: %+v", l)
	}
	if l := lines[2]; l.Lookup != "title" || l.Value != "The Sixth Sense (1999)" || l.Count != 4 || l.Found != 6 {
		t.Fatalf("title line: %+v", l)
	}
	for _, q := range got {
		if strings.Contains(q, "film_name") && strings.Contains(q, "tmdb_id") {
			t.Fatalf("the title and the id were sent together: %s", q)
		}
	}
	for _, l := range lines {
		if strings.Contains(fmt.Sprint(l), "secret-key") {
			t.Fatalf("a key is in %+v", l)
		}
	}
}

// With nothing found by any lookup, each is tried again without the language
// and the extras; the language is then judged here.
func TestSubDLFallsBackToFewerFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("languages") != "" || q.Get("hi") != "" || q.Get("comment") != "" {
			// A server that is picky about those: finds nothing.
			w.Write([]byte(`{"status":true,"results":[],"subtitles":[]}`))
			return
		}
		w.Write([]byte(`{"status":true,"subtitles":[
		 {"release_name":"fits","name":"a.zip","lang":"english","language":"EN","url":"/subtitle/1-1.zip"},
		 {"release_name":"no language given","name":"b.zip","url":"/subtitle/1-2.zip"},
		 {"release_name":"other","name":"c.zip","language":"FR","url":"/subtitle/1-3.zip"}]}`))
	}))
	defer srv.Close()
	ctx, diag := WithDiagnostics(context.Background())
	res, err := NewSubDLWithBaseURL("k", srv.URL, srv.URL).Find(ctx, Query{TMDBID: 5, Language: "en"})
	if err != nil || len(res) != 1 || res[0].Release != "fits" {
		t.Fatalf("results: %+v %v", res, err)
	}
	lines := diag.List()
	if len(lines) != 2 || lines[0].Bare || !lines[1].Bare || lines[1].Count != 1 || lines[1].Found != 3 {
		t.Fatalf("lines: %+v", lines)
	}
}

func TestSubDLDownloadPath(t *testing.T) {
	for in, want := range map[string]string{
		"/subtitle/1-2.zip":                     "/subtitle/1-2.zip",
		" /subtitle/1-2.zip ":                   "/subtitle/1-2.zip",
		"https://dl.subdl.com/subtitle/1-2.zip": "/subtitle/1-2.zip",
		"https://elsewhere.example/x.zip":       "https://elsewhere.example/x.zip",
	} {
		if got := downloadPath(in); got != want {
			t.Errorf("downloadPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	got := redactSecrets(`Get "https://api.subdl.com/api/v1/subtitles?api_key=abc123&tmdb_id=1": dial tcp: no such host`)
	if strings.Contains(got, "abc123") || !strings.Contains(got, "tmdb_id=1") {
		t.Fatalf("redacted: %s", got)
	}
}
