package mediaservers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Plex answers JSON when asked (Accept: application/json) and XML otherwise;
// these types read both. In XML, movies come back as <Video> and shows and
// sections as <Directory>.

type plexLocation struct {
	Path string `json:"path" xml:"path,attr"`
}

type plexGuid struct {
	ID string `json:"id" xml:"id,attr"`
}

type plexEntry struct {
	Key       string         `json:"key" xml:"key,attr"`
	RatingKey string         `json:"ratingKey" xml:"ratingKey,attr"`
	Type      string         `json:"type" xml:"type,attr"`
	Title     string         `json:"title" xml:"title,attr"`
	GUID      string         `json:"guid" xml:"guid,attr"`
	Guids     []plexGuid     `json:"Guid" xml:"Guid"`
	Locations []plexLocation `json:"Location" xml:"Location"`
}

type plexContainer struct {
	MachineIdentifier string      `json:"machineIdentifier" xml:"machineIdentifier,attr"`
	Version           string      `json:"version" xml:"version,attr"`
	FriendlyName      string      `json:"friendlyName" xml:"friendlyName,attr"`
	Directory         []plexEntry `json:"Directory" xml:"Directory"`
	Metadata          []plexEntry `json:"Metadata" xml:"-"`
	Video             []plexEntry `json:"-" xml:"Video"`
}

// plexDoc accepts both {"MediaContainer": {...}} and <MediaContainer ...>.
type plexDoc struct {
	plexContainer
	MediaContainer *plexContainer `json:"MediaContainer" xml:"-"`
}

func (d plexDoc) container() plexContainer {
	if d.MediaContainer != nil {
		return *d.MediaContainer
	}
	return d.plexContainer
}

// items is every title in a listing, whichever way it was encoded.
func (c plexContainer) items() []plexEntry {
	out := append([]plexEntry{}, c.Metadata...)
	out = append(out, c.Video...)
	for _, d := range c.Directory {
		if d.RatingKey != "" {
			out = append(out, d)
		}
	}
	return out
}

func (c *Client) plexGet(ctx context.Context, s Server, path string, q url.Values) (plexContainer, error) {
	var doc plexDoc
	if err := c.do(ctx, s, http.MethodGet, path, q, nil, &doc); err != nil {
		return plexContainer{}, err
	}
	return doc.container(), nil
}

func (c *Client) plexIdentity(ctx context.Context, s Server) (plexContainer, error) {
	id, err := c.plexGet(ctx, s, "/identity", nil)
	if err != nil {
		return plexContainer{}, explain(s, err)
	}
	if id.MachineIdentifier == "" {
		return plexContainer{}, explain(s, errUnrecognised)
	}
	return id, nil
}

func (c *Client) plexSections(ctx context.Context, s Server) ([]Library, error) {
	secs, err := c.plexGet(ctx, s, "/library/sections", nil)
	if err != nil {
		return nil, explain(s, err)
	}
	out := []Library{}
	for _, d := range secs.Directory {
		lib := Library{ID: d.Key, Title: d.Title, Type: d.Type, Locations: []string{}}
		for _, l := range d.Locations {
			lib.Locations = append(lib.Locations, l.Path)
		}
		out = append(out, lib)
	}
	return out, nil
}

func (c *Client) plexTest(ctx context.Context, s Server) (TestResult, error) {
	id, err := c.plexIdentity(ctx, s)
	if err != nil {
		return TestResult{}, err
	}
	libs, err := c.plexSections(ctx, s)
	if err != nil {
		return TestResult{}, err
	}
	name := id.FriendlyName
	if root, err := c.plexGet(ctx, s, "/", nil); err == nil && root.FriendlyName != "" {
		name = root.FriendlyName
	}
	return TestResult{ServerName: name, Version: id.Version, ServerID: id.MachineIdentifier, Libraries: libs}, nil
}

func (c *Client) plexRefreshAll(ctx context.Context, s Server) error {
	libs, err := c.plexSections(ctx, s)
	if err != nil {
		return err
	}
	var errs []error
	for _, l := range libs {
		if err := c.do(ctx, s, http.MethodGet, "/library/sections/"+url.PathEscape(l.ID)+"/refresh", nil, nil, nil); err != nil {
			errs = append(errs, fmt.Errorf("refresh %q: %w", l.Title, explain(s, err)))
		}
	}
	return errors.Join(errs...)
}

// plexSectionType is the Plex section type that holds a media kind.
func plexSectionType(kind MediaKind) string {
	switch kind {
	case MediaTV:
		return "show"
	case MediaMusic:
		return "artist"
	}
	return "movie"
}

// plexRefreshFolders scans each folder in the library that contains it. A
// folder no library contains (usually a missing path mapping) falls back to
// a full scan of every library of the right type, so the new file still
// shows up, just more slowly.
func (c *Client) plexRefreshFolders(ctx context.Context, s Server, kind MediaKind, folders []string) ([]string, error) {
	libs, err := c.plexSections(ctx, s)
	if err != nil {
		return nil, err
	}
	type scan struct{ lib, path string }
	var scans []scan
	seen := map[scan]bool{}
	fullScan := false
	for _, folder := range folders {
		mapped := MapPath(folder, s.PathMap)
		matched := false
		for _, l := range libs {
			for _, loc := range l.Locations {
				if _, ok := within(mapped, loc); ok {
					sc := scan{l.ID, mapped}
					if !seen[sc] {
						seen[sc] = true
						scans = append(scans, sc)
					}
					matched = true
				}
			}
		}
		if !matched {
			fullScan = true
		}
	}
	if fullScan {
		for _, l := range libs {
			if l.Type == plexSectionType(kind) {
				sc := scan{l.ID, ""}
				if !seen[sc] {
					seen[sc] = true
					scans = append(scans, sc)
				}
			}
		}
	}
	if len(scans) == 0 {
		return nil, userErr(nil, "Plex has no %s library to refresh.", map[MediaKind]string{MediaMovie: "movie", MediaTV: "TV", MediaMusic: "music"}[kind])
	}
	titles := map[string]string{}
	for _, l := range libs {
		titles[l.ID] = l.Title
	}
	var done []string
	var errs []error
	for _, sc := range scans {
		var q url.Values
		what := titles[sc.lib] + ": whole library (no library folder matched; check the path mapping)"
		if sc.path != "" {
			q = url.Values{"path": {sc.path}}
			what = titles[sc.lib] + ": " + sc.path
		}
		if err := c.do(ctx, s, http.MethodGet, "/library/sections/"+url.PathEscape(sc.lib)+"/refresh", q, nil, nil); err != nil {
			errs = append(errs, fmt.Errorf("refresh %s: %w", what, explain(s, err)))
			continue
		}
		done = append(done, what)
	}
	return done, errors.Join(errs...)
}

// plexTMDBID reads a TMDB id out of a Plex guid: "tmdb://603" (the current
// Plex agents' external ids) or "com.plexapp.agents.themoviedb://603?lang=en"
// (the legacy agent's own guid).
func plexTMDBID(guid string) (int, bool) {
	var rest string
	switch {
	case strings.HasPrefix(guid, "tmdb://"):
		rest = strings.TrimPrefix(guid, "tmdb://")
	case strings.HasPrefix(guid, "com.plexapp.agents.themoviedb://"):
		rest = strings.TrimPrefix(guid, "com.plexapp.agents.themoviedb://")
	default:
		return 0, false
	}
	if i := strings.IndexAny(rest, "?/"); i >= 0 {
		rest = rest[:i]
	}
	id, err := strconv.Atoi(rest)
	return id, err == nil && id > 0
}

// plexIndex maps TMDB id -> ratingKey for every title in the libraries of
// the given kind. One request per library, cached by Finder.
func (c *Client) plexIndex(ctx context.Context, s Server, kind MediaKind) (map[int]string, error) {
	libs, err := c.plexSections(ctx, s)
	if err != nil {
		return nil, err
	}
	index := map[int]string{}
	for _, l := range libs {
		if l.Type != plexSectionType(kind) {
			continue
		}
		all, err := c.plexGet(ctx, s, "/library/sections/"+url.PathEscape(l.ID)+"/all", url.Values{"includeGuids": {"1"}})
		if err != nil {
			return nil, explain(s, err)
		}
		for _, it := range all.items() {
			ids := []string{it.GUID}
			for _, g := range it.Guids {
				ids = append(ids, g.ID)
			}
			for _, g := range ids {
				if id, ok := plexTMDBID(g); ok {
					if _, dup := index[id]; !dup {
						index[id] = it.RatingKey
					}
				}
			}
		}
	}
	return index, nil
}

// plexFind looks a title up in index (built if nil).
func (c *Client) plexFind(ctx context.Context, s Server, kind MediaKind, tmdbID int, index map[int]string) (Item, bool, error) {
	if index == nil {
		var err error
		if index, err = c.plexIndex(ctx, s, kind); err != nil {
			return Item{}, false, err
		}
	}
	rk, ok := index[tmdbID]
	if !ok || rk == "" {
		return Item{}, false, nil
	}
	if s.ServerID == "" {
		id, err := c.plexIdentity(ctx, s)
		if err != nil {
			return Item{}, false, err
		}
		s.ServerID = id.MachineIdentifier
	}
	return plexItem(s, rk), true, nil
}

// plexItem builds the links to one title. With a public address the link
// opens the server's own web app; otherwise app.plex.tv, which works from
// anywhere the person is signed in to Plex.
func plexItem(s Server, ratingKey string) Item {
	route := "#!/server/" + url.PathEscape(s.ServerID) + "/details?key=" + url.QueryEscape("/library/metadata/"+ratingKey)
	app := "https://app.plex.tv/desktop/" + route
	it := Item{ID: ratingKey, URL: app, AppURL: app}
	if strings.TrimSpace(s.PublicURL) != "" {
		it.URL = s.WebURL() + "/web/index.html" + route
	}
	return it
}
