package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"modernc.org/sqlite"
)

// driverName is the SQLite driver with call timing added. It behaves exactly
// like the plain "sqlite" driver; it only measures how long each statement
// takes, so a statement stuck behind a lock is reported with its SQL instead
// of just making the page hang.
const driverName = "mediarium-sqlite"

func init() {
	sql.Register(driverName, &watchedDriver{inner: &sqlite.Driver{}})
}

// slowCall is how long a statement may take before it is logged.
var slowCall atomic.Int64

// SlowCallThreshold is what a statement or a wait for a free connection may
// take before it is logged.
const SlowCallThreshold = 3 * time.Second

func init() { slowCall.Store(int64(SlowCallThreshold)) }

// SetSlowCallThreshold changes the logging threshold (tests use a short one).
func SetSlowCallThreshold(d time.Duration) { slowCall.Store(int64(d)) }

// slowNotifier is told about every slow statement (see OnSlowCall).
var slowNotifier atomic.Pointer[func(sql string, took time.Duration)]

// OnSlowCall registers fn to hear about every statement that exceeded the
// threshold, in addition to the log line. Used by tests; pass nil to remove.
func OnSlowCall(fn func(sql string, took time.Duration)) {
	if fn == nil {
		slowNotifier.Store(nil)
		return
	}
	slowNotifier.Store(&fn)
}

// sqlName shortens a statement to something safe and short to log: the first
// words of the SQL, never any argument values.
func sqlName(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 90 {
		q = q[:90] + "..."
	}
	return q
}

func noteCall(query string, started time.Time) {
	took := time.Since(started)
	if took < time.Duration(slowCall.Load()) {
		return
	}
	log.Printf("database: slow call took=%s sql=%q", took.Round(time.Millisecond), sqlName(query))
	// The problem log's own statements are left out, or a slow database would
	// fill the log with the log's own trouble.
	if !problems.Saving() && !strings.Contains(query, "problems") {
		problems.Record(problems.Problem{
			Code:    problems.CodeDatabaseSlow,
			Message: fmt.Sprintf("A database call took %s.", took.Round(100*time.Millisecond)),
			Detail:  fmt.Sprintf("took %s: %s", took.Round(time.Millisecond), sqlName(query)),
		})
	}
	if fn := slowNotifier.Load(); fn != nil {
		(*fn)(sqlName(query), took)
	}
}

type watchedDriver struct{ inner driver.Driver }

func (d *watchedDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &watchedConn{inner: c}, nil
}

type watchedConn struct{ inner driver.Conn }

func (c *watchedConn) Prepare(query string) (driver.Stmt, error) {
	s, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &watchedStmt{inner: s, query: query}, nil
}

func (c *watchedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		s, err := p.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &watchedStmt{inner: s, query: query}, nil
	}
	return c.Prepare(query)
}

func (c *watchedConn) Close() error { return c.inner.Close() }

func (c *watchedConn) Begin() (driver.Tx, error) { return c.inner.Begin() } //nolint:staticcheck

func (c *watchedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	defer noteCall("BEGIN", time.Now())
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.inner.Begin() //nolint:staticcheck
}

func (c *watchedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	defer noteCall(query, time.Now())
	return e.ExecContext(ctx, query, args)
}

func (c *watchedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	defer noteCall(query, time.Now())
	return q.QueryContext(ctx, query, args)
}

func (c *watchedConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *watchedConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *watchedConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

type watchedStmt struct {
	inner driver.Stmt
	query string
}

func (s *watchedStmt) Close() error  { return s.inner.Close() }
func (s *watchedStmt) NumInput() int { return s.inner.NumInput() }

func (s *watchedStmt) Exec(args []driver.Value) (driver.Result, error) {
	defer noteCall(s.query, time.Now())
	return s.inner.Exec(args) //nolint:staticcheck
}

func (s *watchedStmt) Query(args []driver.Value) (driver.Rows, error) {
	defer noteCall(s.query, time.Now())
	return s.inner.Query(args) //nolint:staticcheck
}

func (s *watchedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	defer noteCall(s.query, time.Now())
	if e, ok := s.inner.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	return s.inner.Exec(namedToValues(args)) //nolint:staticcheck
}

func (s *watchedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	defer noteCall(s.query, time.Now())
	if q, ok := s.inner.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	return s.inner.Query(namedToValues(args)) //nolint:staticcheck
}

func namedToValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// watchPool logs when callers have been waiting a long time for a free
// connection (every connection busy), which is how a stuck query shows up
// for everyone else. It stops when the database is closed.
func watchPool(db *sql.DB) {
	const every = time.Second
	tick := time.NewTicker(every)
	defer tick.Stop()
	var (
		lastWait  time.Duration
		lastCount int64
		lastLog   time.Time
		blocked   time.Duration // how long in a row a probe could not get a connection
	)
	for range tick.C {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := db.PingContext(ctx)
		cancel()
		if err != nil && strings.Contains(err.Error(), "database is closed") {
			return
		}
		if err != nil {
			blocked += every
		} else {
			blocked = 0
		}
		st := db.Stats()
		delta := st.WaitDuration - lastWait
		count := st.WaitCount - lastCount
		lastWait, lastCount = st.WaitDuration, st.WaitCount
		threshold := time.Duration(slowCall.Load())
		if (delta >= threshold || blocked >= threshold) && time.Since(lastLog) >= 10*time.Second {
			lastLog = time.Now()
			log.Printf("database: callers are waiting for a free connection in_use=%d max=%d waited=%s waits=%d blocked_for=%s",
				st.InUse, st.MaxOpenConnections, delta.Round(time.Millisecond), count, blocked)
		}
	}
}
