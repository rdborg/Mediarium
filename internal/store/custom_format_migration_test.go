package store_test

import (
	"strings"
	"testing"

	migrations "github.com/rdborg/mediarium/db"
)

// Migration 0280 rebuilds the custom formats table so a name is unique per app.
// What was there stays, with its ids, and the same name for the other app now fits.
func TestCustomFormatMigrationKeepsFormatsAndAllowsOneNamePerApp(t *testing.T) {
	sqlBytes, err := migrations.MigrationsFS.ReadFile("migrations/0280_recyclarr_fields.sql")
	if err != nil {
		t.Fatal(err)
	}
	// The rebuild is the first half of the file; the profile column is added
	// by the second half, which a database that is already migrated has.
	rebuild, _, ok := strings.Cut(string(sqlBytes), "ALTER TABLE quality_profiles")
	if !ok {
		t.Fatal("the migration changed shape")
	}
	db := openTemp(t)
	mustExec(t, db, `DROP TABLE custom_formats`)
	mustExec(t, db, `CREATE TABLE custom_formats (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE COLLATE NOCASE, app TEXT NOT NULL DEFAULT 'radarr',
		trash_id TEXT NOT NULL DEFAULT '', specs TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), trash_score INTEGER)`)
	mustExec(t, db, `INSERT INTO custom_formats (id, name, app, trash_id, specs, trash_score) VALUES (7, 'HDR', 'radarr', 'abc', '[]', 0)`)
	mustExec(t, db, `INSERT INTO custom_formats (id, name, app, trash_id, specs, trash_score) VALUES (12, 'Mine', 'sonarr', '', '[{"name":"x"}]', NULL)`)

	if _, err := db.Exec(rebuild); err != nil {
		t.Fatalf("run migration: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM custom_formats WHERE (id = 7 AND name = 'HDR' AND trash_id = 'abc' AND trash_score = 0 AND include_in_name = 0) OR (id = 12 AND app = 'sonarr' AND trash_score IS NULL AND specs = '[{"name":"x"}]')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("the formats after the rebuild: %d of 2 (%v)", n, err)
	}
	mustExec(t, db, `INSERT INTO custom_formats (name, app, specs) VALUES ('hdr', 'sonarr', '[]')`)
	if _, err := db.Exec(`INSERT INTO custom_formats (name, app, specs) VALUES ('HDR', 'radarr', '[]')`); err == nil {
		t.Errorf("a second Radarr HDR should still be refused")
	}
	var next int
	if err := db.QueryRow(`SELECT MAX(id) FROM custom_formats`).Scan(&next); err != nil || next <= 12 {
		t.Errorf("new formats number on from the old ones, got %d (%v)", next, err)
	}
}
