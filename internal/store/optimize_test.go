package store

import (
	"context"
	"path/filepath"
	"testing"
)

// After the first optimize the query planner has statistics for an indexed
// table (sqlite_stat1), so lists over a big library get good plans.
func TestOptimizeGathersStatistics(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{`CREATE TABLE t (a INTEGER, b TEXT)`, `CREATE INDEX t_a ON t(a)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 500; i++ {
		if _, err := db.Exec(`INSERT INTO t (a, b) VALUES (?, 'x')`, i%7); err != nil {
			t.Fatal(err)
		}
	}
	Optimize(context.Background(), db, true)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_stat1 WHERE tbl = 't'`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("no statistics for the table after optimize (rows %d, err %v)", n, err)
	}
}
