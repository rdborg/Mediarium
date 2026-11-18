package indexers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func flareDown(site, addr string) error {
	return &FlareSolverrError{Site: site, Address: SafeAddress(addr), Err: &flareContactError{err: fmt.Errorf(`Post "%s/v1": EOF`, addr)}}
}

func TestProblemMessageExplainsFlareSolverrAndGonePages(t *testing.T) {
	gone410 := NewStatusError(410, http.Header{}, "52BT: the site returned HTTP 410 for a search", time.Now())
	gone404 := NewStatusError(404, http.Header{}, "Site: the site returned HTTP 404 for a search", time.Now())
	tests := []struct {
		name       string
		err        error
		want       string
		wantDetail string
	}{
		{
			"FlareSolverr not running, with its address",
			flareDown("1337x", "http://127.0.0.1:8191"),
			"FlareSolverr isn't running or Mediarium can't reach it at http://127.0.0.1:8191. Start FlareSolverr, or set its address under Settings > Finding releases > Indexers & Search.",
			`1337x: FlareSolverr could not pass the Cloudflare check: contact FlareSolverr: Post "http://127.0.0.1:8191/v1": EOF`,
		},
		{
			"a login in the saved address is never printed",
			flareDown("kickasstorrents.to", "http://user:secret@flare.lan:8191/?key=abc"),
			"FlareSolverr isn't running or Mediarium can't reach it at http://flare.lan:8191. Start FlareSolverr, or set its address under Settings > Finding releases > Indexers & Search.",
			"",
		},
		{
			"FlareSolverr answered but could not pass the check",
			&FlareSolverrError{Site: "Site", Address: "http://flare:8191", Err: errors.New("Challenge not detected")},
			"Site returned an error.",
			"",
		},
		{
			"HTTP 410",
			gone410,
			"This site says the page is gone (HTTP 410). The site may have moved or closed. Check its address, or turn it off.",
			"52BT: the site returned HTTP 410 for a search",
		},
		{
			"HTTP 404",
			gone404,
			"This site says the page is gone (HTTP 404). The site may have moved or closed. Check its address, or turn it off.",
			"Site: the site returned HTTP 404 for a search",
		},
		{
			"another status keeps the old words",
			NewStatusError(500, http.Header{}, "Site: HTTP 500", time.Now()),
			"Site returned an error.",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := Outcome{IndexerName: "Site", Err: tt.err}
			got := ProblemMessage(o)
			if got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "secret") || strings.Contains(got, "abc") {
				t.Errorf("message shows a secret: %q", got)
			}
			if detail := ProblemDetail(o); detail != tt.wantDetail && tt.wantDetail != "" {
				t.Errorf("detail = %q, want %q", detail, tt.wantDetail)
			}
			if tt.want == "Site returned an error." && ProblemDetail(o) != "" {
				t.Errorf("an unexplained failure has no detail line, got %q", ProblemDetail(o))
			}
		})
	}
}

func TestFailureReasonNamesFlareSolverrAndGonePages(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{flareDown("A", "http://127.0.0.1:8191"), "FlareSolverr isn't running or can't be reached"},
		{NewStatusError(410, http.Header{}, "x", time.Now()), "the site says the page is gone (HTTP 410)"},
		{NewStatusError(404, http.Header{}, "x", time.Now()), "the site says the page is gone (HTTP 404)"},
		{NewStatusError(503, http.Header{}, "x", time.Now()), "it answered with an error (HTTP 503)"},
	}
	for _, tt := range tests {
		if got := failureReason(tt.err); got != tt.want {
			t.Errorf("failureReason(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestSafeAddress(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:8191":       "http://127.0.0.1:8191",
		"http://flaresolverr:8191/v1": "http://flaresolverr:8191",
		" http://u:p@host:1/?k=v ":    "http://host:1",
		"":                            "",
		"not a url":                   "",
	} {
		if got := SafeAddress(in); got != want {
			t.Errorf("SafeAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
