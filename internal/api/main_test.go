package api_test

import (
	"os"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
)

// TestMain keeps the tests off the internet: lookups that would go to
// Audible and Audnexus go to a closed local port and fail straight away.
func TestMain(m *testing.M) {
	books.AudibleSearchURL = "http://127.0.0.1:1/catalog/products"
	books.AudnexusURL = "http://127.0.0.1:1"
	os.Exit(m.Run())
}
