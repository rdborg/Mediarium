package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/store"
)

func TestASlowStatementIsInTheProblemLog(t *testing.T) {
	db := openTemp(t)
	l := problems.Open(db)
	problems.SetDefault(l)
	t.Cleanup(func() {
		l.Flush()
		problems.SetDefault(nil)
	})
	store.SetSlowCallThreshold(20 * time.Millisecond)
	t.Cleanup(func() { store.SetSlowCallThreshold(store.SlowCallThreshold) })

	if _, err := db.Exec(`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 400000) SELECT COUNT(*) FROM c WHERE ? != ?`, "hunter2-secret", "other"); err != nil {
		t.Fatal(err)
	}
	l.Flush()
	rows, _, err := l.List(problems.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Code != problems.CodeDatabaseSlow || rows[0].Level != problems.LevelWarning {
		t.Fatalf("want one database.slow row, got %+v", rows)
	}
	if strings.Contains(rows[0].Detail+rows[0].Message, "hunter2") || !strings.Contains(rows[0].Detail, "WITH RECURSIVE") {
		t.Errorf("the detail names the statement without its values: %q", rows[0].Detail)
	}

	// The log's own statements are never reported, or a slow database would
	// fill the log with the log's own trouble.
	store.SetSlowCallThreshold(time.Nanosecond)
	l.Record(problems.Problem{Code: problems.CodeDiskFull})
	l.Flush()
	rows, _, _ = l.List(problems.Filter{Code: problems.CodeDatabaseSlow})
	for _, r := range rows {
		if strings.Contains(r.Detail, "problems") {
			t.Errorf("a statement on the problems table was reported: %q", r.Detail)
		}
	}
}
