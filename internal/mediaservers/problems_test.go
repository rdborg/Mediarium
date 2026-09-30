package mediaservers

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/store"
)

func TestAServerThatCannotBeReachedIsLoggedAfterARefresh(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := problems.Open(db)
	problems.SetDefault(l)
	defer problems.SetDefault(nil)

	down := Server{ID: 4, Name: "Living room", Kind: KindJellyfin, BaseURL: "http://127.0.0.1:1", Token: "sekrit-token-value", Enabled: true, RefreshAfterImport: true}
	r := NewRefresher(&memStore{servers: []Server{down}}, NewClient("test"))
	r.Delay = 1 << 40
	r.Imported(MediaMovie, "/movies/Heat (1995)/Heat (1995).mkv")
	r.Flush()

	l.Flush()
	rows, _, err := l.List(problems.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want one row, got %+v", rows)
	}
	got := rows[0]
	if got.Code != problems.CodeMediaServerDown || got.Subject != "Living room" || got.Area != problems.AreaMediaServers || !strings.Contains(got.Message, "Living room") {
		t.Errorf("row = %+v", got)
	}
	if strings.Contains(got.Message+got.Detail, "sekrit-token-value") {
		t.Errorf("the token leaked: %+v", got)
	}
}
