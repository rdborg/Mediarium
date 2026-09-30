package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorScrubberHidesServerErrorsFromMembers(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusInternalServerError, `SELECT * FROM movies: open /config/app.db: disk I/O error (key=SECRET)`)
	}
	rec := httptest.NewRecorder()
	handler(&errorScrubber{ResponseWriter: rec, r: httptest.NewRequest("GET", "/api/movies", nil)}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "SECRET") || strings.Contains(body, "/config") || !strings.Contains(body, "Something went wrong") {
		t.Fatalf("member saw: %s", body)
	}
}

func TestErrorScrubberLeavesClientErrorsAndSuccessAlone(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &errorScrubber{ResponseWriter: rec, r: httptest.NewRequest("GET", "/x", nil)}
	writeError(w, http.StatusBadRequest, "invalid movie id")
	if !strings.Contains(rec.Body.String(), "invalid movie id") {
		t.Fatalf("400 message was changed: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	w = &errorScrubber{ResponseWriter: rec, r: httptest.NewRequest("GET", "/x", nil)}
	writeJSON(w, http.StatusOK, map[string]string{"error": "not really an error"})
	if !strings.Contains(rec.Body.String(), "not really") {
		t.Fatalf("200 body was changed: %s", rec.Body.String())
	}
}
