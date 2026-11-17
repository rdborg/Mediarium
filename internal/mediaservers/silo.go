package mediaservers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// KindSilo is Silo (siloserver.org), a self-hosted media server. Mediarium
// talks to its own API at the address you open it at, with an administrator's
// API key (Admin > API keys) sent as "Authorization: Bearer sa_…". Silo also
// has a Jellyfin-compatible port, but that one has no full-library scan and
// can't look titles up by their TMDB id, so it isn't used.
//
// Current Silo builds answer every /api/v1 call with 410 Gone and moved the
// API to /api/v2 (docs/scan-api.md, docs/admin-system-api.md and
// contracts/api/v2/openapi.json in github.com/silo-server/silo-server).
// Mediarium tries v2 first and falls back to v1 for older builds, see siloTry.
//
// Confirmed in the v2 contract: GET /api/v2/admin/system/build, GET
// /api/v2/libraries ({items, page}, string ids) and POST /api/v2/scan
// (library_id as a string, or path), all with the same Bearer key and no
// profile header for an API key. Not confirmed, so it stays on v1: the catalog
// lookup behind "Watch in Silo" (v2 GET /api/v2/catalog/items/{id} needs an
// X-Profile-Id and its content ids are not documented in the movie-tmdb-...
// form). Mediarium has no watched sync for Silo.
const KindSilo Kind = "silo"

// siloID is a library id: a number on Silo's v1 API, a string on v2.
type siloID string

func (i *siloID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*i = siloID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*i = siloID(n.String())
	return nil
}

type siloLibrary struct {
	ID      siloID   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Paths   []string `json:"paths"`
	Enabled bool     `json:"enabled"`
}

type siloBuild struct {
	Display     string `json:"display"`
	BuildNumber int    `json:"build_number"`
}

// siloLibraries lists the libraries over whichever API the server speaks.
func (c *Client) siloLibraries(ctx context.Context, s Server) ([]siloLibrary, error) {
	var v1 []siloLibrary
	var v2 struct {
		Items []siloLibrary `json:"items"`
	}
	ver, err := c.siloTry(ctx, s, siloCall{
		method: http.MethodGet,
		v1:     siloReq{path: "/api/v1/libraries/", out: &v1},
		v2:     siloReq{path: "/api/v2/libraries", out: &v2},
	})
	if err != nil {
		return nil, explain(s, err)
	}
	if ver == siloV2 {
		return v2.Items, nil
	}
	return v1, nil
}

func (c *Client) siloTest(ctx context.Context, s Server) (TestResult, error) {
	var build siloBuild
	_, err := c.siloTry(ctx, s, siloCall{
		method: http.MethodGet,
		v1:     siloReq{path: "/api/v1/admin/system/build", out: &build},
		v2:     siloReq{path: "/api/v2/admin/system/build", out: &build},
	})
	if errors.Is(err, errNotFound) {
		// The Jellyfin-compatible port answers /System/Info but not Silo's own API.
		var info embySystemInfo
		if c.do(ctx, s, http.MethodGet, "/System/Info/Public", nil, nil, &info) == nil && info.ID != "" {
			return TestResult{}, userErr(err, "That is Silo's Jellyfin-compatible port. Use the address you open Silo at in your browser (port 8090 unless you changed it).")
		}
	}
	if err != nil {
		if errors.Is(err, errUnauthorized) && s.Token != "" {
			return TestResult{}, userErr(err, "Silo refused the API key. It must be a key of an administrator (Admin > API keys), copied completely.")
		}
		return TestResult{}, explain(s, err)
	}
	libs, err := c.siloLibraries(ctx, s)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{ServerName: "Silo", Libraries: []Library{}}
	switch {
	case build.BuildNumber > 0:
		res.Version = "build " + strconv.Itoa(build.BuildNumber)
	case build.Display != "":
		res.Version = build.Display
	}
	for _, l := range libs {
		locs := l.Paths
		if locs == nil {
			locs = []string{}
		}
		res.Libraries = append(res.Libraries, Library{ID: string(l.ID), Title: l.Name, Type: l.Type, Locations: locs})
	}
	return res, nil
}

func (c *Client) siloScanLibrary(ctx context.Context, s Server, id siloID) error {
	// v1 wants the id as a number, v2 as a string.
	var v1ID any = string(id)
	if n, err := strconv.Atoi(string(id)); err == nil {
		v1ID = n
	}
	_, err := c.siloTry(ctx, s, siloCall{
		method: http.MethodPost,
		v1:     siloReq{path: "/api/v1/scan", body: map[string]any{"library_id": v1ID}},
		v2:     siloReq{path: "/api/v2/scan", body: map[string]any{"library_id": string(id)}},
	})
	return explain(s, err)
}

// siloScanPath asks Silo to scan one folder.
func (c *Client) siloScanPath(ctx context.Context, s Server, p string) error {
	body := map[string]any{"path": p}
	_, err := c.siloTry(ctx, s, siloCall{
		method: http.MethodPost,
		v1:     siloReq{path: "/api/v1/scan", body: body},
		v2:     siloReq{path: "/api/v2/scan", body: body},
	})
	return explain(s, err)
}

func (c *Client) siloRefreshAll(ctx context.Context, s Server) error {
	libs, err := c.siloLibraries(ctx, s)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if !l.Enabled {
			continue
		}
		if err := c.siloScanLibrary(ctx, s, l.ID); err != nil {
			return err
		}
	}
	return nil
}

// siloRefreshFolders asks Silo to scan just the folders that changed. A movie
// or show folder outside every library gets a scan of all of them instead,
// as with Jellyfin and Emby: Silo would refuse the folder and the new title
// would never show up. Book folders outside every library are left alone.
func (c *Client) siloRefreshFolders(ctx context.Context, s Server, kind MediaKind, folders []string) ([]string, error) {
	libs, err := c.siloLibraries(ctx, s)
	if err != nil {
		return nil, err
	}
	var locations []string
	for _, l := range libs {
		if l.Enabled {
			locations = append(locations, l.Paths...)
		}
	}
	var done []string
	seen := map[string]bool{}
	outside := ""
	for _, f := range folders {
		p := MapPath(f, s.PathMap)
		if seen[p] {
			continue
		}
		seen[p] = true
		if !insideAny(p, locations) {
			if outside == "" {
				outside = p
			}
			continue
		}
		if err := c.siloScanPath(ctx, s, p); err != nil {
			return done, err
		}
		done = append(done, p)
	}
	if outside != "" && kind != MediaBook {
		slog.Warn("media server: a new folder is not inside any of the server's libraries, so the whole library is scanned. Add a folder mapping for it in the media server settings.", "server", s.Name, "folder", outside)
		if err := c.siloRefreshAll(ctx, s); err != nil {
			return done, err
		}
		done = append(done, "whole library (a new folder is not inside any of this server's libraries)")
	}
	return done, nil
}

// siloIDs are the ids Silo may file a title under, best first. Silo names a
// movie after its TMDB id and a show after its TheTVDB id, falling back to
// TMDB and then IMDb ids.
func (c *Client) siloIDs(kind MediaKind, tmdbID int) []string {
	switch kind {
	case MediaMovie:
		return []string{"movie-tmdb-" + strconv.Itoa(tmdbID)}
	case MediaTV:
		var ids []string
		tvdb, imdb := 0, ""
		if c.SeriesIDs != nil {
			tvdb, imdb = c.SeriesIDs(tmdbID)
		}
		if tvdb > 0 {
			ids = append(ids, "series-tvdb-"+strconv.Itoa(tvdb))
		}
		ids = append(ids, "series-tmdb-"+strconv.Itoa(tmdbID))
		if imdb = strings.TrimSpace(imdb); imdb != "" {
			ids = append(ids, "series-imdb-"+imdb)
		}
		return ids
	}
	return nil
}

func (c *Client) siloFind(ctx context.Context, s Server, kind MediaKind, tmdbID int) (Item, bool, error) {
	for _, id := range c.siloIDs(kind, tmdbID) {
		var item struct {
			ContentID string `json:"content_id"`
		}
		err := c.do(ctx, s, http.MethodGet, "/api/v1/catalog/items/"+url.PathEscape(id), nil, nil, &item)
		if errors.Is(err, errNotFound) {
			continue
		}
		var se *statusError
		if errors.As(err, &se) && se.code == http.StatusGone {
			// A current Silo build has retired this v1 lookup and its v2 one is
			// not confirmed (see KindSilo), so there is no link to offer.
			slog.Debug("media server: this Silo build no longer answers the title lookup", "server", s.Name)
			return Item{}, false, nil
		}
		if err != nil {
			return Item{}, false, explain(s, err)
		}
		if item.ContentID == "" {
			item.ContentID = id
		}
		return Item{ID: item.ContentID, URL: s.WebURL() + "/item/" + url.PathEscape(item.ContentID)}, true, nil
	}
	return Item{}, false, nil
}

// Silo API generations.
const (
	siloV1 = 1
	siloV2 = 2
)

// siloReq is one request on one API generation.
type siloReq struct {
	path string
	body any
	out  any
}

// siloCall is the same operation on both API generations.
type siloCall struct {
	method string
	v1, v2 siloReq
}

// siloTry runs a call on the API generation this server last answered on, or
// v2 when that is not known yet, and on the other generation when the first
// says 404 or 410 (Gone: current builds retire v1 that way, old builds have
// no v2). A 401 or any other answer is final: it means the route exists. The
// generation that worked is remembered per server address for as long as
// Mediarium runs, so a server that was updated or downgraded is found again
// through the fallback. It returns the generation that answered.
func (c *Client) siloTry(ctx context.Context, s Server, call siloCall) (int, error) {
	order := [2]int{siloV2, siloV1}
	if v, ok := c.siloAPI.Load(s.BaseURL); ok && v.(int) == siloV1 {
		order = [2]int{siloV1, siloV2}
	}
	var err error
	for _, ver := range order {
		r := call.v2
		if ver == siloV1 {
			r = call.v1
		}
		err = c.do(ctx, s, call.method, r.path, nil, r.body, r.out)
		if err == nil {
			c.siloAPI.Store(s.BaseURL, ver)
			return ver, nil
		}
		var se *statusError
		if !errors.Is(err, errNotFound) && !(errors.As(err, &se) && se.code == http.StatusGone) {
			return ver, err
		}
	}
	return 0, err
}
