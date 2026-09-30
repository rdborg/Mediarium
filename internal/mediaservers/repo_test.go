package mediaservers_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/store"
)

func TestRepoStoresTokenEncrypted(t *testing.T) {
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
	repo := mediaservers.NewRepo(db, box)

	created, err := repo.Create(mediaservers.Server{
		Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: "http://plex:32400", Token: "super-secret-token",
		Enabled: true, RefreshAfterImport: true, PathMap: []mediaservers.PathMapping{{From: "/movies", To: "/data/movies"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT token_encrypted FROM media_servers WHERE id = ?`, created.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || strings.Contains(raw, "super-secret-token") {
		t.Fatalf("token must be stored encrypted, got %q", raw)
	}
	if created.Token != "super-secret-token" || len(created.PathMap) != 1 || !created.RefreshAfterImport {
		t.Fatalf("round trip: %+v", created)
	}

	// A good test stores the server id; a failure is remembered until the next success.
	if err := repo.RecordCheck(created.ID, "machine-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordCheck(created.ID, "", errors.New("Plex refused the token.")); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerID != "machine-1" || got.LastError != "Plex refused the token." || got.LastCheckedAt.IsZero() {
		t.Fatalf("after checks: %+v", got)
	}

	// Renaming keeps the check; a new address clears it.
	got.Name = "Living room"
	got, err = repo.Update(got)
	if err != nil || got.ServerID != "machine-1" || got.LastError == "" {
		t.Fatalf("rename: %+v %v", got, err)
	}
	got.BaseURL = "http://10.0.0.2:32400"
	got, err = repo.Update(got)
	if err != nil || got.ServerID != "" || got.LastError != "" || !got.LastCheckedAt.IsZero() {
		t.Fatalf("new address: %+v %v", got, err)
	}

	list, err := repo.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := repo.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := repo.Get(created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("get deleted: %v", err)
	}
}
