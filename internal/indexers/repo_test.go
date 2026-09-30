package indexers_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/store"
)

func newTestRepo(t *testing.T) *indexers.Repo {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	return indexers.NewRepo(db, box)
}

func TestRepoCreateListDelete(t *testing.T) {
	repo := newTestRepo(t)

	created, err := repo.Create(indexers.Instance{
		Name:         "Fixture Indexer",
		DefinitionID: "fixtureindexer",
		BaseURL:      "https://fixture.test",
		APIKey:       "super-secret-key",
		Enabled:      true,
	}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id")
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 indexer, got %d", len(list))
	}
	if list[0].APIKey != "super-secret-key" {
		t.Fatalf("expected decrypted api key round-trip, got %q", list[0].APIKey)
	}

	if err := repo.SetEnabled(created.ID, false); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	list, _ = repo.List()
	if list[0].Enabled {
		t.Fatal("expected indexer to be disabled")
	}

	if err := repo.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 indexers after delete, got %d", len(list))
	}
}
