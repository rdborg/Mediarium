package download

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/store"
)

// problemLog attaches a real problem log for the test and returns it.
func problemLog(t *testing.T) *problems.Log {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	l := problems.Open(db)
	problems.SetDefault(l)
	t.Cleanup(func() {
		l.Flush()
		problems.SetDefault(nil)
		db.Close()
	})
	return l
}

func problemRows(t *testing.T, l *problems.Log) []problems.Entry {
	t.Helper()
	l.Flush()
	got, _, err := l.List(problems.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTooManyConnectionsIsLoggedOnceWithACount(t *testing.T) {
	fastGate(t)
	l := problemLog(t)
	g := gateFor(ClientConfig{Host: "problems.example", Port: 119, Username: "u", Password: "hunter2", Connections: 8})
	for i := 0; i < 5; i++ {
		g.tooMany()
	}
	rows := problemRows(t, l)
	if len(rows) != 1 {
		t.Fatalf("want one row for five refusals, got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Code != problems.CodeUsenetTooMany || r.Count != 5 || r.Subject != "problems.example" || r.Level != problems.LevelWarning {
		t.Errorf("row = %+v", r)
	}
	if !strings.Contains(r.Message, "problems.example") || strings.Contains(r.Message+r.Detail, "hunter2") {
		t.Errorf("message = %q", r.Message)
	}
}

func TestServerTroubleIsLoggedByKind(t *testing.T) {
	l := problemLog(t)
	cfg := ClientConfig{Host: "news.example.com", Port: 563, Username: "u", Password: "hunter2"}
	tests := []struct {
		name string
		err  error
		want string // "" means nothing is logged here
	}{
		{"refused login", &NNTPError{Code: 481, Message: "Authentication failed"}, problems.CodeUsenetAuthRefused},
		{"cannot connect", errors.New("dial tcp: lookup news.example.com: no such host"), problems.CodeUsenetUnreachable},
		{"too many is logged by the gate", &NNTPError{Code: 502, Message: "Too many connections"}, ""},
		{"a removed server is not a problem", errServerRemoved, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(problemRows(t, l))
			_ = plainConnError(cfg, fmt.Errorf("nntp authenticate on %s: %w", cfg.Host, tt.err))
			_ = plainConnError(cfg, tt.err)
			rows := problemRows(t, l)
			if tt.want == "" {
				if len(rows) != before {
					t.Fatalf("nothing should be logged, got %+v", rows)
				}
				return
			}
			var found bool
			for _, r := range rows {
				if r.Code == tt.want {
					found = true
					if strings.Contains(r.Message+r.Detail, "hunter2") {
						t.Errorf("the password leaked: %+v", r)
					}
				}
			}
			if !found {
				t.Fatalf("want %s in %+v", tt.want, rows)
			}
		})
	}
}
