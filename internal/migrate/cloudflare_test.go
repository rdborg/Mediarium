package migrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An address that goes through Cloudflare and answers 530 (the tunnel can't
// reach the app) says so, and points at the app's own address.
func TestReadExplainsCloudflareErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header map[string]string
		want   string
	}{
		{"530 from a tunnel", 530, nil, "behind Cloudflare"},
		{"522 timed out", 522, nil, "behind Cloudflare"},
		{"502 with a Cloudflare ray id", 502, map[string]string{"Cf-Ray": "8abc-AMS"}, "behind Cloudflare"},
		{"a plain 502 is just a 502", 502, nil, "answered 502"},
		{"a plain 500 is just a 500", 500, nil, "answered 500"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			var out map[string]any
			err := read(context.Background(), srv.Client(), Conn{URL: srv.URL, APIKey: "k"}, call{path: "/api/v3/system/status"}, &out)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
			if tc.want == "behind Cloudflare" && !strings.Contains(err.Error(), "on your own network") {
				t.Fatalf("err = %v, want advice to use the local address", err)
			}
		})
	}
}
