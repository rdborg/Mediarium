package library_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

type officeEp struct {
	title, date string
}

// officeTMDB is season 4 of The Office as the movie database lists it: 14
// episodes, each double-length one counted once.
var officeTMDB = []officeEp{
	{"Fun Run", "2007-09-27"}, {"Dunder Mifflin Infinity", "2007-10-04"}, {"Launch Party", "2007-10-11"}, {"Money", "2007-10-18"},
	{"Local Ad", "2007-10-25"}, {"Branch Wars", "2007-11-01"}, {"Survivor Man", "2007-11-08"}, {"The Deposition", "2007-11-15"},
	{"Dinner Party", "2008-04-10"}, {"Chair Model", "2008-04-17"}, {"Night Out", "2008-04-24"}, {"Did I Stutter?", "2008-05-01"},
	{"Job Fair", "2008-05-08"}, {"Goodbye, Toby", "2008-05-15"},
}

// officeTVDB is the same season on TheTVDB: 19 episodes.
var officeTVDB = []officeEp{
	{"Fun Run (1)", "2007-09-27"}, {"Fun Run (2)", "2007-09-27"},
	{"Dunder Mifflin Infinity (1)", "2007-10-04"}, {"Dunder Mifflin Infinity (2)", "2007-10-04"},
	{"Launch Party (1)", "2007-10-11"}, {"Launch Party (2)", "2007-10-11"},
	{"Money (1)", "2007-10-18"}, {"Money (2)", "2007-10-18"},
	{"Local Ad", "2007-10-25"}, {"Branch Wars", "2007-11-01"}, {"Survivor Man", "2007-11-08"}, {"The Deposition", "2007-11-15"},
	{"Dinner Party", "2008-04-10"}, {"Chair Model", "2008-04-17"}, {"Night Out", "2008-04-24"}, {"Did I Stutter?", "2008-05-01"},
	{"Job Fair", "2008-05-08"}, {"Goodbye, Toby (1)", "2008-05-15"}, {"Goodbye, Toby (2)", "2008-05-15"},
}

func listOf(season int, eps []officeEp) []library.Episode {
	out := make([]library.Episode, len(eps))
	for i, e := range eps {
		out[i] = library.Episode{Season: season, Episode: i + 1, Title: e.title, AirDate: e.date}
	}
	return out
}

func TestOrderedEpisodes(t *testing.T) {
	tmdb := append([]library.Episode{{Season: 0, Episode: 1, Title: "Special"}}, listOf(4, officeTMDB)...)
	tmdb = append(tmdb, library.Episode{Season: 5, Episode: 1, Title: "Weight Loss"})
	tvdb := append(listOf(4, officeTVDB), library.Episode{Season: 0, Episode: 9, Title: "TheTVDB special"}, library.Episode{Season: 4, Episode: 3, Title: "duplicate"})
	got := library.OrderedEpisodes(tmdb, tvdb)
	var s0, s4, s5 int
	for _, e := range got {
		switch e.Season {
		case 0:
			s0++
			if e.Title != "Special" {
				t.Errorf("a special must come from TMDB: %+v", e)
			}
		case 4:
			s4++
		case 5:
			s5++
		}
	}
	if s0 != 1 || s4 != 19 || s5 != 1 {
		t.Errorf("specials %d (want 1), season 4 %d (want 19), season 5 %d (want 1, TMDB's)", s0, s4, s5)
	}
}

// numbers of the moves as "from>to".
func moveText(p library.EpisodeOrderPlan) []string {
	var out []string
	for _, m := range p.Moves {
		out = append(out, fmt.Sprintf("%d>%d", m.From, m.Episode))
	}
	return out
}

func stored(id int64, season, ep int, title, date, file string) library.Episode {
	e := library.Episode{ID: id, Season: season, Episode: ep, Title: title, AirDate: date, Status: library.StatusMissing}
	if file != "" {
		e.Status, e.FilePath = library.StatusDownloaded, file
	}
	return e
}

func TestPlanEpisodeOrder(t *testing.T) {
	cases := []struct {
		name    string
		old     []library.Episode
		target  []library.Episode
		moves   []string
		drop    []int64
		blocked []int
	}{
		{
			"the same list changes nothing",
			[]library.Episode{stored(1, 1, 1, "A", "2020-01-01", "/a"), stored(2, 1, 2, "B", "2020-01-08", "")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "B", AirDate: "2020-01-09"}},
			nil, nil, nil,
		},
		{
			"a double episode moves the ones after it, files follow",
			[]library.Episode{stored(1, 1, 1, "Pilot", "2020-01-01", "/p"), stored(2, 1, 2, "Second", "2020-01-08", "/s"), stored(3, 1, 3, "Third", "2020-01-15", "")},
			[]library.Episode{
				{Season: 1, Episode: 1, Title: "Pilot (1)", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "Pilot (2)", AirDate: "2020-01-01"},
				{Season: 1, Episode: 3, Title: "Second", AirDate: "2020-01-08"}, {Season: 1, Episode: 4, Title: "Third", AirDate: "2020-01-15"},
			},
			[]string{"2>3", "3>4"}, nil, nil,
		},
		{
			"episodes that never got anything and have no place go",
			[]library.Episode{stored(1, 1, 1, "A", "2020-01-01", "/a"), stored(2, 1, 2, "Gone", "2019-05-05", "")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}},
			nil, []int64{2}, nil,
		},
		{
			"a file that cannot be placed leaves the season alone",
			[]library.Episode{stored(1, 1, 1, "A", "2020-01-01", "/a"), stored(2, 1, 2, "Mystery", "2021-06-06", "/m"), stored(3, 1, 3, "C", "2020-01-15", "")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "Other", AirDate: "2020-01-08"}, {Season: 1, Episode: 3, Title: "C", AirDate: "2020-01-15"}},
			nil, nil, []int{1},
		},
		{
			"two files on one new episode: the second has no place, the season stays",
			[]library.Episode{stored(1, 1, 1, "A", "2020-01-01", "/a"), stored(2, 1, 2, "A again", "2020-01-01", "/a2")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "Zed", AirDate: "2020-02-02"}},
			nil, nil, []int{1},
		},
		{
			"a download in progress counts like a file",
			func() []library.Episode {
				e := stored(2, 1, 2, "Mystery", "2021-06-06", "")
				e.Status = library.StatusDownloading
				return []library.Episode{stored(1, 1, 1, "A", "2020-01-01", ""), e}
			}(),
			[]library.Episode{{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "Other", AirDate: "2020-01-08"}},
			nil, nil, []int{1},
		},
		{
			"numbers swapped between the lists",
			[]library.Episode{stored(1, 1, 1, "Alpha", "2020-01-01", "/a"), stored(2, 1, 2, "Beta", "2020-01-08", "/b")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "Beta", AirDate: "2020-01-08"}, {Season: 1, Episode: 2, Title: "Alpha", AirDate: "2020-01-01"}},
			[]string{"1>2", "2>1"}, nil, nil,
		},
		{
			"dates unknown, titles different, same shape: nothing to move",
			[]library.Episode{stored(1, 1, 1, "Ep One", "", "/a"), stored(2, 1, 2, "Ep Two", "", "/b")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "First", AirDate: ""}, {Season: 1, Episode: 2, Title: "Second", AirDate: ""}},
			nil, nil, nil,
		},
		{
			"a season only the old list has is not touched",
			[]library.Episode{stored(1, 3, 1, "A", "2020-01-01", "/a")},
			[]library.Episode{{Season: 1, Episode: 1, Title: "X", AirDate: "2018-01-01"}},
			nil, nil, nil,
		},
		{
			"specials are never moved",
			[]library.Episode{stored(1, 0, 1, "Special", "2020-01-01", "/sp")},
			[]library.Episode{{Season: 0, Episode: 5, Title: "Elsewhere", AirDate: "2020-01-01"}},
			nil, nil, nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := library.PlanEpisodeOrder(c.old, c.target)
			if got := moveText(p); !slices.Equal(got, c.moves) {
				t.Errorf("moves %v, want %v", got, c.moves)
			}
			if !slices.Equal(p.Drop, c.drop) {
				t.Errorf("drop %v, want %v", p.Drop, c.drop)
			}
			if !slices.Equal(p.Blocked, c.blocked) {
				t.Errorf("blocked %v, want %v", p.Blocked, c.blocked)
			}
			// Whatever the plan is, an episode with a file is never dropped.
			for _, id := range p.Drop {
				for _, o := range c.old {
					if o.ID == id && o.FilePath != "" {
						t.Errorf("episode %d has a file and was dropped", id)
					}
				}
			}
		})
	}
}

// The Office, end to end: the show has TMDB's 14 episodes of season 4 with
// some files; moving it to TheTVDB's 19 keeps every file on the episode it
// belongs to, and nothing with a file goes.
func TestEpisodeOrderOfTheOffice(t *testing.T) {
	repo := newRepo(t)
	sr, err := repo.AddSeries(library.Series{TMDBID: 2316, Title: "The Office", Monitored: true}, listOf(4, officeTMDB))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"Fun Run": "/tv/office/s04e01.mkv", "Dunder Mifflin Infinity": "/tv/office/s04e02.mkv", "Local Ad": "/tv/office/s04e05.mkv", "Goodbye, Toby": "/tv/office/s04e14.mkv"}
	old, _ := repo.ListEpisodes(sr.ID)
	for _, e := range old {
		if f, ok := files[e.Title]; ok {
			if err := repo.SetEpisodeStatus(e.ID, library.StatusDownloaded, "HDTV-720p", f); err != nil {
				t.Fatal(err)
			}
		}
		if e.Title == "Money" { // the person switched this one off
			_ = repo.SetEpisodeMonitored(e.ID, false)
		}
	}
	old, _ = repo.ListEpisodes(sr.ID)
	plan := library.PlanEpisodeOrder(old, listOf(4, officeTVDB))
	if len(plan.Blocked) != 0 {
		t.Fatalf("blocked %v", plan.Blocked)
	}
	if err := repo.ApplyEpisodeOrder(sr.ID, plan); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertEpisodes(sr.ID, plan.Final, true); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ListEpisodes(sr.ID)
	if len(got) != 19 {
		t.Fatalf("%d episodes, want 19", len(got))
	}
	byNumber := map[int]library.Episode{}
	for _, e := range got {
		byNumber[e.Episode] = e
	}
	for n, want := range map[int]string{1: "/tv/office/s04e01.mkv", 3: "/tv/office/s04e02.mkv", 9: "/tv/office/s04e05.mkv", 18: "/tv/office/s04e14.mkv"} {
		if byNumber[n].FilePath != want || byNumber[n].Status != library.StatusDownloaded {
			t.Errorf("S04E%02d = %+v, want the file %s", n, byNumber[n], want)
		}
	}
	for _, n := range []int{2, 4, 19} {
		if byNumber[n].FilePath != "" || byNumber[n].Status != library.StatusMissing {
			t.Errorf("S04E%02d should be missing: %+v", n, byNumber[n])
		}
	}
	if byNumber[3].Title != "Dunder Mifflin Infinity (1)" || byNumber[7].Monitored {
		t.Errorf("titles come from TheTVDB (%q) and the switched-off Money (%+v) stays switched off", byNumber[3].Title, byNumber[7])
	}
	files2 := 0
	for _, e := range got {
		if e.FilePath != "" {
			files2++
		}
	}
	if files2 != 4 {
		t.Errorf("%d files attached, want 4", files2)
	}
	// Going back to TMDB's list: the doubles' second halves have no file, so
	// the files return to their old numbers.
	old, _ = repo.ListEpisodes(sr.ID)
	back := library.PlanEpisodeOrder(old, listOf(4, officeTMDB))
	if len(back.Blocked) != 0 {
		t.Fatalf("going back is blocked: %v", back.Blocked)
	}
	if err := repo.ApplyEpisodeOrder(sr.ID, back); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertEpisodes(sr.ID, back.Final, true); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.ListEpisodes(sr.ID)
	if len(again) != 14 {
		t.Fatalf("%d episodes after going back, want 14", len(again))
	}
	for _, e := range again {
		if want, ok := files[e.Title]; ok && e.FilePath != want {
			t.Errorf("after going back %s has %q, want %q", e.Title, e.FilePath, want)
		}
	}
}

// A file that cannot be placed blocks only its season, and the database is
// left as it was for that season.
func TestEpisodeOrderKeepsABlockedSeasonAsItWas(t *testing.T) {
	repo := newRepo(t)
	sr, _ := repo.AddSeries(library.Series{TMDBID: 5, Title: "Show", Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "Odd one", AirDate: "2030-01-01"},
		{Season: 2, Episode: 1, Title: "C", AirDate: "2021-01-01"}, {Season: 2, Episode: 2, Title: "D", AirDate: "2021-01-08"},
	})
	old, _ := repo.ListEpisodes(sr.ID)
	for _, e := range old {
		if e.Title == "Odd one" || e.Title == "D" {
			_ = repo.SetEpisodeStatus(e.ID, library.StatusDownloaded, "", "/f/"+e.Title)
		}
	}
	old, _ = repo.ListEpisodes(sr.ID)
	target := []library.Episode{
		{Season: 1, Episode: 1, Title: "A", AirDate: "2020-01-01"}, {Season: 1, Episode: 2, Title: "Zed", AirDate: "2020-01-08"},
		{Season: 2, Episode: 1, Title: "C (1)", AirDate: "2021-01-01"}, {Season: 2, Episode: 2, Title: "C (2)", AirDate: "2021-01-01"}, {Season: 2, Episode: 3, Title: "D", AirDate: "2021-01-08"},
	}
	plan := library.PlanEpisodeOrder(old, target)
	if !slices.Equal(plan.Blocked, []int{1}) {
		t.Fatalf("blocked %v, want [1]", plan.Blocked)
	}
	if err := repo.ApplyEpisodeOrder(sr.ID, plan); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertEpisodes(sr.ID, plan.Final, true); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ListEpisodes(sr.ID)
	have := map[[2]int]library.Episode{}
	for _, e := range got {
		have[[2]int{e.Season, e.Episode}] = e
	}
	if e := have[[2]int{1, 2}]; e.Title != "Odd one" || e.FilePath != "/f/Odd one" {
		t.Errorf("the blocked season must stay as it was: %+v", e)
	}
	if e := have[[2]int{2, 3}]; e.FilePath != "/f/D" || e.Title != "D" {
		t.Errorf("season 2 should have moved D to episode 3: %+v", e)
	}
}

func TestEpisodeOrderIsStoredPerShow(t *testing.T) {
	repo := newRepo(t)
	sr, _ := repo.AddSeries(library.Series{TMDBID: 6, Title: "Show", Monitored: true}, nil)
	if sr.EpisodeOrder != library.EpisodeOrderTMDB {
		t.Fatalf("a new show follows %q, want tmdb", sr.EpisodeOrder)
	}
	if err := repo.SetEpisodeOrder(sr.ID, "nonsense"); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetSeries(sr.ID); got.EpisodeOrder != library.EpisodeOrderTMDB {
		t.Errorf("an unknown order must be stored as tmdb, got %q", got.EpisodeOrder)
	}
	if err := repo.SetEpisodeOrder(sr.ID, library.EpisodeOrderTVDB); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetSeries(sr.ID); got.EpisodeOrder != library.EpisodeOrderTVDB {
		t.Errorf("order = %q, want tvdb", got.EpisodeOrder)
	}
	cp, err := repo.AddSeriesCopy(sr.ID, 3)
	if err != nil {
		t.Skipf("second copy: %v", err)
	}
	if cp.EpisodeOrder != library.EpisodeOrderTVDB {
		t.Errorf("a second copy follows the main show's order, got %q", cp.EpisodeOrder)
	}
}

// Downloads, watched state and delayed releases name an episode by its
// numbers: they move with the episode when the order changes.
func TestEpisodeOrderMovesRowsKeyedByNumber(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := library.NewRepo(db)
	sr, err := repo.AddSeries(library.Series{TMDBID: 2316, Title: "The Office", Monitored: true}, listOf(4, officeTMDB))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := repo.ListEpisodes(sr.ID)
	for _, e := range old {
		if e.Title == "Local Ad" || e.Title == "Goodbye, Toby" {
			if err := repo.SetEpisodeStatus(e.ID, library.StatusDownloaded, "HDTV-720p", fmt.Sprintf("/tv/office/s04e%02d.mkv", e.Episode)); err != nil {
				t.Fatal(err)
			}
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Local Ad (E05) is downloading, Goodbye, Toby (E14) was watched and
	// Dunder Mifflin Infinity (E02) has a release held back by a delay.
	exec(`INSERT INTO download_queue (release_title, series_id, season, episode) VALUES ('The.Office.S04E05', ?, 4, 5)`, sr.ID)
	exec(`INSERT INTO download_queue (release_title, series_id, season, episode) VALUES ('The.Office.S04', ?, 4, NULL)`, sr.ID)
	exec(`INSERT INTO watched (kind, title_id, season, episode, plays) VALUES ('episode', ?, 4, 14, 2)`, sr.ID)
	exec(`INSERT INTO delayed_releases (kind, series_id, season, episode, release_title, first_seen_at, due_at) VALUES ('episode', ?, 4, 2, 'The.Office.S04E02', 'x', 'y')`, sr.ID)

	old, _ = repo.ListEpisodes(sr.ID)
	plan := library.PlanEpisodeOrder(old, listOf(4, officeTVDB))
	if err := repo.ApplyEpisodeOrder(sr.ID, plan); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, query string
		want        int
	}{
		{"download of Local Ad", `SELECT episode FROM download_queue WHERE release_title = 'The.Office.S04E05'`, 9},
		{"watched Goodbye, Toby", `SELECT episode FROM watched WHERE title_id = ` + fmt.Sprint(sr.ID), 18},
		{"delayed Dunder Mifflin Infinity", `SELECT episode FROM delayed_releases WHERE release_title = 'The.Office.S04E02'`, 3},
		{"season pack stays a season pack", `SELECT COALESCE(episode, -1) FROM download_queue WHERE release_title = 'The.Office.S04'`, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			if err := db.QueryRow(tt.query).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("episode %d, want %d", got, tt.want)
			}
		})
	}
}

// A removed episode takes its downloads, watched state and delayed releases
// with it: the episode that moves to its number must not inherit them.
func TestEpisodeOrderDropsTheRowsOfARemovedEpisode(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := library.NewRepo(db)
	var eps []library.Episode
	for n := 1; n <= 6; n++ {
		eps = append(eps, library.Episode{Season: 1, Episode: n, Title: fmt.Sprintf("Episode %d", n)})
	}
	sr, err := repo.AddSeries(library.Series{TMDBID: 99, Title: "Show", Monitored: true}, eps)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := repo.ListEpisodes(sr.ID)
	ids := map[int]int64{}
	for _, e := range old {
		ids[e.Episode] = e.ID
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// E6 goes and E5 takes its number. Both have a release held back by a
	// delay (one of them the same release), and E6 an old failed download
	// and a play.
	exec(`INSERT INTO delayed_releases (kind, series_id, season, episode, release_title, first_seen_at, due_at) VALUES
		('episode', ?, 1, 6, 'Show.S01E06.Dropped', 'x', 'y'),
		('episode', ?, 1, 6, 'Show.S01.Same', 'x', 'dropped'),
		('episode', ?, 1, 5, 'Show.S01E05.Moved', 'x', 'y'),
		('episode', ?, 1, 5, 'Show.S01.Same', 'x', 'moved')`, sr.ID, sr.ID, sr.ID, sr.ID)
	exec(`INSERT INTO download_queue (release_title, series_id, season, episode, status) VALUES ('Show.S01E06.Old', ?, 1, 6, 'failed')`, sr.ID)
	exec(`INSERT INTO watched (kind, title_id, season, episode, plays) VALUES ('episode', ?, 1, 6, 3)`, sr.ID)

	plan := library.EpisodeOrderPlan{
		Moves: []library.EpisodeMove{{ID: ids[5], Season: 1, From: 5, Episode: 6}},
		Drop:  []int64{ids[6]},
	}
	if err := repo.ApplyEpisodeOrder(sr.ID, plan); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, query string
		want        string
	}{
		{"delayed releases at E6", `SELECT group_concat(release_title || '/' || due_at, ',') FROM (SELECT * FROM delayed_releases WHERE season = 1 AND episode = 6 ORDER BY release_title)`, "Show.S01.Same/moved,Show.S01E05.Moved/y"},
		{"delayed releases at E5", `SELECT COUNT(*) FROM delayed_releases WHERE season = 1 AND episode = 5`, "0"},
		{"downloads at E6", `SELECT COUNT(*) FROM download_queue WHERE season = 1 AND episode = 6`, "0"},
		{"plays at E6", `SELECT COUNT(*) FROM watched WHERE title_id = ` + fmt.Sprint(sr.ID) + ` AND season = 1 AND episode = 6`, "0"},
		{"the removed episode", `SELECT COUNT(*) FROM episodes WHERE id = ` + fmt.Sprint(ids[6]), "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if err := db.QueryRow(tt.query).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
