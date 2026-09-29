package mediaservers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakePlex is a local stand-in for a Plex server: /identity is open,
// everything else needs the token. It records every refresh request.
type fakePlex struct {
	srv      *httptest.Server
	token    string
	machine  string
	xml      bool // answer XML, as Plex does without Accept: application/json
	sections []Library
	items    map[string][]map[string]any // section id -> Metadata entries

	mu        sync.Mutex
	refreshes []string // "sectionID path" ("sectionID" alone for a full scan)
	listCalls int
}

func newFakePlex(t *testing.T, token string) *fakePlex {
	t.Helper()
	f := &fakePlex{token: token, machine: "abc123machine", items: map[string][]map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePlex) server() Server {
	return Server{ID: 1, Name: "Plex", Kind: KindPlex, BaseURL: f.srv.URL, Token: f.token, Enabled: true, RefreshAfterImport: true}
}

func (f *fakePlex) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/identity" {
		f.write(w, map[string]any{"machineIdentifier": f.machine, "version": "1.41.0"}, nil)
		return
	}
	if r.Header.Get("X-Plex-Token") != f.token || r.URL.Query().Get("X-Plex-Token") != "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/":
		f.write(w, map[string]any{"friendlyName": "Living Room"}, nil)
	case r.URL.Path == "/library/sections":
		var dirs []map[string]any
		for _, s := range f.sections {
			var locs []map[string]any
			for _, l := range s.Locations {
				locs = append(locs, map[string]any{"path": l})
			}
			dirs = append(dirs, map[string]any{"key": s.ID, "type": s.Type, "title": s.Title, "Location": locs})
		}
		f.write(w, map[string]any{"Directory": dirs}, nil)
	case strings.HasSuffix(r.URL.Path, "/refresh"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/library/sections/"), "/refresh")
		entry := id
		if p := r.URL.Query().Get("path"); p != "" {
			entry += " " + p
		}
		f.mu.Lock()
		f.refreshes = append(f.refreshes, entry)
		f.mu.Unlock()
	case strings.HasSuffix(r.URL.Path, "/all"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/library/sections/"), "/all")
		f.mu.Lock()
		f.listCalls++
		f.mu.Unlock()
		f.write(w, map[string]any{"Metadata": f.items[id]}, f.items[id])
	default:
		http.NotFound(w, r)
	}
}

// write answers JSON, or XML when f.xml is set (items become <Video> or,
// for shows, <Directory> elements).
func (f *fakePlex) write(w http.ResponseWriter, container map[string]any, items []map[string]any) {
	if !f.xml {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": container})
		return
	}
	w.Header().Set("Content-Type", "text/xml;charset=utf-8")
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<MediaContainer")
	for _, k := range []string{"machineIdentifier", "version", "friendlyName"} {
		if v, ok := container[k]; ok {
			fmt.Fprintf(&b, " %s=%q", k, v)
		}
	}
	b.WriteString(">")
	if dirs, ok := container["Directory"].([]map[string]any); ok {
		for _, d := range dirs {
			fmt.Fprintf(&b, `<Directory key=%q type=%q title=%q>`, d["key"], d["type"], d["title"])
			if locs, ok := d["Location"].([]map[string]any); ok {
				for _, l := range locs {
					fmt.Fprintf(&b, `<Location path=%q/>`, l["path"])
				}
			}
			b.WriteString("</Directory>")
		}
	}
	for _, it := range items {
		tag := "Video"
		if it["type"] == "show" {
			tag = "Directory"
		}
		fmt.Fprintf(&b, `<%s ratingKey=%q type=%q guid=%q>`, tag, it["ratingKey"], it["type"], it["guid"])
		if guids, ok := it["Guid"].([]map[string]any); ok {
			for _, g := range guids {
				fmt.Fprintf(&b, `<Guid id=%q/>`, g["id"])
			}
		}
		fmt.Fprintf(&b, "</%s>", tag)
	}
	b.WriteString("</MediaContainer>")
	_, _ = io.WriteString(w, b.String())
}

// fakeEmby stands in for Jellyfin or Emby.
type fakeEmby struct {
	srv         *httptest.Server
	kind        Kind
	key         string
	product     string // ProductName in /System/Info
	serverID    string
	headerOnly  bool // accept only the Authorization header (Jellyfin with legacy auth off)
	noMediaScan bool // /Library/Media/Updated missing (old version)
	items       []embyItem

	mu        sync.Mutex
	updated   []string // paths sent to /Library/Media/Updated
	refreshes int      // calls to /Library/Refresh
	itemCalls int
}

func newFakeEmby(t *testing.T, kind Kind, key string) *fakeEmby {
	t.Helper()
	f := &fakeEmby{kind: kind, key: key, serverID: "srv-" + string(kind)}
	if kind == KindJellyfin {
		f.product = "Jellyfin Server"
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEmby) server() Server {
	return Server{ID: 2, Name: f.kind.Label(), Kind: f.kind, BaseURL: f.srv.URL, Token: f.key, Enabled: true, RefreshAfterImport: true}
}

func (f *fakeEmby) authorized(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Authorization"), `Token="`+f.key+`"`) {
		return true
	}
	return !f.headerOnly && r.Header.Get("X-Emby-Token") == f.key
}

func (f *fakeEmby) serve(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(r) {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/System/Info":
		info := map[string]any{"ServerName": "Den", "Version": "10.10.0", "Id": f.serverID}
		if f.product != "" {
			info["ProductName"] = f.product
		}
		_ = json.NewEncoder(w).Encode(info)
	case r.URL.Path == "/Library/VirtualFolders":
		_ = json.NewEncoder(w).Encode([]map[string]any{{"Name": "Movies", "Locations": []string{"/media/movies"}, "CollectionType": "movies", "ItemId": "lib1"}})
	case r.URL.Path == "/Library/Media/Updated" && r.Method == http.MethodPost:
		if f.noMediaScan {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Updates []embyMediaUpdate `json:"Updates"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		for _, u := range body.Updates {
			f.updated = append(f.updated, u.Path)
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/Library/Refresh" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.refreshes++
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/Items":
		f.mu.Lock()
		f.itemCalls++
		f.mu.Unlock()
		q := r.URL.Query()
		want := strings.TrimPrefix(q.Get("AnyProviderIdEquals"), "tmdb.")
		var out []embyItem
		for _, it := range f.items {
			if it.Type == q.Get("IncludeItemTypes") && providerID(it.ProviderIDs, "tmdb") == want {
				out = append(out, it)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Items": out, "TotalRecordCount": len(out)})
	default:
		http.NotFound(w, r)
	}
}
