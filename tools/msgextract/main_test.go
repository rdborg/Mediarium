package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The problem catalog is a list of positional Help literals: its headings
// and button labels don't end like a sentence but are shown all the same.
func TestScanFindsTheProblemCatalogText(t *testing.T) {
	src := `package problems

type Help struct{ Code, Level, Area, Title, Explain, Try, LinkLabel, LinkPath string }

var catalog = []Help{
	{"database.slow", "warning", "database",
		"The database is slow",
		"A database call took several seconds.",
		"Keep the settings folder on a local disk.",
		"Open System settings", "/settings/system"},
}

var notShown = []string{"some heading without a full stop"}
`
	path := filepath.Join(t.TempDir(), "catalog.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	scan(path, found)
	for _, want := range []string{"The database is slow", "A database call took several seconds.", "Keep the settings folder on a local disk.", "Open System settings"} {
		if !found[want] {
			t.Errorf("missing %q in %v", want, found)
		}
	}
	if found["some heading without a full stop"] || found["/settings/system"] {
		t.Errorf("found text that isn't shown: %v", found)
	}
}

// A piece of a query built up in Go is not a sentence for the screen.
func TestQueryPiecesAreNotSentences(t *testing.T) {
	for _, s := range []string{"AND s.id = ?", "WHERE i.reported_by = ?", "ORDER BY added DESC, id"} {
		if text, ok := template(s, true); ok {
			t.Errorf("%q was taken as a sentence: %q", s, text)
		}
	}
	if _, ok := template("And then it was done.", false); !ok {
		t.Error("a normal sentence starting with And must still count")
	}
}

// A line of the activity feed made with feedLine is kept, whatever it ends with.
func TestScanFindsFeedLines(t *testing.T) {
	src := `package api

func line(title string, only bool) string {
	if only {
		return feedLine("Blocklisted \"%s\": by you (only for this title)", title)
	}
	return feedLine("Blocklisted \"%s\": by you", title)
}
`
	path := filepath.Join(t.TempDir(), "feed.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	scan(path, found)
	for _, want := range []string{`Blocklisted "{0}": by you (only for this title)`, `Blocklisted "{0}": by you`} {
		if !found[want] {
			t.Errorf("missing %q in %v", want, found)
		}
	}
}
