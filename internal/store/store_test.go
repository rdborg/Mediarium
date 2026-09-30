package store_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

func openTemp(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenUsesWALWithASmallPool(t *testing.T) {
	db := openTemp(t)
	if in := store.Mode(db); in.JournalMode != store.ModeWAL || in.WALProblem != "" {
		t.Fatalf("mode = %+v, want WAL", in)
	}
	if got := db.Stats().MaxOpenConnections; got < 2 {
		t.Errorf("pool size = %d, want several connections", got)
	}
	// The pragmas apply to every connection in the pool, not just the first:
	// hold several open at once and ask each.
	var conns []*sql.Conn
	for i := 0; i < 4; i++ {
		c, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i, c := range conns {
		for pragma, want := range map[string]string{
			"journal_mode": "wal",
			"synchronous":  "1", // NORMAL
			"busy_timeout": "10000",
			"foreign_keys": "1",
		} {
			var got string
			if err := c.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("conn %d: pragma %s: %v", i, pragma, err)
			}
			if got != want {
				t.Errorf("conn %d: %s = %q, want %q", i, pragma, got, want)
			}
		}
	}
}

func TestFallsBackToOneConnectionWhenWALIsNotAvailable(t *testing.T) {
	// An in-memory database cannot use WAL, which stands in for a file system
	// that cannot.
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	in := store.Mode(db)
	if in.JournalMode != store.ModeFallback || in.WALProblem == "" {
		t.Fatalf("mode = %+v, want the fallback with a reason", in)
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("fallback pool size = %d, want 1", got)
	}
	// Still a working, migrated database.
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A reader must never wait for a writer: this is what stalled every page
// while a download was saving its progress.
func TestReadersAreNotBlockedByAnOpenWriteTransaction(t *testing.T) {
	db := openTemp(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('held', 'x')`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		var n int
		done <- db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&n)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read while a write transaction is open: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a read waited for an open write transaction")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Writers queue for the write lock instead of failing with "database is locked".
func TestConcurrentWritersAllSucceed(t *testing.T) {
	db := openTemp(t)
	const writers, each = 12, 25
	var wg sync.WaitGroup
	var failed atomic.Int64
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				tx, err := db.Begin()
				if err != nil {
					failed.Add(1)
					t.Errorf("begin: %v", err)
					return
				}
				if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "k"+strings.Repeat("x", w)+string(rune('a'+i)), "v"); err != nil {
					tx.Rollback()
					failed.Add(1)
					t.Errorf("write: %v", err)
					return
				}
				if err := tx.Commit(); err != nil {
					failed.Add(1)
					t.Errorf("commit: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key LIKE 'k%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if failed.Load() != 0 || n != writers*each {
		t.Fatalf("%d writes failed, %d rows stored, want %d", failed.Load(), n, writers*each)
	}
}

func TestSlowStatementsAreReportedWithoutTheirValues(t *testing.T) {
	db := openTemp(t)
	store.SetSlowCallThreshold(20 * time.Millisecond)
	t.Cleanup(func() { store.SetSlowCallThreshold(store.SlowCallThreshold) })

	var mu sync.Mutex
	var seen []string
	store.OnSlowCall(func(sql string, took time.Duration) {
		mu.Lock()
		seen = append(seen, sql)
		mu.Unlock()
	})
	t.Cleanup(func() { store.OnSlowCall(nil) })

	if _, err := db.Exec(`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 400000) SELECT COUNT(*) FROM c WHERE ? != ?`, "hunter2-secret", "other"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(seen, " | ")
	if len(seen) == 0 {
		t.Fatal("the slow statement was not reported")
	}
	if strings.Contains(joined, "hunter2") {
		t.Fatalf("a value leaked into the slow call report: %q", joined)
	}
	if !strings.Contains(joined, "WITH RECURSIVE") {
		t.Errorf("report should name the statement, got %q", joined)
	}
}
