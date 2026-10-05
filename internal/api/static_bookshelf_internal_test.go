package api

import (
	"strings"
	"testing"
)

func TestBookshelfPageHasItsOwnManifest(t *testing.T) {
	page := []byte(`<html><head><title>Mediarium</title><link rel="manifest" href="/manifest.webmanifest" /></head></html>`)
	got := string(bookshelfPage(page))
	if !strings.Contains(got, `href="/bookshelf.webmanifest"`) || strings.Contains(got, `/manifest.webmanifest`) || !strings.Contains(got, "<title>Mediarium Books</title>") {
		t.Fatalf("bookshelf page: %s", got)
	}
}
