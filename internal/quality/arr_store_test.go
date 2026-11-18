package quality_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/store"
)

func titleFormat(name string, app quality.FormatApp) quality.CustomFormat {
	f := quality.CustomFormat{Name: name, App: app, Specs: []quality.FormatSpec{{Name: "p", Kind: quality.SpecReleaseTitle, Value: "abc"}}}
	f.Compile()
	return f
}

// A format name is unique for each app: the TRaSH Guides use the same name in
// the Radarr and the Sonarr guide.
func TestFormatNamesAreUniquePerApp(t *testing.T) {
	r := newFormatRepo(t)
	a, err := r.Create(titleFormat("HDR", quality.AppRadarr))
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Create(titleFormat("hdr", quality.AppSonarr))
	if err != nil {
		t.Fatalf("the same name for the other app: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("one id for two formats")
	}
	if _, err := r.Create(titleFormat("Hdr", quality.AppRadarr)); !errors.Is(err, quality.ErrFormatName) {
		t.Errorf("a second Radarr HDR: %v, want ErrFormatName", err)
	}
	// Save (the importers) replaces within the app, and never the other app's.
	again := titleFormat("HDR", quality.AppSonarr)
	again.Specs[0].Value = "changed"
	saved, isNew, err := r.Save(again)
	if err != nil || isNew || saved.ID != b.ID {
		t.Fatalf("Save: id %d new %v err %v, want the Sonarr one (%d) updated", saved.ID, isNew, err, b.ID)
	}
	if got, _ := r.Get(a.ID); got.Specs[0].Value != "abc" {
		t.Errorf("the Radarr format changed: %+v", got.Specs)
	}
	// Renaming onto a name the same app has is refused, the other app's is fine.
	c, _ := r.Create(titleFormat("Other", quality.AppRadarr))
	if err := r.Rename(c.ID, "hdr"); !errors.Is(err, quality.ErrFormatName) {
		t.Errorf("rename onto a taken name: %v", err)
	}
	d, _ := r.Create(titleFormat("Other", quality.AppSonarr))
	if err := r.Rename(d.ID, "HDR"); !errors.Is(err, quality.ErrFormatName) {
		t.Errorf("rename onto a taken name of the same app: %v", err)
	}
}

func TestFormatReplaceKeepsWhereItCameFrom(t *testing.T) {
	r := newFormatRepo(t)
	zero := 0
	f := titleFormat("From TRaSH", quality.AppRadarr)
	f.TrashID, f.TrashScore = "abc123", &zero
	made, err := r.Create(f)
	if err != nil {
		t.Fatal(err)
	}
	next := titleFormat("From TRaSH 2", quality.AppRadarr)
	next.IncludeInName = true
	next.Specs[0].Value = "new"
	got, err := r.Replace(made.ID, next)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "From TRaSH 2" || !got.IncludeInName || got.Specs[0].Value != "new" || got.TrashID != "abc123" || got.TrashScore == nil {
		t.Errorf("Replace: %+v", got)
	}
	if _, err := r.Replace(99999, next); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Replace of nothing: %v", err)
	}
}

// What an app wrote next to a profile comes back with it.
func TestProfileKeepsWhatTheArrAPIWrote(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := quality.NewRepo(db)
	p := quality.Profile{Name: "Kept", Allowed: []quality.Tier{quality.TierBluray1080p}, Cutoff: quality.TierBluray1080p, ArrExtra: `{"minUpgrade":3}`}
	created, err := r.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(created.ID)
	if err != nil || got.ArrExtra != `{"minUpgrade":3}` {
		t.Fatalf("Get: %+v %v", got, err)
	}
	got.ArrExtra = ""
	if _, err := r.Update(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := r.Get(created.ID); again.ArrExtra != "" {
		t.Errorf("cleared: %q", again.ArrExtra)
	}
}
