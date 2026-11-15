// Package origin says where a title or a download came from: who added it, or
// which list or scheduled job did. It is a small record kept next to the thing
// it describes, shown as "Added by ..." in the app. A row from before this was
// kept has no origin (the zero value), and nothing is shown for it.
package origin

import "strings"

// The kinds of origin.
const (
	// Manual is something a person did: added a title, picked a release.
	Manual = "manual"
	// Search is a release Mediarium picked because a person started a search
	// (Search now, a bulk search).
	Search = "search"
	// Auto is something a scheduled job did on its own; Name says which one
	// (see the Job constants).
	Auto = "auto"
	// List is something an import list did; Name is the list's name.
	List = "list"
	// Import is a title found by scanning an existing library folder.
	Import = "import"
	// API is a title added through the Radarr or Sonarr compatible API, for
	// example by Overseerr or a phone app.
	API = "api"
	// Request is a title added when a request was approved.
	Request = "request"
	// Watchlist is a title added from a person's Plex watchlist.
	Watchlist = "watchlist"
)

// The scheduled jobs an Auto origin names.
const (
	JobSearch   = "search"   // the scheduled search for missing titles and better versions
	JobRSS      = "rss"      // the RSS check of the indexers
	JobAnnounce = "announce" // an IRC announce channel
)

// Origin is where something came from.
type Origin struct {
	Kind string `json:"kind"`
	// Name is the list's name for a List, the job for an Auto origin and empty
	// otherwise.
	Name string `json:"name,omitempty"`
	// UserID and User are the account behind a Manual or Search origin: its id
	// and its name as it was when the thing was added (0 and "" when unknown).
	UserID int64  `json:"userId,omitempty"`
	User   string `json:"user,omitempty"`
}

// Known says the origin was recorded.
func (o Origin) Known() bool { return o.Kind != "" }

// Clean returns the origin with an unknown kind dropped and the text trimmed
// to a sensible length, so nothing odd is stored.
func (o Origin) Clean() Origin {
	switch o.Kind {
	case Manual, Search, Auto, List, Import, API, Request, Watchlist:
	default:
		return Origin{}
	}
	o.Name = clip(o.Name)
	o.User = clip(o.User)
	if o.UserID < 0 {
		o.UserID = 0
	}
	return o
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 120 {
		return string(r[:120])
	}
	return s
}
