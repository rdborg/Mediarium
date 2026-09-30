package subtitles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// FuzzParseDownloadBody: whatever OpenSubtitles (or anything pretending to be
// it) answers, a reset time is a sane moment and never wraps around.
func FuzzParseDownloadBody(f *testing.F) {
	f.Add([]byte(`{"link":"https://dl.example/x.srt","requests":5,"remaining":15,"reset_time":"23 hours and 59 minutes","reset_time_utc":"2026-01-02T03:04:05.000Z"}`))
	f.Add([]byte(`{"reset_time":"99999999999999999999 hours 99999999999999999 minutes"}`))
	f.Add([]byte(`{"remaining":-1,"requests":9223372036854775807,"link":null}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, body []byte) {
		observed := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
		info, err := parseDownloadBody(body, observed)
		if err != nil || info.ResetAt.IsZero() {
			return
		}
		_ = info.ResetAt.Format(time.RFC3339)
	})
}

func FuzzParseResetDuration(f *testing.F) {
	for _, s := range []string{"23 hours and 59 minutes", "12 minutes", "1h 5m", "", "99999999999999999999999 hours", "-5 hours", "hours", "0 minutes", strings.Repeat("9", 400) + " m"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, ok := parseResetDuration(s)
		if ok && (d < 0 || d > 2*maxResetHours*time.Hour) {
			t.Fatalf("parseResetDuration(%q) = %v", s, d)
		}
	})
}

func TestDownloadFileHidesTheLinkKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) }))
	srv.Close() // nothing listens: the connection fails and the error names the address
	c := NewWithBaseURL("key", srv.URL)
	_, err := c.DownloadFile(context.Background(), srv.URL+"/dl/x.srt?token=SECRET123")
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("error = %v", err)
	}
	for _, link := range []string{"file:///etc/passwd", "ftp://example.org/x", "javascript:alert(1)", "//example.org/x", ""} {
		if _, err := c.DownloadFile(context.Background(), link); err == nil || !strings.Contains(err.Error(), "not a web address") {
			t.Errorf("DownloadFile(%q) = %v", link, err)
		}
	}
}

func TestResetTimeCannotWrapAround(t *testing.T) {
	observed := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	info, err := parseDownloadBody([]byte(`{"reset_time":"99999999999999999999 hours and 5 minutes"}`), observed)
	if err != nil {
		t.Fatal(err)
	}
	if d := info.ResetAt.Sub(observed); d <= 0 || d > 8*24*time.Hour {
		t.Fatalf("reset in %v", d)
	}
}

// FuzzDetectLanguage: any file path gives a canonical language code or none,
// so the name a subtitle is copied under is built only from known codes.
func FuzzDetectLanguage(f *testing.F) {
	for _, s := range []string{"Movie.en.srt", "Subs/English/forced.srt", "Show.S01E02/2_English.SDH.srt", "pt-BR.srt", "../../etc/passwd.srt", "a/b/c/d/e.srt", "", ".", "/", "\x00.srt", "\xff\xfe.en.srt", "Movie.2020.FRENCH.1080p.srt", "..en..srt", "en.forced.hi.srt"} {
		f.Add(s)
	}
	canonical := map[string]bool{}
	for _, l := range languageNames {
		canonical[l.code] = true
	}
	f.Fuzz(func(t *testing.T, rel string) {
		d, ok := DetectLanguage(rel)
		if ok && !canonical[d.Lang] {
			t.Fatalf("DetectLanguage(%q) = %q, which is not a known code", rel, d.Lang)
		}
		if ok {
			sc := Sidecar{Lang: d.Lang, Forced: d.Forced, SDH: d.SDH}
			if suffix := sc.suffix(); strings.ContainsAny(suffix, "/\\ \x00") || strings.Contains(suffix, "..") {
				t.Fatalf("suffix %q", suffix)
			}
		}
		_ = LanguageSatisfied(map[string]bool{"en": true}, rel)
	})
}
