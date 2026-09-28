package indexers_test

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/indexers"
)

const fixtureDefinition = `
id: fixtureindexer
name: Fixture Indexer
type: private
links:
  - https://fixture.test/
caps:
  categories:
    2000: Movies
    2010: Movies/SD
`

func TestParseDefinition(t *testing.T) {
	def, err := indexers.ParseDefinition([]byte(fixtureDefinition))
	if err != nil {
		t.Fatalf("parse definition: %v", err)
	}
	if def.ID != "fixtureindexer" {
		t.Errorf("unexpected id: %s", def.ID)
	}
	if def.Name != "Fixture Indexer" {
		t.Errorf("unexpected name: %s", def.Name)
	}
	if def.Type != "private" {
		t.Errorf("unexpected type: %s", def.Type)
	}
	if len(def.Links) != 1 || def.Links[0] != "https://fixture.test/" {
		t.Errorf("unexpected links: %v", def.Links)
	}
	if def.Caps.Categories["2000"] != "Movies" {
		t.Errorf("unexpected categories: %v", def.Caps.Categories)
	}
}

func TestParseDefinitionMissingID(t *testing.T) {
	_, err := indexers.ParseDefinition([]byte(`name: No ID Here`))
	if err == nil {
		t.Fatal("expected error for definition missing id")
	}
}
