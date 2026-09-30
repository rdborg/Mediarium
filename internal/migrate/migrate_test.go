package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/store"
)

const (
	radarrKey   = "radarr-fixture-key-0001"
	sonarrKey   = "sonarr-fixture-key-0002"
	prowlarrKey = "prowlarr-fixture-key-0003"
	sabKey      = "abcdef0123456789abcdef0123456789"
)

type harness struct {
	db         *sql.DB
	im         *Importer
	lib        *library.Repo
	idx        *indexers.Repo
	servers    *download.Repo
	profiles   map[string]int64 // profile name -> id
	moviesRoot string
	tvRoot     string
	radarr     *fakeApp
	sonarr     *fakeApp
	prowlarr   *fakeApp
	sab        *fakeApp
	logs       *bytes.Buffer
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// siteDefinitions stand in for Mediarium's downloaded site list.
func siteDefinitions(_ context.Context, id string) (indexers.DefinitionSummary, error) {
	switch id {
	case "1337x":
		return indexers.DefinitionSummary{
			ID: "1337x", Name: "1337x", Protocol: "torrent", Supported: true,
			Links: []string{"https://1337x.to/", "https://1337x.st/"},
			Settings: []indexers.SettingSummary{
				{Name: "downloadlink", Type: "select", Options: []indexers.SettingOption{{Value: "http://itorrents.org/"}, {Value: "magnet:"}}},
				{Name: "sort", Type: "select", Options: []indexers.SettingOption{{Value: "time"}, {Value: "seeders"}, {Value: "size"}}},
				{Name: "info_flaresolverr", Type: "info_flaresolverr"},
			},
		}, nil
	case "privatesite":
		return indexers.DefinitionSummary{
			ID: "privatesite", Name: "PrivateSite", Protocol: "torrent", Supported: true,
			Links: []string{"https://private.example/"},
			Settings: []indexers.SettingSummary{
				{Name: "username", Type: "text"},
				{Name: "password", Type: "password"},
			},
		}, nil
	}
	return indexers.DefinitionSummary{}, indexers.ErrDefinitionNotFound
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		db: db, lib: library.NewRepo(db), idx: indexers.NewRepo(db, box), servers: download.NewRepo(db, box),
		profiles: map[string]int64{}, moviesRoot: filepath.Join(dir, "movies"), tvRoot: filepath.Join(dir, "shows"),
	}
	qrepo := quality.NewRepo(db)
	seeded, err := qrepo.SeedPresets(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range seeded {
		h.profiles[p.Name] = p.ID
	}

	// The library as Mediarium sees it: /data/media/movies in Radarr is
	// <tmp>/movies here, /data/media/tv in Sonarr is <tmp>/shows.
	touchFile(t, filepath.Join(h.moviesRoot, "Inception (2010)", "Inception (2010) Bluray-1080p.mkv"))
	touchFile(t, filepath.Join(h.moviesRoot, "Inception (2010)", "Inception (2010)-trailer.mkv"))
	touchFile(t, filepath.Join(h.moviesRoot, "Heat (1995)", "Heat.mkv"))
	touchFile(t, filepath.Join(h.moviesRoot, "The Matrix (1999)", "The Matrix (1999) Bluray-1080p.mkv"))
	touchFile(t, filepath.Join(h.tvRoot, "Breaking Bad", "Season 01", "Breaking.Bad.S01E01.720p.HDTV.x264-GRP.mkv"))
	touchFile(t, filepath.Join(h.tvRoot, "Breaking Bad", "Season 01", "Breaking.Bad.S01E02.720p.HDTV.x264-GRP.mkv"))
	touchFile(t, filepath.Join(h.tvRoot, "Breaking Bad", "Season 02", "S02E01 - Seven Thirty-Seven.mkv"))
	if err := os.MkdirAll(filepath.Join(h.tvRoot, "Fixture Old Show"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The Matrix is already in Mediarium, downloaded.
	matrix, err := h.lib.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.lib.SetStatus(matrix.ID, library.StatusDownloaded, "Bluray-1080p", filepath.Join(h.moviesRoot, "The Matrix (1999)", "The Matrix (1999) Bluray-1080p.mkv")); err != nil {
		t.Fatal(err)
	}

	tmdb := metadata.NewWithBaseURL("tmdb-fixture-key", newFakeTMDB(t).URL)
	h.radarr = newFakeRadarr(t, radarrKey)
	h.sonarr = newFakeSonarr(t, sonarrKey)
	h.prowlarr = newFakeProwlarr(t, prowlarrKey)
	h.sab = newFakeSABnzbd(t, sabKey)

	defaultID := h.profiles["1080p"]
	h.im = New(Deps{
		Library: h.lib, Indexers: h.idx, Servers: h.servers, Profiles: qrepo,
		TMDB:             func() *metadata.Client { return tmdb },
		MoviesRoot:       func() string { return h.moviesRoot },
		TVRoot:           func() string { return h.tvRoot },
		DefaultProfileID: func() int64 { return defaultID },
		Definition:       siteDefinitions,
		TestIndexer: func(_ context.Context, inst indexers.Instance) error {
			if inst.Name == "Jackett (all)" {
				return errors.New("could not reach jackett:9117")
			}
			return nil
		},
	})

	// Capture everything logged, to prove no secret ends up in the log.
	h.logs = &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func (h *harness) sources() Sources {
	return Sources{
		Radarr:   &Conn{URL: h.radarr.URL, APIKey: radarrKey},
		Sonarr:   &Conn{URL: h.sonarr.URL, APIKey: sonarrKey},
		Prowlarr: &Conn{URL: h.prowlarr.URL, APIKey: prowlarrKey},
		SABnzbd:  &Conn{URL: h.sab.URL + "/", APIKey: sabKey},
	}
}

func (h *harness) assertOnlyGET(t *testing.T) {
	t.Helper()
	for _, f := range []*fakeApp{h.radarr, h.sonarr, h.prowlarr, h.sab} {
		f.assertOnlyGET(t)
	}
}

func titleByName(t *testing.T, items []TitleItem, title string) TitleItem {
	t.Helper()
	for _, it := range items {
		if it.Title == title {
			return it
		}
	}
	t.Fatalf("no item %q in %+v", title, items)
	return TitleItem{}
}

func TestPreviewChangesNothingAndReportsEveryItem(t *testing.T) {
	h := newHarness(t)
	pv, err := h.im.Preview(context.Background(), Options{Sources: h.sources()})
	if err != nil {
		t.Fatal(err)
	}

	// Radarr
	r := pv.Radarr
	if r == nil || !r.OK || r.Version != "5.14.0.9383" {
		t.Fatalf("radarr status: %+v", r)
	}
	if r.Summary != (Summary{Total: 6, Add: 3, Exists: 1, Skip: 2}) {
		t.Fatalf("radarr summary: %+v", r.Summary)
	}
	if len(r.RootFolders) != 1 {
		t.Fatalf("radarr root folders: %+v", r.RootFolders)
	}
	if rf := r.RootFolders[0]; rf.Path != "/data/media/movies" || rf.MappedTo != h.moviesRoot || !rf.Exists || !rf.Suggested || rf.Titles != 6 || rf.FoldersFound != 3 {
		t.Fatalf("radarr root folder: %+v", rf)
	}
	missing := titleByName(t, r.Items, "Missing Folder")
	if missing.Action != ActionSkip || missing.Reason != "folder not found at "+filepath.Join(h.moviesRoot, "Missing Folder (2001)") {
		t.Fatalf("missing folder: %+v", missing)
	}
	if it := titleByName(t, r.Items, "Obscure Home Video"); it.Action != ActionSkip || it.Reason != "no TMDB id" {
		t.Fatalf("no tmdb id: %+v", it)
	}
	if it := titleByName(t, r.Items, "The Matrix"); it.Action != ActionExists || it.Reason != "already in your library" {
		t.Fatalf("existing movie: %+v", it)
	}
	inc := titleByName(t, r.Items, "Inception")
	if inc.Action != ActionAdd || !inc.FolderFound || !inc.InLibraryFolder || inc.ProfileID != h.profiles["1080p"] || inc.Quality != "Bluray-1080p" {
		t.Fatalf("inception: %+v", inc)
	}
	if it := titleByName(t, r.Items, "Heat"); it.ProfileID != h.profiles["4K & over"] || it.ArrProfile != "Ultra-HD" {
		t.Fatalf("heat profile: %+v", it)
	}

	// Sonarr: the TV folder has another name, found by its title folders.
	s := pv.Sonarr
	if s == nil || !s.OK || s.Summary != (Summary{Total: 3, Add: 2, Skip: 1}) {
		t.Fatalf("sonarr: %+v", s)
	}
	if rf := s.RootFolders[0]; rf.Path != "/data/media/tv" || rf.MappedTo != h.tvRoot || !rf.Suggested {
		t.Fatalf("sonarr root folder: %+v", rf)
	}
	old := titleByName(t, s.Items, "Fixture Old Show")
	if old.TMDBID != 5555 || old.TVDBID != 12345 || len(old.UnmonitoredSeasons) != 1 || old.UnmonitoredSeasons[0] != 2 || old.ProfileID != h.profiles["720p"] {
		t.Fatalf("tvdb-only show: %+v", old)
	}
	if it := titleByName(t, s.Items, "Unknown To TMDB"); it.Action != ActionSkip || !strings.Contains(it.Reason, "no TMDB id") {
		t.Fatalf("unknown show: %+v", it)
	}

	// Prowlarr
	p := pv.Prowlarr
	if p == nil || !p.OK || p.Summary != (Summary{Total: 9, Add: 5, Exists: 1, Skip: 3}) {
		t.Fatalf("prowlarr: %+v", p)
	}
	for _, it := range p.Items {
		switch it.Name {
		case "NZBgeek":
			if it.Action != ActionAdd || it.BaseURL != "https://api.nzbgeek.info" || it.Protocol != "usenet" || len(it.Categories) != 5 {
				t.Fatalf("nzbgeek: %+v", it)
			}
		case "NZBgeek (second copy)":
			if it.Action != ActionExists {
				t.Fatalf("duplicate indexer: %+v", it)
			}
		case "1337x":
			if it.Action != ActionAdd || it.DefinitionID != "1337x" || it.BaseURL != "https://1337x.to/" {
				t.Fatalf("1337x: %+v", it)
			}
		case "Gone Site", "BroadcasTheNet", "Odd Path":
			if it.Action != ActionSkip || it.Reason == "" {
				t.Fatalf("%s should be skipped with a reason: %+v", it.Name, it)
			}
		}
	}

	// SABnzbd
	sab := pv.SABnzbd
	if sab == nil || !sab.OK || sab.Version != "4.3.3" || sab.Summary != (Summary{Total: 3, Add: 3}) || sab.CompleteDir != "/downloads" {
		t.Fatalf("sabnzbd: %+v", sab)
	}
	if len(sab.Categories) != 2 || !sab.Items[0].HasPassword || sab.Items[1].HasPassword || sab.Items[1].Priority != 1 || sab.Items[2].Enabled {
		t.Fatalf("sabnzbd items: %+v", sab.Items)
	}

	if len(pv.SuggestedPathMap) != 2 || len(pv.PathMap) != 2 {
		t.Fatalf("path maps: %+v / %+v", pv.PathMap, pv.SuggestedPathMap)
	}

	// Nothing was written anywhere.
	if movies, _ := h.lib.List(); len(movies) != 1 {
		t.Fatalf("preview added movies: %+v", movies)
	}
	if list, _ := h.idx.List(); len(list) != 0 {
		t.Fatalf("preview added indexers: %+v", list)
	}
	if list, _ := h.servers.ListAll(); len(list) != 0 {
		t.Fatalf("preview added servers: %+v", list)
	}
	h.assertOnlyGET(t)
}

func TestImportRunsOnceAndIsIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	opts := Options{Sources: h.sources()}

	st, err := h.im.Run(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if st.Running || st.Step != StepDone || st.Done != st.Total || st.Total != 21 {
		t.Fatalf("status: running=%v step=%s %d/%d errors=%v", st.Running, st.Step, st.Done, st.Total, st.Errors)
	}
	res := st.Results
	if res.Movies != (Counts{Added: 3, Existing: 1, Skipped: 2, FilesLinked: 2}) {
		t.Fatalf("movies: %+v (items %+v)", res.Movies, st.Items)
	}
	if res.Series != (Counts{Added: 2, Skipped: 1, FilesLinked: 3}) {
		t.Fatalf("series: %+v (items %+v)", res.Series, st.Items)
	}
	if res.Indexers != (Counts{Added: 5, Existing: 1, Skipped: 3, Disabled: 3}) {
		t.Fatalf("indexers: %+v (items %+v)", res.Indexers, st.Items)
	}
	if res.UsenetServers != (Counts{Added: 3, Disabled: 2}) {
		t.Fatalf("servers: %+v", res.UsenetServers)
	}
	if len(res.QualityProfiles) != 7 {
		t.Fatalf("profile mapping: %+v", res.QualityProfiles)
	}
	if len(st.Notes) == 0 || !strings.Contains(st.Notes[0], "No search was started") {
		t.Fatalf("expected the no-search note, got %v", st.Notes)
	}
	if len(st.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", st.Errors)
	}

	// Movies registered in place with the right quality, flags and profile.
	inc, ok, _ := h.lib.GetByTMDBID(27205)
	wantInc := filepath.Join(h.moviesRoot, "Inception (2010)", "Inception (2010) Bluray-1080p.mkv")
	if !ok || inc.Status != library.StatusDownloaded || inc.FilePath != wantInc || inc.Quality != "Bluray-1080p" || !inc.Monitored || inc.ProfileID != h.profiles["1080p"] {
		t.Fatalf("inception: %+v", inc)
	}
	heat, _, _ := h.lib.GetByTMDBID(949)
	if heat.Status != library.StatusDownloaded || heat.Quality != "Remux-1080p" || heat.Monitored || heat.ProfileID != h.profiles["4K & over"] {
		t.Fatalf("heat (quality from Radarr, unmonitored): %+v", heat)
	}
	dune, _, _ := h.lib.GetByTMDBID(693134)
	if dune.Status != library.StatusMissing || !dune.Monitored {
		t.Fatalf("dune should be wanted: %+v", dune)
	}
	if _, found, _ := h.lib.GetByTMDBID(1111); found {
		t.Fatal("a movie whose folder wasn't found must not be added (it would be downloaded again)")
	}
	if _, err := os.Stat(wantInc); err != nil {
		t.Fatalf("files must stay where they are: %v", err)
	}

	// Series: episodes linked, Sonarr's season monitoring kept.
	bb, _, _ := h.lib.GetSeriesByTMDBID(1396)
	eps, _ := h.lib.ListEpisodes(bb.ID)
	downloaded := map[[2]int]string{}
	for _, e := range eps {
		if e.Status == library.StatusDownloaded {
			downloaded[[2]int{e.Season, e.Episode}] = e.Quality
		}
	}
	if len(downloaded) != 3 || downloaded[[2]int{1, 1}] != "HDTV-720p" || downloaded[[2]int{2, 1}] != "Unknown" {
		t.Fatalf("breaking bad episodes: %+v", downloaded)
	}
	old, _, _ := h.lib.GetSeriesByTMDBID(5555)
	oldEps, _ := h.lib.ListEpisodes(old.ID)
	for _, e := range oldEps {
		if e.Monitored != (e.Season == 1) {
			t.Fatalf("season monitoring not kept: %+v", e)
		}
	}
	if old.ProfileID != h.profiles["720p"] {
		t.Fatalf("old show profile: %d", old.ProfileID)
	}

	// Indexers: keys kept (encrypted at rest), failing test switched off.
	list, _ := h.idx.List()
	byName := map[string]indexers.Instance{}
	for _, inst := range list {
		byName[inst.Name] = inst
	}
	if g := byName["NZBgeek"]; g.APIKey != "geek-secret-key" || !g.Enabled || g.Kind != indexers.KindNewznab || g.BaseURL != "https://api.nzbgeek.info" {
		t.Fatalf("nzbgeek: %+v", g)
	}
	if j := byName["Jackett (all)"]; j.Enabled || j.Kind != indexers.KindTorznab || !strings.Contains(j.LastTestError, "jackett") {
		t.Fatalf("jackett should be added switched off with its test error: %+v", j)
	}
	if d := byName["DrunkenSlug"]; d.Enabled || d.APIKey != "" {
		t.Fatalf("masked key must not be copied and the indexer must be off: %+v", d)
	}
	if x := byName["1337x"]; x.Kind != indexers.KindCardigann || x.Settings["downloadlink"] != "magnet:" || x.Settings["sort"] != "size" || !x.Enabled {
		t.Fatalf("1337x: %+v", x)
	}
	if p := byName["PrivateSite"]; p.Enabled || p.Settings["username"] != "ryan" || p.Settings["password"] != "" {
		t.Fatalf("private site: %+v", p)
	}
	var raw string
	if err := h.db.QueryRow(`SELECT COALESCE(group_concat(api_key_encrypted, '|'), '') FROM indexers`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "geek-secret-key") {
		t.Fatal("indexer API key stored in plain text")
	}

	// Usenet servers: passwords encrypted, priority order kept.
	servers, _ := h.servers.ListAll()
	if len(servers) != 3 || servers[0].Config.Host != "news.eweka.nl" || servers[0].Config.Password != "eweka-password" || !servers[0].Enabled ||
		servers[0].Config.Connections != 20 || !servers[0].Config.UseSSL || servers[1].Priority != 1 || servers[1].Enabled || servers[2].Enabled {
		t.Fatalf("servers: %+v", servers)
	}
	if err := h.db.QueryRow(`SELECT COALESCE(group_concat(password_encrypted, '|'), '') FROM download_clients`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "eweka-password") {
		t.Fatal("server password stored in plain text")
	}

	// A second run adds nothing.
	st2, err := h.im.Run(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	r2 := st2.Results
	if r2.Movies.Added != 0 || r2.Movies.Existing != 4 || r2.Series.Added != 0 || r2.Series.Existing != 2 ||
		r2.Indexers.Added != 0 || r2.Indexers.Existing != 6 || r2.UsenetServers.Added != 0 || r2.UsenetServers.Existing != 3 || r2.Series.FilesLinked != 0 || r2.Movies.FilesLinked != 0 {
		t.Fatalf("second run changed something: %+v", r2)
	}
	if movies, _ := h.lib.List(); len(movies) != 4 {
		t.Fatalf("movies after two runs: %d", len(movies))
	}
	if shows, _ := h.lib.ListSeries(); len(shows) != 2 {
		t.Fatalf("series after two runs: %d", len(shows))
	}
	if list, _ := h.idx.List(); len(list) != 5 {
		t.Fatalf("indexers after two runs: %d", len(list))
	}
	if list, _ := h.servers.ListAll(); len(list) != 3 {
		t.Fatalf("servers after two runs: %d", len(list))
	}

	h.assertOnlyGET(t)
	for _, secret := range []string{radarrKey, sonarrKey, prowlarrKey, sabKey, "geek-secret-key", "eweka-password", "old-password"} {
		if strings.Contains(h.logs.String(), secret) {
			t.Fatalf("a secret (%s...) was logged", secret[:4])
		}
	}
}

func TestImportRespectsIncludeAndProfileMapping(t *testing.T) {
	h := newHarness(t)
	no, yes := false, true
	st, err := h.im.Run(context.Background(), Options{
		Sources:        h.sources(),
		Include:        Include{Movies: &yes, Series: &no, Indexers: &no, UsenetServers: &no},
		ProfileMapping: map[string]int64{"hd-1080p": h.profiles["Any"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.Series.Added+st.Results.Indexers.Added+st.Results.UsenetServers.Added != 0 {
		t.Fatalf("left-out parts were imported: %+v", st.Results)
	}
	inc, _, _ := h.lib.GetByTMDBID(27205)
	if inc.ProfileID != h.profiles["Any"] {
		t.Fatalf("profile mapping not applied: %d", inc.ProfileID)
	}
	// Apps not being imported aren't even read.
	h.sonarr.mu.Lock()
	n := len(h.sonarr.requests)
	h.sonarr.mu.Unlock()
	if n != 0 {
		t.Fatalf("sonarr was read although series weren't included: %d requests", n)
	}
}

func TestImportWithoutProfilesUsesDefault(t *testing.T) {
	h := newHarness(t)
	no := false
	if _, err := h.im.Run(context.Background(), Options{Sources: Sources{Radarr: h.sources().Radarr}, Include: Include{QualityProfiles: &no}}); err != nil {
		t.Fatal(err)
	}
	heat, _, _ := h.lib.GetByTMDBID(949)
	if heat.ProfileID != 0 {
		t.Fatalf("expected the default profile (0), got %d", heat.ProfileID)
	}
}

func TestStartRefusesASecondImport(t *testing.T) {
	h := newHarness(t)
	h.im.mu.Lock()
	h.im.status.Running = true
	h.im.mu.Unlock()
	if err := h.im.Start(Options{}); !errors.Is(err, ErrRunning) {
		t.Fatalf("expected ErrRunning, got %v", err)
	}
}

func TestConnectionErrors(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tests := []struct {
		name    string
		src     Sources
		app     func(Preview) AppStatus
		wantErr string
	}{
		{
			name:    "wrong Radarr key",
			src:     Sources{Radarr: &Conn{URL: h.radarr.URL, APIKey: "nope"}},
			app:     func(p Preview) AppStatus { return p.Radarr.AppStatus },
			wantErr: "the API key was not accepted",
		},
		{
			name:    "Sonarr address in the Radarr box",
			src:     Sources{Radarr: &Conn{URL: h.sonarr.URL, APIKey: sonarrKey}},
			app:     func(p Preview) AppStatus { return p.Radarr.AppStatus },
			wantErr: "this address is Sonarr, not Radarr",
		},
		{
			name:    "wrong SABnzbd key",
			src:     Sources{SABnzbd: &Conn{URL: h.sab.URL, APIKey: "0123456789abcdef0123456789abcdef"}},
			app:     func(p Preview) AppStatus { return p.SABnzbd.AppStatus },
			wantErr: "NZB Key",
		},
		{
			name:    "nothing listening",
			src:     Sources{SABnzbd: &Conn{URL: "http://127.0.0.1:1", APIKey: "secret-in-query"}},
			app:     func(p Preview) AppStatus { return p.SABnzbd.AppStatus },
			wantErr: "could not reach 127.0.0.1:1",
		},
		{
			name:    "no API key",
			src:     Sources{Prowlarr: &Conn{URL: h.prowlarr.URL}},
			app:     func(p Preview) AppStatus { return p.Prowlarr.AppStatus },
			wantErr: "enter the API key",
		},
		{
			name:    "not a web address",
			src:     Sources{Prowlarr: &Conn{URL: "ftp://nas", APIKey: "k"}},
			app:     func(p Preview) AppStatus { return p.Prowlarr.AppStatus },
			wantErr: "is not a web address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := h.im.Preview(ctx, Options{Sources: tt.src})
			if err != nil {
				t.Fatal(err)
			}
			st := tt.app(pv)
			if st.OK || !strings.Contains(st.Error, tt.wantErr) {
				t.Fatalf("got %+v, want error containing %q", st, tt.wantErr)
			}
			if strings.Contains(st.Error, "secret-in-query") || strings.Contains(st.Error, "apikey=") {
				t.Fatalf("error leaks the API key: %s", st.Error)
			}
		})
	}
}

func TestExplicitPathMapWins(t *testing.T) {
	h := newHarness(t)
	elsewhere := t.TempDir()
	touchFile(t, filepath.Join(elsewhere, "Inception (2010)", "Inception.2010.720p.BluRay.x264.mkv"))
	pv, err := h.im.Preview(context.Background(), Options{
		Sources: Sources{Radarr: h.sources().Radarr},
		PathMap: []PathMapping{{From: "/data/media/movies/", To: elsewhere}, {From: "", To: "/ignored"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.SuggestedPathMap) != 0 || len(pv.PathMap) != 1 {
		t.Fatalf("an explicit mapping must not be second-guessed: %+v / %+v", pv.PathMap, pv.SuggestedPathMap)
	}
	inc := titleByName(t, pv.Radarr.Items, "Inception")
	if inc.Path != filepath.Join(elsewhere, "Inception (2010)") || !inc.FolderFound || inc.InLibraryFolder || !strings.Contains(inc.Reason, "outside Mediarium's movies folder") {
		t.Fatalf("inception: %+v", inc)
	}
	if heat := titleByName(t, pv.Radarr.Items, "Heat"); heat.Action != ActionSkip || !strings.Contains(heat.Reason, "folder not found at "+filepath.Join(elsewhere, "Heat (1995)")) {
		t.Fatalf("heat: %+v", heat)
	}
}
