package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Search engines are asked to keep out, in the file they look for and in a
// header on every answer.
func TestSearchEnginesAreAskedToStayAway(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	resp, err := http.Get(base + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") ||
		!strings.Contains(string(body), "Disallow: /") {
		t.Fatalf("robots.txt: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if got := resp.Header.Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Fatalf("X-Robots-Tag = %q", got)
	}
}
