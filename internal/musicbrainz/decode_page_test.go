package musicbrainz

import (
	"errors"
	"testing"
)

// A maintenance page sent with status 200 is "the music database is down", not
// a JSON syntax error, and it is not remembered.
func TestAWebPageInsteadOfDataIsUnavailable(t *testing.T) {
	var out struct{}
	for _, body := range []string{"<html><body>We'll be back</body></html>", "  \n<!DOCTYPE html><html></html>"} {
		err := decode([]byte(body), &out, "/ws/2/artist")
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%q: %v", body, err)
		}
	}
	if err := decode([]byte(`{"x":`), &out, "/ws/2/artist"); err == nil || errors.Is(err, ErrUnavailable) {
		t.Errorf("a cut-off answer: %v", err)
	}
}
