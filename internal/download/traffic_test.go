package download

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// What each server delivers is counted per login and saved per day, and an
// article the server doesn't have counts as missing (#167).
func TestTrafficIsCountedPerServerAndSaved(t *testing.T) {
	fullData := []byte("The quick brown fox jumps over the lazy dog. Traffic fixture.")
	mid := len(fullData) / 2
	srv := newFakeNNTPServer(t, map[string][]byte{
		"t1@example": buildMultipartYenc("a.mkv", fullData, 1, mid),
		"t2@example": buildMultipartYenc("a.mkv", fullData, mid+1, len(fullData)),
	})
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "traffic-user", Password: "p", Connections: 1}
	nzb := &NZB{Files: []NZBFile{{
		Subject: `"a.mkv" yEnc (1/3)`, Groups: []string{"alt.binaries.test"},
		Segments: []NZBSegment{
			{Number: 1, Bytes: int64(mid), MessageID: "t1@example"},
			{Number: 2, Bytes: int64(len(fullData) - mid), MessageID: "t2@example"},
			{Number: 3, Bytes: 10, MessageID: "gone@example"},
		},
	}}}
	takeTraffic() // start from nothing
	_, _ = Download(context.Background(), cfg, nzb, t.TempDir(), nil)

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "k.key"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepo(db, box)
	sc, err := repo.Create(StoredClient{Name: "Fixture", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.SaveTraffic(now); err != nil {
		t.Fatal(err)
	}
	list, err := repo.TrafficSince(now.AddDate(0, 0, -1))
	if err != nil || len(list) != 1 {
		t.Fatalf("traffic: %v %v", list, err)
	}
	got := list[0]
	if got.ServerID != sc.ID || got.Articles != 2 || got.Bytes <= int64(len(fullData)) || got.Missing < 1 {
		t.Errorf("saved %+v, want 2 articles, more than %d bytes and at least 1 missing", got, len(fullData))
	}
	// Saved counts are not saved twice.
	if err := repo.SaveTraffic(now); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.TrafficSince(now.AddDate(0, 0, -1)); again[0].Articles != 2 {
		t.Errorf("counted twice: %+v", again[0])
	}
	repo.PruneTraffic(now.AddDate(0, 0, 100), 90)
	if pruned, _ := repo.TrafficSince(now.AddDate(0, 0, -1)); pruned[0].Articles != 0 {
		t.Errorf("old days were not pruned: %+v", pruned[0])
	}
}
