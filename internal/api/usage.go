package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/trakt"
	"github.com/rdborg/mediarium/internal/usage"
)

// Service ids used by the usage record and GET /api/usage.
const (
	serviceTMDB          = "tmdb"
	serviceTrakt         = "trakt"
	serviceOpenSubtitles = "opensubtitles"
)

// instrumentTMDB, instrumentTrakt and instrumentSubtitles make a live client
// count its requests, and its limit hits, in the usage record. They are not
// applied to the throwaway clients used to test a key before saving it.
func (s *Server) instrumentTMDB(c *metadata.Client) {
	c.WrapTransport(s.Usage.Wrap(serviceTMDB, usage.OnTooManyRequests))
}

func (s *Server) instrumentTrakt(c *trakt.Client) {
	c.WrapTransport(s.Usage.Wrap(serviceTrakt, usage.OnTooManyRequests))
}

func (s *Server) instrumentSubtitles(c *subtitles.Client) {
	c.WrapTransport(s.Usage.Wrap(serviceOpenSubtitles, openSubtitlesLimitHit))
}

// openSubtitlesLimitHit: a 429 anywhere is a rate limit; a 406 from the
// download endpoint is OpenSubtitles' "daily download limit reached".
func openSubtitlesLimitHit(req *http.Request, resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusNotAcceptable && strings.HasSuffix(req.URL.Path, "/download"))
}

// serviceKeys says, for one service, whether the person saved their own key
// and whether the app's shared key is what is in use.
type serviceKeys struct {
	id, label  string
	ownKey     bool
	sharedKey  bool
	settingKey string
}

func (s *Server) serviceKeyStates() []serviceKeys {
	states := []serviceKeys{
		{id: serviceTrakt, label: "Trakt", settingKey: settings.KeyTraktClientID, sharedKey: s.builtin.TraktClientID != ""},
		{id: serviceOpenSubtitles, label: "OpenSubtitles", settingKey: settings.KeyOpenSubtitlesAPIKey, sharedKey: s.builtin.OpenSubtitles != ""},
		{id: serviceTMDB, label: "TMDB", settingKey: settings.KeyTMDBAPIKey, sharedKey: s.builtin.TMDB != ""},
	}
	for i := range states {
		states[i].ownKey = s.usingOwnKey(states[i].settingKey)
		states[i].sharedKey = states[i].sharedKey && !states[i].ownKey
	}
	return states
}

// usingSharedKey reports whether the service runs on the key shipped with the app.
func (s *Server) usingSharedKey(id string) bool {
	for _, st := range s.serviceKeyStates() {
		if st.id == id {
			return st.sharedKey
		}
	}
	return false
}

type serviceUsagePayload struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	UsingOwnKey    bool    `json:"usingOwnKey"`
	UsingSharedKey bool    `json:"usingSharedKey"`
	Requests24h    int     `json:"requests24h"`
	LimitHits24h   int     `json:"limitHits24h"`
	LastLimitHitAt *string `json:"lastLimitHitAt"`
	Note           string  `json:"note"`
}

// usageNote is the plain-language sentence about how a service is doing.
func usageNote(label string, shared bool, requests, hits int) string {
	switch {
	case hits > 0 && shared:
		return fmt.Sprintf("%s has refused %s in the last 24 hours because the shared key is busy. A free personal key removes the limit.", label, plural(hits, "request"))
	case hits > 0:
		return fmt.Sprintf("%s has refused %s in the last 24 hours. You're using your own key, so this is your own account's limit.", label, plural(hits, "request"))
	case requests > 0:
		return fmt.Sprintf("%s is working normally: %s in the last 24 hours and no limit reached.", label, plural(requests, "request"))
	}
	return fmt.Sprintf("%s hasn't been used in the last 24 hours.", label)
}

// handleUsage reports, per third-party service, how much Mediarium used it in
// the last 24 hours and whether it refused because a limit was reached.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	out := []serviceUsagePayload{}
	for _, st := range s.serviceKeyStates() {
		stats := s.Usage.Stats(st.id)
		p := serviceUsagePayload{
			ID: st.id, Label: st.label, UsingOwnKey: st.ownKey, UsingSharedKey: st.sharedKey,
			Requests24h: stats.Requests, LimitHits24h: stats.LimitHits,
			Note: usageNote(st.label, st.sharedKey, stats.Requests, stats.LimitHits),
		}
		if !stats.LastLimitHit.IsZero() {
			t := stats.LastLimitHit.UTC().Format(time.RFC3339)
			p.LastLimitHitAt = &t
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": out})
}
