// Package mediaservers talks to the media servers people watch their library
// with (Plex, Jellyfin and Emby): it tests a connection, asks the server to
// rescan the library folder a new file landed in, and finds a title on the
// server so the web UI can link to it ("Watch in Plex").
//
// It stands on its own: it knows nothing about downloads, the queue or the
// API, and is handed plain file paths and TMDB ids by its callers.
package mediaservers

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Kind is the type of media server.
type Kind string

const (
	KindPlex     Kind = "plex"
	KindJellyfin Kind = "jellyfin"
	KindEmby     Kind = "emby"
)

// Label is the product name as people know it.
func (k Kind) Label() string {
	switch k {
	case KindPlex:
		return "Plex"
	case KindJellyfin:
		return "Jellyfin"
	case KindEmby:
		return "Emby"
	}
	return string(k)
}

// ParseKind accepts a kind in any case.
func ParseKind(s string) (Kind, bool) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(s))); k {
	case KindPlex, KindJellyfin, KindEmby:
		return k, true
	}
	return "", false
}

// MediaKind is what was imported or is being looked up.
type MediaKind string

const (
	MediaMovie MediaKind = "movie"
	MediaTV    MediaKind = "tv"
	// MediaMusic is only ever refreshed (an album folder was imported); it
	// has no TMDB id, so it is never looked up by ParseMediaKind or Find.
	MediaMusic MediaKind = "music"
)

// ParseMediaKind accepts "movie" or "tv" (also "series"/"show").
func ParseMediaKind(s string) (MediaKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "movie", "movies":
		return MediaMovie, true
	case "tv", "series", "show", "shows":
		return MediaTV, true
	}
	return "", false
}

// PathMapping rewrites a folder as Mediarium sees it (From) into the same
// folder as the media server sees it (To), for when the two run in
// different containers or machines with the library mounted in different
// places.
type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Server is one configured media server.
type Server struct {
	ID                 int64
	Name               string
	Kind               Kind
	BaseURL            string // how Mediarium reaches the server
	PublicURL          string // what people open in their browser; empty means BaseURL
	Token              string // Plex token or Jellyfin/Emby API key (plain text in memory only)
	Enabled            bool
	RefreshAfterImport bool
	PathMap            []PathMapping
	ServerID           string // Plex machineIdentifier or Jellyfin/Emby server Id
	LastError          string
	LastCheckedAt      time.Time // zero when never checked
}

// WebURL is the address people open, without a trailing slash.
func (s Server) WebURL() string {
	if u := strings.TrimRight(strings.TrimSpace(s.PublicURL), "/"); u != "" {
		return u
	}
	return strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
}

// UserError is a problem worth showing to the person as it is: a wrong
// token, an address that doesn't answer, a server of another kind.
type UserError struct {
	Message string
	Err     error // the underlying cause, for logs; may be nil
}

func (e *UserError) Error() string { return e.Message }
func (e *UserError) Unwrap() error { return e.Err }

func userErr(err error, format string, args ...any) error {
	return &UserError{Message: fmt.Sprintf(format, args...), Err: err}
}

// NormalizeURL checks an http(s) address and returns it without a trailing
// slash. An address typed without a scheme gets http://.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("an address is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a valid address; use something like http://192.168.1.10:32400", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("the address must start with http:// or https://")
	}
	u.RawQuery, u.Fragment, u.ForceQuery = "", "", false
	return strings.TrimRight(u.String(), "/"), nil
}

// NormalizePathMap drops empty rows, trims whitespace and trailing
// separators, and rejects a row that has only one side.
func NormalizePathMap(in []PathMapping) ([]PathMapping, error) {
	out := []PathMapping{}
	for _, m := range in {
		from, to := trimSep(strings.TrimSpace(m.From)), trimSep(strings.TrimSpace(m.To))
		if from == "" && to == "" {
			continue
		}
		if from == "" || to == "" {
			return nil, fmt.Errorf("each path mapping needs both a Mediarium folder and a media server folder")
		}
		out = append(out, PathMapping{From: from, To: to})
	}
	return out, nil
}

// trimSep removes trailing separators but keeps a bare root ("/", "C:\").
func trimSep(p string) string {
	for len(p) > 1 && (strings.HasSuffix(p, "/") || strings.HasSuffix(p, `\`)) {
		if len(p) == 3 && p[1] == ':' {
			break // "C:\"
		}
		p = p[:len(p)-1]
	}
	return p
}

// windowsStyle reports whether p looks like a Windows path (C:\... or \\host\...).
func windowsStyle(p string) bool {
	return (len(p) >= 2 && p[1] == ':') || strings.HasPrefix(p, `\\`)
}

// comparable turns a path into a form two paths can be compared in: forward
// slashes, no trailing slash, lower case for Windows-style paths.
func comparable(p string) string {
	win := windowsStyle(p)
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimRight(p, "/")
	if win {
		p = strings.ToLower(p)
	}
	return p
}

// within reports whether path is dir itself or inside it, returning the rest
// of path after dir ("" or "/sub/folder", forward slashes).
func within(path, dir string) (rest string, ok bool) {
	if comparable(dir) == "" { // a root folder
		return strings.ReplaceAll(path, `\`, "/"), true
	}
	// Compare character by character on the slash-normalised text. Lower
	// casing whole strings and then cutting the original at the same byte
	// count goes wrong (or past the end) for characters whose lower case is
	// a different length.
	np := strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	nd := strings.TrimRight(strings.ReplaceAll(dir, `\`, "/"), "/")
	foldPath, foldDir := windowsStyle(path), windowsStyle(dir)
	i := 0
	for j := 0; j < len(nd); {
		if i >= len(np) {
			return "", false
		}
		rp, sp := utf8.DecodeRuneInString(np[i:])
		rd, sd := utf8.DecodeRuneInString(nd[j:])
		if foldPath {
			rp = unicode.ToLower(rp)
		}
		if foldDir {
			rd = unicode.ToLower(rd)
		}
		if rp != rd {
			return "", false
		}
		i += sp
		j += sd
	}
	rest = np[i:]
	if rest != "" && rest[0] != '/' {
		return "", false
	}
	return rest, true
}

// MapPath rewrites path from Mediarium's view to the media server's using
// the longest matching mapping. Without a match it returns path unchanged.
// The rest of the path takes the separator style of the mapping's target, so
// a Windows Plex server gets backslashes.
func MapPath(path string, mappings []PathMapping) string {
	best, bestLen, bestRest := -1, -1, ""
	for i, m := range mappings {
		rest, ok := within(path, m.From)
		if ok && len(comparable(m.From)) > bestLen {
			best, bestLen, bestRest = i, len(comparable(m.From)), rest
		}
	}
	if best < 0 {
		return path
	}
	to := mappings[best].To
	if bestRest == "" {
		return to
	}
	if windowsStyle(to) || (strings.Contains(to, `\`) && !strings.Contains(to, "/")) {
		bestRest = strings.ReplaceAll(bestRest, "/", `\`)
		return strings.TrimRight(to, `\`) + bestRest
	}
	return strings.TrimRight(to, "/") + bestRest
}
