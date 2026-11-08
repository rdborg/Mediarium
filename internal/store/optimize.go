package store

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// Optimize lets SQLite gather the table statistics its query planner uses
// (PRAGMA optimize). Without them a big library can get slow plans for the
// lists and the clean-up queries. analysis_limit keeps each table's look
// short, so it takes a moment even on a large database. first is true for the
// run right after start-up, which also looks at tables that were never
// analysed (0x10002, as SQLite advises for long-running programs); the later
// runs only redo what has changed a lot.
func Optimize(ctx context.Context, db *sql.DB, first bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx) // analysis_limit is per connection: keep this one
	if err != nil {
		return
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA analysis_limit = 400`); err != nil {
		slog.Warn("database: optimize", "err", err)
		return
	}
	stmt := `PRAGMA optimize`
	if first {
		stmt = `PRAGMA optimize = 0x10002`
	}
	start := time.Now()
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		slog.Warn("database: optimize", "err", err)
		return
	}
	if d := time.Since(start); d > 5*time.Second {
		slog.Info("database: optimize took a while", "took", d.Round(time.Second))
	}
}
