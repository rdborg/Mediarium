package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeatureOfferAnswersAreRemembered(t *testing.T) {
	s := newBareServer(t)
	put := func(body string) (int, string) {
		w := httptest.NewRecorder()
		s.handlePutFeatureOffer(w, httptest.NewRequest(http.MethodPut, "/api/feature-offers", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	if code, _ := put(`{"id": "anime", "choice": "sometimes"}`); code != http.StatusBadRequest {
		t.Errorf("an unknown choice: %d", code)
	}
	if code, _ := put(`{"id": "Not An Id!", "choice": "never"}`); code != http.StatusBadRequest {
		t.Errorf("a bad id: %d", code)
	}
	if code, body := put(`{"id": "anime", "choice": "never"}`); code != http.StatusOK || !strings.Contains(body, `"anime":"never"`) {
		t.Errorf("never: %d %s", code, body)
	}
	if code, body := put(`{"id": "thetvdb", "choice": "later", "version": "4.0.0"}`); code != http.StatusOK || !strings.Contains(body, `"thetvdb":"later:4.0.0"`) {
		t.Errorf("later: %d %s", code, body)
	}
	if code, body := put(`{"id": "anime", "choice": "clear"}`); code != http.StatusOK || strings.Contains(body, `"anime"`) {
		t.Errorf("clear: %d %s", code, body)
	}
	if got := s.featureOfferChoices(); got["thetvdb"] != "later:4.0.0" || len(got) != 1 {
		t.Errorf("stored: %v", got)
	}
}
