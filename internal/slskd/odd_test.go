package slskd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A proxy's login page, or another program on the port, answers 200 with a web
// page; the key is never in an error text; Soulseek paths with either slash
// give the right file name.
func TestSlskdAnswersThatAreNotItsAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<!doctype html><html><body>Sign in</body></html>"))
	}))
	defer srv.Close()
	c := New(srv.URL, "secret-key-123")
	_, err := c.Version(context.Background())
	if !errors.Is(err, ErrNotSlskd) {
		t.Fatalf("Version: %v", err)
	}
	if strings.Contains(err.Error(), "secret-key-123") {
		t.Errorf("the key is in the error: %v", err)
	}
}

func TestSoulseekPaths(t *testing.T) {
	tests := []struct{ path, base, dir string }{
		{`@@abc\Music\AC_DC\Back in Black\01 - Hells Bells.flac`, "01 - Hells Bells.flac", `@@abc\Music\AC_DC\Back in Black`},
		{`/music/Artist/Album/01.mp3`, "01.mp3", "/music/Artist/Album"},
		{`mixed\slashes/02 Song.mp3`, "02 Song.mp3", `mixed\slashes`},
		{`just.mp3`, "just.mp3", ""},
		{`dir\`, "", "dir"},
		{``, "", ""},
		{`\server\share\é日本語 🎵.flac`, "é日本語 🎵.flac", `\server\share`},
		{`a\..\..\evil.flac`, "evil.flac", `a\..\..`},
	}
	for _, tt := range tests {
		if got := BaseName(tt.path); got != tt.base {
			t.Errorf("BaseName(%q) = %q, want %q", tt.path, got, tt.base)
		}
		if got := Dir(tt.path); got != tt.dir {
			t.Errorf("Dir(%q) = %q, want %q", tt.path, got, tt.dir)
		}
		if strings.ContainsAny(BaseName(tt.path), `\/`) {
			t.Errorf("BaseName(%q) still has a separator", tt.path)
		}
	}
}
