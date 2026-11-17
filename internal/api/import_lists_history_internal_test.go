package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/queue"
)

// A list's history counts the downloads it started by the list's id: a new
// name keeps the count, and two lists with the same name each count their own.
func TestListHistoryCountsDownloadsByList(t *testing.T) {
	s := newAuditServer(t)
	s.dispatch.Stop()
	newList := func(name string) importList {
		res, err := s.db.Exec(`INSERT INTO import_lists (name, source) VALUES (?, 'trakt')`, name)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return importList{ID: id, Name: name}
	}
	a, b := newList("Picks"), newList("Picks")
	n := 0
	enqueue := func(l importList, count int) {
		for i := 0; i < count; i++ {
			n++
			if _, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: fmt.Sprintf("Release %d", n), NZBURL: fmt.Sprintf("http://x.invalid/%d", n), Origin: l.origin()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	enqueue(a, 2)
	enqueue(b, 1)

	downloads := func(l importList) int {
		h, err := s.listHistory(l)
		if err != nil {
			t.Fatal(err)
		}
		return h.Downloads
	}
	if _, err := s.db.Exec(`UPDATE import_lists SET name = 'Weekend picks' WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	renamed := importList{ID: a.ID, Name: "Weekend picks"}
	tests := []struct {
		name string
		list importList
		want int
	}{
		{"a renamed list keeps its count", renamed, 2},
		{"the other list with the old name keeps its own", b, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloads(tt.list); got != tt.want {
				t.Fatalf("downloads = %d, want %d", got, tt.want)
			}
		})
	}
}

// Downloads and titles from before a list's id was kept are matched to the
// list by name, but only where a single list has that name.
func TestListIDBackfill(t *testing.T) {
	s := newAuditServer(t)
	for _, name := range []string{"Only one", "Twice", "Twice"} {
		if _, err := s.db.Exec(`INSERT INTO import_lists (name, source) VALUES (?, 'trakt')`, name); err != nil {
			t.Fatal(err)
		}
	}
	var only int64
	if err := s.db.QueryRow(`SELECT id FROM import_lists WHERE name = 'Only one'`).Scan(&only); err != nil {
		t.Fatal(err)
	}
	// Rows as they were before: the list's name only.
	for _, name := range []string{"Only one", "Twice", "Removed since"} {
		if _, err := s.db.Exec(`INSERT INTO download_queue (release_title, nzb_url, origin_kind, origin_name) VALUES (?, 'http://x.invalid', 'list', ?)`, name, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO download_queue (release_title, nzb_url, origin_kind, origin_name) VALUES ('Scheduled', 'http://x.invalid', ?, 'Only one')`, origin.Auto); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO movies (tmdb_id, title, origin_kind, origin_name) VALUES (1, 'A film', 'list', 'Only one')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO series (tmdb_id, title, origin_kind, origin_name) VALUES (2, 'A show', 'list', 'Twice')`); err != nil {
		t.Fatal(err)
	}

	sqlText, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0274_origin_list_id.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(sqlText), ";") {
		if body := stripSQLComments(stmt); strings.HasPrefix(body, "UPDATE") {
			if _, err := s.db.Exec(body); err != nil {
				t.Fatalf("%s: %v", body, err)
			}
		}
	}

	tests := []struct {
		name  string
		query string
		want  int64
	}{
		{"a download of the only list with the name", `SELECT COALESCE(origin_list_id, 0) FROM download_queue WHERE release_title = 'Only one'`, only},
		{"a name two lists share stays unmatched", `SELECT COALESCE(origin_list_id, 0) FROM download_queue WHERE release_title = 'Twice'`, 0},
		{"a list removed since stays unmatched", `SELECT COALESCE(origin_list_id, 0) FROM download_queue WHERE release_title = 'Removed since'`, 0},
		{"not from a list", `SELECT COALESCE(origin_list_id, 0) FROM download_queue WHERE release_title = 'Scheduled'`, 0},
		{"a film the list added", `SELECT COALESCE(origin_list_id, 0) FROM movies WHERE tmdb_id = 1`, only},
		{"a show of a shared name", `SELECT COALESCE(origin_list_id, 0) FROM series WHERE tmdb_id = 2`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int64
			if err := s.db.QueryRow(tt.query).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("list id = %d, want %d", got, tt.want)
			}
		})
	}
}

// stripSQLComments drops the -- comment lines of a statement and trims it.
func stripSQLComments(stmt string) string {
	var keep []string
	for _, line := range strings.Split(stmt, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			keep = append(keep, line)
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}
