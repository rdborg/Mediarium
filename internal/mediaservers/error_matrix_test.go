package mediaservers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every kind of media server, tested and asked to refresh while the server
// answers wrongly in each way a real one (or the proxy in front of it) does.
// The person reads the error: it must be in plain words, never "EOF" or a
// parser's text, and the token must never be in it.
func TestMediaServerErrorsAreMessagesNotParserText(t *testing.T) {
	const token = "tok-very-secret-1234567890"
	behaviours := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"401", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope "+token, 401) }},
		{"403", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "forbidden", 403) }},
		{"404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }},
		{"410", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", 410) }},
		{"429", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "slow", 429)
		}},
		{"500", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom "+token, 500) }},
		{"502", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad gateway", 502) }},
		{"503", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "starting", 503) }},
		{"a login page after a redirect", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/login" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<!doctype html><html><body><form>Sign in</form></body></html>"))
		}},
		{"an endless redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, r.URL.Path+"x", http.StatusFound) }},
		{"a web page with 200", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body>Welcome to nginx</body></html>"))
		}},
		{"nothing with 200", func(w http.ResponseWriter, r *http.Request) {}},
		{"json that is not theirs", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`["a","b"]`)) }},
		{"cut-off json", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"MediaContainer":{"Direc`)) }},
		{"plain text with 200", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }},
		{"slow", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}},
		{"a connection that drops", func(w http.ResponseWriter, r *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
		}},
	}
	kinds := []Kind{KindPlex, KindJellyfin, KindEmby, KindSilo, KindAudiobookshelf}
	for _, kind := range kinds {
		for _, b := range behaviours {
			t.Run(string(kind)+"/"+b.name, func(t *testing.T) {
				srv := httptest.NewServer(b.handler)
				defer srv.Close()
				client := NewClient("test")
				client.HTTP = &http.Client{Timeout: 300 * time.Millisecond}
				s := Server{ID: 1, Name: "Test", Kind: kind, BaseURL: srv.URL, Token: token, Enabled: true}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				check := func(what string, err error) {
					if err == nil {
						return
					}
					msg := err.Error()
					if strings.Contains(msg, token) {
						t.Errorf("%s: the token is in the error: %q", what, msg)
					}
					var ue *UserError
					if !errors.As(err, &ue) {
						t.Errorf("%s: not a message for a person: %q", what, msg)
						return
					}
					for _, bad := range []string{"EOF", "invalid character", "unexpected end", "cannot unmarshal", "XML syntax", "unrecognised response", "json:", "xml:"} {
						if strings.Contains(msg, bad) {
							t.Errorf("%s: parser text in the error: %q", what, msg)
						}
					}
					if strings.TrimSpace(msg) == "" {
						t.Errorf("%s: an empty error", what)
					}
				}
				_, err := client.Test(ctx, s)
				if err == nil && b.name != "plain text with 200" && b.name != "json that is not theirs" {
					// A server that answers nothing like itself must not pass the test.
					if !strings.Contains(b.name, "nothing with 200") {
						t.Errorf("Test passed on %q", b.name)
					}
				}
				check("Test", err)
				check("RefreshAll", client.RefreshAll(ctx, s))
				_, err = client.RefreshFolders(ctx, s, MediaMovie, []string{"/movies/Heat (1995)"})
				check("RefreshFolders", err)
			})
		}
	}
}
