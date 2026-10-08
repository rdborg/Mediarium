package metadata

import (
	"encoding/json"
	"testing"
)

func TestReleaseWindow(t *testing.T) {
	var d MovieDetail
	raw := `{"release_dates":{"results":[
		{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"2026-09-05T00:00:00.000Z","certification":"PG-13"},{"type":4,"release_date":"2026-11-10T00:00:00.000Z"}]},
		{"iso_3166_1":"GB","release_dates":[{"type":3,"release_date":"2026-09-04T00:00:00.000Z"},{"type":5,"release_date":"2026-12-01T00:00:00.000Z"},{"type":6,"release_date":"2027-01-01T00:00:00.000Z"}]},
		{"iso_3166_1":"FR","release_dates":[{"type":1,"release_date":"2026-08-20T00:00:00.000Z"},{"type":4,"release_date":""}]}
	]}}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	theatrical, home := d.ReleaseWindow()
	if got := theatrical.Format("2006-01-02"); got != "2026-09-04" {
		t.Errorf("theatrical = %s, want the earliest cinema date 2026-09-04", got)
	}
	if got := home.Format("2006-01-02"); got != "2026-11-10" {
		t.Errorf("home = %s, want the earliest digital or disc date 2026-11-10", got)
	}
	var none MovieDetail
	if th, h := none.ReleaseWindow(); !th.IsZero() || !h.IsZero() {
		t.Errorf("no dates must give zero times, got %v %v", th, h)
	}
}
