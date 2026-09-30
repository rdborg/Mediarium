package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

// fakeMusicPlex is a stand-in for Plex with a movie library and a music
// library (a section of type "artist") at /data/music. It records the paths
// it is asked to scan, per section.
type fakeMusicPlex struct {
	*httptest.Server
	mu    sync.Mutex
	scans []string
}

func newFakeMusicPlex(t *testing.T) *fakeMusicPlex {
	t.Helper()
	f := &fakeMusicPlex{}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": v})
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			write(w, map[string]any{"machineIdentifier": "plex-machine-2", "version": "1.41.0"})
			return
		}
		if r.Header.Get("X-Plex-Token") != "plex-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/":
			write(w, map[string]any{"friendlyName": "Living Room"})
		case "/library/sections":
			write(w, map[string]any{"Directory": []map[string]any{
				{"key": "1", "type": "movie", "title": "Movies", "Location": []map[string]any{{"path": "/data/movies"}}},
				{"key": "2", "type": "artist", "title": "Music", "Location": []map[string]any{{"path": "/data/music"}}},
			}})
		case "/library/sections/1/refresh", "/library/sections/2/refresh":
			f.mu.Lock()
			f.scans = append(f.scans, r.URL.Path[len("/library/sections/"):len("/library/sections/")+1]+" "+r.URL.Query().Get("path"))
			f.mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMusicPlex) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.scans...)
}

func TestAlbumImportRefreshesThePlexMusicLibrary(t *testing.T) {
	e := newMusicEnv(t)
	plex := newFakeMusicPlex(t)
	postJSON[map[string]any](t, e.client, e.base+"/api/media-servers", map[string]any{
		"kind": "plex", "baseUrl": plex.URL, "token": "plex-token",
		"pathMap": []map[string]string{{"from": e.musicRoot, "to": "/data/music"}},
	}, http.StatusCreated)

	e.useLosslessByDefault(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	albumURL := fmt.Sprintf("%s/api/music/albums/%v", e.base, first["id"])
	file := func(track int, title string) nntpArticle {
		return nntpArticle{fileName: fmt.Sprintf("t%d.flac", track), content: flacFile(44100, 16, 200, fmt.Sprintf("TRACKNUMBER=%d", track), "TITLE="+title)}
	}
	articles := map[string]nntpArticle{"a@x": file(1, "Opening"), "b@x": file(2, "Middle Song"), "c@x": file(3, "Closing Time")}
	host, port := newArticleNNTPServer(t, articles)
	idx := newMusicIndexer(t, []string{"Fixture Band - First Album (1999) [FLAC]"}, nzbFor(articles))
	postJSON[map[string]any](t, e.client, e.base+"/api/indexers", map[string]any{"name": "Music Indexer", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	if now := postJSON[map[string]any](t, e.client, albumURL+"/search-now", nil, http.StatusOK); now["grabbed"] != float64(1) {
		t.Fatalf("search now: %v", now)
	}
	waitBackground(t, e.server)
	e.server.TestFlushMediaServerRefresh()

	want := "2 " + filepath.ToSlash("/data/music/Fixture Band/First Album (1999)")
	if got := plex.seen(); len(got) != 1 || got[0] != want {
		t.Fatalf("plex scans = %q, want [%q]: the album folder, in the music library, once", got, want)
	}
}
