package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrorSpeaksPlainlyOnServerErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		want    string // "" means: unchanged
		notWant []string
	}{
		{"sentence stays", http.StatusInternalServerError, "Couldn't save that. Try again.", "", nil},
		{"dns", http.StatusBadGateway, `Get "https://x.example/api?apikey=SECRET": dial tcp: lookup x.example: no such host`,
			"The address could not be found. Check the address and your internet connection.", []string{"SECRET", "dial tcp", "lookup"}},
		{"sqlite", http.StatusInternalServerError, "list movies: sqlite: database is locked", "Something went wrong inside Mediarium.", []string{"sqlite"}},
		{"a 400 is written for people already", http.StatusBadRequest, "sql: not a real validation message", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, tt.status, tt.message)
			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			got := body["error"]
			if tt.want == "" && got != tt.message {
				t.Fatalf("message changed: %q", got)
			}
			if tt.want != "" && !strings.HasPrefix(got, tt.want) {
				t.Fatalf("got %q, want prefix %q", got, tt.want)
			}
			for _, bad := range tt.notWant {
				if strings.Contains(got, bad) {
					t.Fatalf("%q still contains %q", got, bad)
				}
			}
		})
	}
}
