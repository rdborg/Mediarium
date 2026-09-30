package store_test

import (
	"testing"

	migrations "github.com/rdborg/mediarium/db"
)

// Migration 0026 sets the subtitles switch on once, for an install that was
// already using subtitles, and never overrides a choice that was made.
func TestSubtitlesSwitchMigration(t *testing.T) {
	sqlBytes, err := migrations.MigrationsFS.ReadFile("migrations/0026_subtitles_switch.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	cases := []struct {
		name string
		seed []string // statements that set up an install as it was before
		want string   // the switch afterwards: "" means unset (which reads as off)
	}{
		{"fresh install", nil, ""},
		{"own key saved",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.opensubtitles_api_key', 'ciphertext', 1)`}, "1"},
		{"empty key",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.opensubtitles_api_key', '', 0)`}, ""},
		{"account saved",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.opensubtitles_username', 'ryan', 0)`}, "1"},
		{"automatic downloading on",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.auto_download', '1', 0)`}, "1"},
		{"automatic downloading only ever off",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.auto_download', '0', 0)`}, ""},
		{"only languages chosen",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.languages', 'en,fr', 0)`}, ""},
		{"a subtitle was downloaded",
			[]string{`INSERT INTO subtitle_downloads (downloaded_at) VALUES ('2026-01-01T00:00:00Z')`}, "1"},
		{"a subtitle was looked for",
			[]string{`INSERT INTO subtitle_attempts (kind, media_id, language) VALUES ('movie', 1, 'en')`}, "1"},
		{"a title was marked as not needing subtitles",
			[]string{`INSERT INTO subtitle_dismissed (kind, media_id) VALUES ('movie', 1)`}, "1"},
		{"activity says a subtitle was downloaded",
			[]string{`INSERT INTO activity (event_type, message) VALUES ('subtitle', 'Downloaded en subtitle for Alien')`}, "1"},
		{"activity only says subtitles came with a download",
			[]string{`INSERT INTO activity (event_type, message) VALUES ('subtitle', 'Alien imported with 2 subtitle files: en, fr')`}, ""},
		{"switched off on purpose stays off",
			[]string{
				`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.opensubtitles_api_key', 'ciphertext', 1)`,
				`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.enabled', '0', 0)`,
			}, "0"},
		{"switched on stays on",
			[]string{`INSERT INTO settings (key, value, encrypted) VALUES ('subtitles.enabled', '1', 0)`}, "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Opening the database already ran the migration on an empty one;
			// clear what it did and run it again against the seeded install.
			db := openTemp(t)
			if _, err := db.Exec(`DELETE FROM settings WHERE key = 'subtitles.enabled'`); err != nil {
				t.Fatal(err)
			}
			for _, stmt := range tc.seed {
				if _, err := db.Exec(stmt); err != nil {
					t.Fatalf("seed %q: %v", stmt, err)
				}
			}
			if _, err := db.Exec(string(sqlBytes)); err != nil {
				t.Fatalf("run migration: %v", err)
			}
			var got string
			switch err := db.QueryRow(`SELECT value FROM settings WHERE key = 'subtitles.enabled'`).Scan(&got); {
			case err != nil && tc.want == "":
				// unset, as wanted
			case err != nil:
				t.Fatalf("read the switch: %v", err)
			}
			if got != tc.want {
				t.Fatalf("subtitles.enabled = %q, want %q", got, tc.want)
			}
		})
	}
}

// A new install goes through the migration with nothing in it, so the switch
// stays unset and subtitles are off.
func TestFreshInstallLeavesTheSubtitlesSwitchUnset(t *testing.T) {
	db := openTemp(t)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'subtitles.enabled'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a fresh install has the subtitles switch set (%d rows)", n)
	}
}
