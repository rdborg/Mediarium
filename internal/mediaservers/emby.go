package mediaservers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Jellyfin started as a fork of Emby and both still share most of the API
// used here, so one implementation serves both, with the differences noted.

type embySystemInfo struct {
	ServerName  string `json:"ServerName"`
	Version     string `json:"Version"`
	ID          string `json:"Id"`
	ProductName string `json:"ProductName"` // "Jellyfin Server" on Jellyfin; absent on Emby
}

type embyVirtualFolder struct {
	Name           string   `json:"Name"`
	Locations      []string `json:"Locations"`
	CollectionType string   `json:"CollectionType"`
	ItemID         string   `json:"ItemId"`
}

type embyItem struct {
	ID          string            `json:"Id"`
	Name        string            `json:"Name"`
	ServerID    string            `json:"ServerId"`
	Type        string            `json:"Type"`
	ProviderIDs map[string]string `json:"ProviderIds"`
}

type embyItems struct {
	Items []embyItem `json:"Items"`
}

func (c *Client) embyInfo(ctx context.Context, s Server) (embySystemInfo, error) {
	var info embySystemInfo
	if err := c.do(ctx, s, http.MethodGet, "/System/Info", nil, nil, &info); err != nil {
		return embySystemInfo{}, explain(s, err)
	}
	if info.ID == "" || info.Version == "" {
		return embySystemInfo{}, explain(s, errUnrecognised)
	}
	isJellyfin := strings.Contains(strings.ToLower(info.ProductName), "jellyfin")
	switch {
	case s.Kind == KindEmby && isJellyfin:
		return embySystemInfo{}, userErr(nil, "This is a Jellyfin server. Choose Jellyfin as the server type.")
	case s.Kind == KindJellyfin && info.ProductName != "" && !isJellyfin:
		return embySystemInfo{}, userErr(nil, "This looks like an Emby server (%s). Choose Emby as the server type.", info.ProductName)
	}
	return info, nil
}

func (c *Client) embyTest(ctx context.Context, s Server) (TestResult, error) {
	info, err := c.embyInfo(ctx, s)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{ServerName: info.ServerName, Version: info.Version, ServerID: info.ID, Libraries: []Library{}}
	var folders []embyVirtualFolder
	if err := c.do(ctx, s, http.MethodGet, "/Library/VirtualFolders", nil, nil, &folders); err == nil {
		for _, f := range folders {
			locs := f.Locations
			if locs == nil {
				locs = []string{}
			}
			res.Libraries = append(res.Libraries, Library{ID: f.ItemID, Title: f.Name, Type: f.CollectionType, Locations: locs})
		}
	}
	return res, nil
}

type embyMediaUpdate struct {
	Path       string `json:"Path"`
	UpdateType string `json:"UpdateType"`
}

// embyLibraryLocations lists the folders the server's libraries hold, or nil
// when it can't say (then nothing is assumed about them).
func (c *Client) embyLibraryLocations(ctx context.Context, s Server) []string {
	var folders []embyVirtualFolder
	if err := c.do(ctx, s, http.MethodGet, "/Library/VirtualFolders", nil, nil, &folders); err != nil {
		return nil
	}
	locations := []string{}
	for _, f := range folders {
		locations = append(locations, f.Locations...)
	}
	return locations
}

// insideAny reports whether path is one of dirs or inside one.
func insideAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if _, ok := within(path, d); ok {
			return true
		}
	}
	return false
}

// embyRefreshFolders tells the server which folders changed; it then scans
// just those. A server that won't take that (an old version) gets a full
// library scan instead, and so does a folder that is not inside any of the
// server's libraries: the server would take the message and do nothing with
// it, so the new title would never show up. That happens when the server
// sees the files under another name and no folder mapping says so.
func (c *Client) embyRefreshFolders(ctx context.Context, s Server, folders []string) ([]string, error) {
	var updates []embyMediaUpdate
	var done []string
	var unknown []string
	locations := c.embyLibraryLocations(ctx, s)
	seen := map[string]bool{}
	for _, f := range folders {
		p := MapPath(f, s.PathMap)
		if seen[p] {
			continue
		}
		seen[p] = true
		if locations != nil && !insideAny(p, locations) {
			unknown = append(unknown, p)
			continue
		}
		updates = append(updates, embyMediaUpdate{Path: p, UpdateType: "Created"})
		done = append(done, p)
	}
	if len(unknown) > 0 {
		slog.Warn("media server: a new folder is not inside any of the server's libraries, so the whole library is scanned. Add a folder mapping for it in the media server settings.", "server", s.Name, "folder", unknown[0])
		if err := c.do(ctx, s, http.MethodPost, "/Library/Refresh", nil, nil, nil); err != nil {
			return nil, explain(s, err)
		}
		done = append(done, "whole library (a new folder is not inside any of this server's libraries)")
	}
	if len(updates) == 0 {
		return done, nil
	}
	err := c.do(ctx, s, http.MethodPost, "/Library/Media/Updated", nil, map[string]any{"Updates": updates}, nil)
	if err == nil {
		return done, nil
	}
	var ue *UserError
	if errors.Is(err, errUnauthorized) || errors.As(err, &ue) {
		return nil, explain(s, err)
	}
	if err := c.do(ctx, s, http.MethodPost, "/Library/Refresh", nil, nil, nil); err != nil {
		return nil, explain(s, err)
	}
	return []string{"whole library (this server did not accept a folder scan)"}, nil
}

func embyItemType(kind MediaKind) string {
	switch kind {
	case MediaTV:
		return "Series"
	case MediaMusic:
		return "MusicAlbum"
	}
	return "Movie"
}

// providerID reads an id from ProviderIds, whose key case varies ("Tmdb").
func providerID(ids map[string]string, name string) string {
	for k, v := range ids {
		if strings.EqualFold(k, name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (c *Client) embyFind(ctx context.Context, s Server, kind MediaKind, tmdbID int) (Item, bool, error) {
	want := strconv.Itoa(tmdbID)
	q := url.Values{
		"Recursive":           {"true"},
		"IncludeItemTypes":    {embyItemType(kind)},
		"AnyProviderIdEquals": {"tmdb." + want},
		"HasTmdbId":           {"true"},
		"Fields":              {"ProviderIds"},
		"EnableImages":        {"false"},
		"EnableUserData":      {"false"},
	}
	var res embyItems
	if err := c.do(ctx, s, http.MethodGet, "/Items", q, nil, &res); err != nil {
		return Item{}, false, explain(s, err)
	}
	// The id is checked again here: a server version that ignores the
	// provider filter returns everything, and only the match counts.
	for _, it := range res.Items {
		if it.ID == "" || providerID(it.ProviderIDs, "Tmdb") != want {
			continue
		}
		serverID := it.ServerID
		if serverID == "" {
			serverID = s.ServerID
		}
		return embyLink(s, it.ID, serverID), true, nil
	}
	return Item{}, false, nil
}

func embyLink(s Server, itemID, serverID string) Item {
	if s.Kind == KindEmby {
		u := s.WebURL() + "/web/index.html#!/item?id=" + url.QueryEscape(itemID)
		if serverID != "" {
			u += "&serverId=" + url.QueryEscape(serverID)
		}
		return Item{ID: itemID, URL: u}
	}
	u := s.WebURL() + "/web/#/details?id=" + url.QueryEscape(itemID)
	if serverID != "" {
		u += "&serverId=" + url.QueryEscape(serverID)
	}
	return Item{ID: itemID, URL: u}
}
