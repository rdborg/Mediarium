package indexers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/store"
)

func TestFailedSearchesAreLoggedByWhatWentWrong(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := problems.Open(db)
	problems.SetDefault(l)
	defer problems.SetDefault(nil)

	tests := []struct {
		name string
		err  error
		want string // empty: nothing logged
	}{
		{"rate limit", errors.New("newznab: status 429 Too Many Requests"), problems.CodeIndexerRateLimited},
		{"refused key", errors.New("newznab error 100: Incorrect user credentials"), problems.CodeIndexerAuthRefused},
		{"cannot reach", fmt.Errorf("search: %w", context.DeadlineExceeded), problems.CodeIndexerUnreachable},
		{"other", errors.New("the site returned something odd"), problems.CodeIndexerFailed},
		{"cancelled by the person", context.Canceled, ""},
		{"fine", nil, ""},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := "Source " + string(rune('A'+i))
			noteSearchProblem(Outcome{IndexerName: name, Err: tt.err})
			l.Flush()
			all, _, err := l.List(problems.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			var rows []problems.Entry
			for _, r := range all {
				if r.Subject == name {
					rows = append(rows, r)
				}
			}
			if tt.want == "" {
				if len(rows) != 0 {
					t.Fatalf("nothing should be logged: %+v", rows)
				}
				return
			}
			if len(rows) != 1 || rows[0].Code != tt.want || rows[0].Subject != name {
				t.Fatalf("rows = %+v, want one %s", rows, tt.want)
			}
		})
	}
}
