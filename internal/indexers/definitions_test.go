package indexers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefinitionStoreDownloadsAndCaches(t *testing.T) {
	archive := tarGz(t, map[string][]byte{
		"README.md":                            []byte("readme"),
		"definitions/v10/fixturepublic.yml":    []byte("id: fixturepublic\nname: Old Version\nlinks: [https://old.test/]\n"),
		"definitions/v11/fixturepublic.yml":    fixtureDef(t, "fixturepublic"),
		"definitions/v11/fixtureprivate.yml":   fixtureDef(t, "fixtureprivate"),
		"definitions/v11/broken.yml":           []byte("id: [unclosed\n"),
		"definitions/v11/schema.json":          []byte("{}"),
		"definitions/v11/unsupported.yml":      []byte("id: unsupported\nname: Needs XPath\nlinks: [https://u.test/]\nsearch:\n  paths: [{path: x}]\n  rows: {selector: 'tr:xpath(1)'}\n  fields: {title: {selector: a}}\n"),
		"definitions/v12/fixturepublic.yml":    []byte("id: fixturepublic\nname: Future Version\n"),
		"definitions/v11/../../escape.yml":     []byte("id: escape\n"),
		"definitions/v11/renamed-new-name.yml": []byte("id: renamed-new-name\nname: Renamed\nreplaces: [old-name]\nlinks: [https://r.test/]\nsearch:\n  paths: [{path: x}]\n  rows: {selector: tr}\n  fields: {title: {selector: a}}\n"),
	})
	srv, hits := archiveServer(t, archive)
	dir := filepath.Join(t.TempDir(), "indexer-definitions")
	store := NewDefinitionStore(dir)
	store.SourceURL = srv.URL

	idx, err := store.Catalogue(context.Background(), false)
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	if hits.Load() != 1 || idx.SchemaVersion != 11 {
		t.Fatalf("hits=%d schema=%d", hits.Load(), idx.SchemaVersion)
	}
	byID := map[string]DefinitionSummary{}
	for _, d := range idx.Definitions {
		byID[d.ID] = d
	}
	if len(byID) != 5 {
		t.Fatalf("expected 5 definitions (v11 only, no escaping paths), got %v", idx.Definitions)
	}
	pub := byID["fixturepublic"]
	if pub.Name != "Fixture Public" || pub.Type != "public" || pub.Protocol != "torrent" || !pub.Supported || pub.Language != "en-US" {
		t.Errorf("unexpected summary %+v", pub)
	}
	if len(pub.Links) != 1 || pub.Links[0] != "https://public.fixture.test/" {
		t.Errorf("links = %v", pub.Links)
	}
	var sortSetting, infoSetting, checkbox SettingSummary
	for _, s := range pub.Settings {
		switch s.Name {
		case "sort":
			sortSetting = s
		case "info_flaresolverr":
			infoSetting = s
		case "disablesort":
			checkbox = s
		}
	}
	if sortSetting.Type != "select" || sortSetting.Default != "time" || len(sortSetting.Options) != 2 ||
		sortSetting.Options[0] != (SettingOption{Value: "time", Label: "created"}) {
		t.Errorf("select setting = %+v", sortSetting)
	}
	if infoSetting.Label == "" || !strings.Contains(infoSetting.Default.(string), "FlareSolverr") {
		t.Errorf("predefined info setting should be filled in: %+v", infoSetting)
	}
	if checkbox.Default != false {
		t.Errorf("checkbox default should be a bool: %#v", checkbox.Default)
	}
	if b := byID["broken"]; b.Supported || !strings.Contains(b.Problem, "can't be read") {
		t.Errorf("broken file should be listed as unusable: %+v", b)
	}
	if u := byID["unsupported"]; u.Supported || !strings.Contains(u.Problem, "xpath") {
		t.Errorf("unsupported definition should say why: %+v", u)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Errorf("index.json not written: %v", err)
	}

	// cached: no new download
	if _, err := store.Catalogue(context.Background(), false); err != nil || hits.Load() != 1 {
		t.Fatalf("expected the cache to be used (hits=%d, err=%v)", hits.Load(), err)
	}
	// a fresh store on the same folder reads the cache from disk
	store2 := NewDefinitionStore(dir)
	store2.SourceURL = srv.URL
	if idx2, err := store2.Catalogue(context.Background(), false); err != nil || len(idx2.Definitions) != 5 || hits.Load() != 1 {
		t.Fatalf("disk cache not used: hits=%d err=%v", hits.Load(), err)
	}

	// Load parses and validates; renamed definitions are found by their old id
	def, err := store.Load(context.Background(), "fixtureprivate")
	if err != nil || def.Login == nil || def.Login.Method != "form" {
		t.Fatalf("load: %v", err)
	}
	if def, err := store.Load(context.Background(), "old-name"); err != nil || def.ID != "renamed-new-name" {
		t.Fatalf("load by replaced id: %v", err)
	}
	if _, err := store.Load(context.Background(), "unsupported"); !errors.Is(err, ErrUnsupportedDefinition) {
		t.Errorf("expected ErrUnsupportedDefinition, got %v", err)
	}
	if _, err := store.Load(context.Background(), "nope"); !errors.Is(err, ErrDefinitionNotFound) {
		t.Errorf("expected ErrDefinitionNotFound, got %v", err)
	}

	// a forced refresh downloads again (once the minimum gap has passed)
	gen := store.Generation()
	store.lastAttempt = time.Time{}
	time.Sleep(10 * time.Millisecond)
	if _, err := store.Catalogue(context.Background(), true); err != nil || hits.Load() != 2 {
		t.Fatalf("forced refresh: hits=%d err=%v", hits.Load(), err)
	}
	if store.Generation() == gen {
		t.Error("generation should change after a refresh")
	}
	// ...but not twice within a minute
	if _, err := store.Catalogue(context.Background(), true); err != nil || hits.Load() != 2 {
		t.Fatalf("refresh should be rate limited: hits=%d err=%v", hits.Load(), err)
	}

	// stale cache refreshes automatically when the list is opened
	store.MaxAge = time.Millisecond
	store.lastAttempt = time.Time{}
	time.Sleep(5 * time.Millisecond)
	if _, err := store.Catalogue(context.Background(), false); err != nil || hits.Load() != 3 {
		t.Fatalf("stale refresh: hits=%d err=%v", hits.Load(), err)
	}
}

func TestDefinitionStoreZipAndFailures(t *testing.T) {
	zipped := zipArchive(t, map[string][]byte{"definitions/v11/fixturejson.yml": fixtureDef(t, "fixturejson")})
	srv, _ := archiveServer(t, zipped)
	store := NewDefinitionStore(filepath.Join(t.TempDir(), "defs"))
	store.SourceURL = srv.URL
	idx, err := store.Catalogue(context.Background(), false)
	if err != nil || len(idx.Definitions) != 1 || idx.Definitions[0].ID != "fixturejson" {
		t.Fatalf("zip archive: %v %+v", err, idx)
	}

	// a failed refresh keeps the cached copy and reports the error
	srv.Close()
	store.lastAttempt = time.Time{}
	idx, err = store.Catalogue(context.Background(), true)
	if err == nil || idx == nil || len(idx.Definitions) != 1 {
		t.Fatalf("expected cached list plus an error, got %v %v", idx, err)
	}

	// no cache and no network: an error
	empty := NewDefinitionStore(filepath.Join(t.TempDir(), "none"))
	empty.SourceURL = srv.URL
	if _, err := empty.Catalogue(context.Background(), false); err == nil {
		t.Fatal("expected an error without cache or network")
	}

	// garbage and empty archives are rejected
	for _, body := range [][]byte{[]byte("not an archive"), tarGz(t, map[string][]byte{"README.md": []byte("x")})} {
		bad, _ := archiveServer(t, body)
		s := NewDefinitionStore(filepath.Join(t.TempDir(), "bad"))
		s.SourceURL = bad.URL
		if _, err := s.Catalogue(context.Background(), false); err == nil {
			t.Error("expected an error for a bad archive")
		}
	}
}

func TestExtractDefinitionsPicksSupportedVersion(t *testing.T) {
	archive := tarGz(t, map[string][]byte{
		"definitions/v12/a.yml": []byte("id: a"),
		"definitions/v13/a.yml": []byte("id: a"),
	})
	files, v, err := extractDefinitions(archive, 11)
	if err != nil || v != 12 || len(files) != 1 {
		t.Fatalf("only newer versions: expected the oldest (12), got v%d %v", v, err)
	}
}
