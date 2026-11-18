package books

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Open Library is a free service that is sometimes slow, down for
// maintenance or asking for fewer requests. Each of those is said in words, and
// the ones worth trying again are marked so.
func TestOpenLibraryAnswersThatAreNotData(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantBusy bool
		wantText string
	}{
		{"too many requests", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "30"); w.WriteHeader(429) }, true, "busy"},
		{"unavailable", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }, true, "busy"},
		{"a web page with 200", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html><body>Maintenance</body></html>")) }, true, "web page"},
		{"nothing with 200", func(w http.ResponseWriter, r *http.Request) {}, false, "read the answer"},
		{"cut off", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"docs":[{"key":"/works/OL1W","ti`)) }, false, "read the answer"},
		{"slow", func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }, true, "in time"},
		{"huge", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"docs":[`))
			chunk := []byte(strings.Repeat(" ", 1<<20))
			for i := 0; i < 20; i++ {
				w.Write(chunk)
			}
		}, false, "larger"},
		{"plain error", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, false, "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			// Twenty megabytes have to arrive before the size limit is hit, which takes
			// longer than 100ms on a busy machine or with the race detector on.
			timeout := 100 * time.Millisecond
			if tt.name == "huge" {
				timeout = 30 * time.Second
			}
			c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: timeout}}
			_, err := c.Search(context.Background(), "dune", 5)
			if err == nil {
				t.Fatal("no error")
			}
			if got := errors.Is(err, ErrBusy); got != tt.wantBusy {
				t.Errorf("busy = %v, want %v (%v)", got, tt.wantBusy, err)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error %q does not say %q", err, tt.wantText)
			}
		})
	}
}
