package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestSimulateProfileThroughAPI(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/quality-profiles", map[string]any{
		"name": "Only x265", "allowed": []string{"WEBDL-1080p", "Bluray-1080p"}, "cutoff": "Bluray-1080p",
		"mustContain": []string{"x265"}, "preferred": []map[string]any{{"term": "FuN", "score": 25}},
	}, http.StatusCreated)
	id := created["id"]
	url := httpSrv.URL + "/api/quality-profiles/simulate"

	tests := []struct {
		name    string
		body    map[string]any
		verdict string
		quality string
		reason  string // a piece of a rejection, "" for none
	}{
		{"taken", map[string]any{"title": "Some.Movie.2020.1080p.WEB-DL.x265-FuN", "profileId": id, "sizeMB": 4000}, "grab", "WEBDL-1080p", ""},
		{"missing a must-contain word", map[string]any{"title": "Some.Movie.2020.1080p.WEB-DL.x264-FuN", "profileId": id}, "reject", "WEBDL-1080p", "x265"},
		{"quality not allowed", map[string]any{"title": "Some.Movie.2020.720p.WEB-DL.x265-FuN", "profileId": id}, "reject", "WEBDL-720p", "isn't allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postJSON[map[string]any](t, client, url, tt.body, http.StatusOK)
			if got["verdict"] != tt.verdict {
				t.Errorf("verdict = %v, want %v: %+v", got["verdict"], tt.verdict, got)
			}
			if parsed, _ := got["parsed"].(map[string]any); parsed["quality"] != tt.quality {
				t.Errorf("quality = %v, want %v", parsed["quality"], tt.quality)
			}
			reasons, _ := got["reasons"].([]any)
			joined := ""
			for _, r := range reasons {
				joined += r.(map[string]any)["text"].(string) + "\n"
			}
			if tt.reason == "" && joined != "" {
				t.Errorf("no reason expected, got %q", joined)
			}
			if tt.reason != "" && !strings.Contains(joined, tt.reason) {
				t.Errorf("reasons %q lack %q", joined, tt.reason)
			}
		})
	}
	// The preferred term FuN adds its score.
	got := postJSON[map[string]any](t, client, url, map[string]any{"title": "Some.Movie.2020.1080p.WEB-DL.x265-FuN", "profileId": id}, http.StatusOK)
	if score, _ := got["score"].(float64); score != 25 {
		t.Errorf("score = %v, want 25", got["score"])
	}
	postJSON[map[string]any](t, client, url, map[string]any{"title": "  "}, http.StatusBadRequest)
}
