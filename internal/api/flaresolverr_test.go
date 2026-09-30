package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeFlareSolverr(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantRunning bool
		wantVersion string
	}{
		{"ready", http.StatusOK, `{"msg":"FlareSolverr is ready!","version":"3.4.0"}`, true, "3.4.0"},
		{"not ready message", http.StatusOK, `{"msg":"starting","version":"3.4.0"}`, false, "3.4.0"},
		{"server error", http.StatusInternalServerError, `{"msg":"FlareSolverr is ready!"}`, false, ""},
		{"not json", http.StatusOK, `<html>hello</html>`, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			running, version := probeFlareSolverr(srv.URL)
			if running != tc.wantRunning || version != tc.wantVersion {
				t.Fatalf("got running=%v version=%q, want running=%v version=%q", running, version, tc.wantRunning, tc.wantVersion)
			}
		})
	}
}

func TestProbeFlareSolverrUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if running, _ := probeFlareSolverr(url); running {
		t.Fatal("a closed port must not count as running")
	}
}
