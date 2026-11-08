package api

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/thetvdb"
)

// TheTVDB as a second source for shows (V4 D25). TMDB stays the source of a
// show's episode list. With a TheTVDB key saved and "Fill in shows from
// TheTVDB" on, a refresh also reads the show's episodes on TheTVDB and adds
// what TMDB is missing, but only where the two clearly count the same way:
//
//   - a season TMDB doesn't have at all is added when TMDB's page is nearly
//     empty (Formula 1: one episode on TMDB, every season on TheTVDB), or when
//     every season both have agrees;
//   - an episode missing inside a season is added when that season agrees.
//
// A season agrees when the episodes both list mostly aired on the same days.
// Specials (season 0) and anime, whose seasons the two often split
// differently, are never filled.

// thinTMDBShow is how few regular episodes a TMDB page may list and still
// count as nearly empty.
const thinTMDBShow = 3

// tvdbClient is the TheTVDB client for the saved key, or nil when none is
// saved or filling is off.
func (s *Server) tvdbClient() *thetvdb.Client {
	if s.Settings == nil {
		return nil
	}
	if on, _ := s.Settings.GetBool(settings.KeyTVDBFill); !on {
		return nil
	}
	key, _ := s.Settings.Get(settings.KeyTVDBAPIKey)
	if strings.TrimSpace(key) == "" {
		return nil
	}
	pin, _ := s.Settings.Get(settings.KeyTVDBPIN)
	s.tvdbMu.Lock()
	defer s.tvdbMu.Unlock()
	if s.tvdb == nil || s.tvdbKey != key+"\x00"+pin {
		s.tvdb = thetvdb.NewAt(s.tvdbBase(), key, pin)
		s.tvdbKey = key + "\x00" + pin
	}
	return s.tvdb
}

func (s *Server) tvdbBase() string {
	if s.tvdbTestBase != "" {
		return s.tvdbTestBase
	}
	return thetvdb.DefaultBaseURL
}

// withTVDBEpisodes adds to eps (TMDB's list) what TheTVDB has and TMDB lacks,
// when filling is on. A failure leaves TMDB's list as it is.
func (s *Server) withTVDBEpisodes(ctx context.Context, sr library.Series, tvdbID int, eps []library.Episode) []library.Episode {
	c := s.tvdbClient()
	if c == nil || tvdbID <= 0 || sr.SeriesType == library.SeriesAnime {
		return eps
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	tv, err := c.Episodes(ctx, tvdbID)
	if err != nil {
		slog.Warn("thetvdb: couldn't read a show's episodes", "show", sr.Title, "err", err)
		return eps
	}
	out, added := fillFromTVDB(eps, tv)
	if added > 0 {
		slog.Info("thetvdb: filled in episodes TMDB is missing", "show", sr.Title, "added", added)
	}
	return out
}

// fillFromTVDB is the rule above. It returns TMDB's list with TheTVDB's extra
// episodes after it, and how many were added.
func fillFromTVDB(tmdb []library.Episode, tvdb []thetvdb.Episode) ([]library.Episode, int) {
	have := map[int]map[int]library.Episode{}
	regular := 0
	for _, e := range tmdb {
		if have[e.Season] == nil {
			have[e.Season] = map[int]library.Episode{}
		}
		have[e.Season][e.Episode] = e
		if e.Season > 0 {
			regular++
		}
	}
	bySeason := map[int][]thetvdb.Episode{}
	for _, e := range tvdb {
		if e.Season > 0 {
			bySeason[e.Season] = append(bySeason[e.Season], e)
		}
	}
	agrees := map[int]bool{}
	overlap, disagree := 0, 0
	for season, list := range bySeason {
		if len(have[season]) == 0 {
			continue
		}
		overlap++
		if seasonAgrees(have[season], list) {
			agrees[season] = true
		} else {
			disagree++
		}
	}
	fillWhole := regular <= thinTMDBShow || (overlap > 0 && disagree == 0)
	out := append([]library.Episode(nil), tmdb...)
	added := 0
	for _, season := range slices.SortedFunc(maps.Keys(bySeason), cmp.Compare[int]) {
		list := bySeason[season]
		whole := len(have[season]) == 0
		if (whole && !fillWhole) || (!whole && !agrees[season]) {
			continue
		}
		for _, e := range list {
			if _, ok := have[season][e.Number]; ok {
				continue
			}
			out = append(out, library.Episode{Season: season, Episode: e.Number, Title: e.Name, Overview: e.Overview, AirDate: e.Aired})
			if have[season] == nil {
				have[season] = map[int]library.Episode{}
			}
			have[season][e.Number] = library.Episode{}
			added++
		}
	}
	return out, added
}

// seasonAgrees reports whether TMDB's and TheTVDB's episodes of a season with
// the same numbers aired on the same days (two days apart at most), for at
// least four in five of those both give a date for, and at least one.
func seasonAgrees(tmdb map[int]library.Episode, tvdb []thetvdb.Episode) bool {
	compared, same := 0, 0
	for _, e := range tvdb {
		t, ok := tmdb[e.Number]
		if !ok || t.AirDate == "" || e.Aired == "" {
			continue
		}
		a, err1 := time.Parse("2006-01-02", t.AirDate)
		b, err2 := time.Parse("2006-01-02", e.Aired)
		if err1 != nil || err2 != nil {
			continue
		}
		compared++
		if d := a.Sub(b); d <= 48*time.Hour && d >= -48*time.Hour {
			same++
		}
	}
	return compared > 0 && same*5 >= compared*4
}

// ---- The TheTVDB key (Settings > Info, lists and subtitles)

type tvdbState struct {
	Set  bool `json:"set"`  // a key is saved
	Fill bool `json:"fill"` // shows are filled in from TheTVDB
}

func (s *Server) tvdbState() tvdbState {
	key, _ := s.Settings.Get(settings.KeyTVDBAPIKey)
	fill, _ := s.Settings.GetBool(settings.KeyTVDBFill)
	return tvdbState{Set: strings.TrimSpace(key) != "", Fill: fill}
}

// handleGetTVDB says whether a TheTVDB key is saved and whether shows are
// filled in from it.
func (s *Server) handleGetTVDB(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tvdbState())
}

// handlePutTVDB saves a TheTVDB key (checked with TheTVDB first, kept
// encrypted) and the switch for filling in shows from it. Body: {"key", "pin",
// "fill", "remove"}; an empty key keeps the saved one, remove takes it away and
// switches filling off.
func (s *Server) handlePutTVDB(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key    string `json:"key"`
		PIN    string `json:"pin"`
		Fill   *bool  `json:"fill"`
		Remove bool   `json:"remove"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	key, pin := strings.TrimSpace(req.Key), strings.TrimSpace(req.PIN)
	if len(key) > 200 || len(pin) > 200 || strings.ContainsAny(key+pin, " \r\n\t") {
		writeError(w, http.StatusBadRequest, "That doesn't look like a TheTVDB API key. Copy it again from thetvdb.com, Dashboard, API keys.")
		return
	}
	switch {
	case req.Remove:
		for _, k := range []string{settings.KeyTVDBAPIKey, settings.KeyTVDBPIN} {
			if err := s.Settings.Set(k, "", true); err != nil {
				writeError(w, http.StatusInternalServerError, "The key couldn't be removed.")
				return
			}
		}
		_ = s.Settings.Set(settings.KeyTVDBFill, "0", false)
	case key != "":
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := thetvdb.NewAt(s.tvdbBase(), key, pin).Check(ctx); err != nil {
			if errors.Is(err, thetvdb.ErrBadKey) {
				writeError(w, http.StatusBadRequest, "TheTVDB didn't accept that key. Check it on thetvdb.com, and add your subscriber PIN if the key is a user key.")
				return
			}
			writeUpstreamError(w, "check the key with TheTVDB", err)
			return
		}
		if s.Settings.Set(settings.KeyTVDBAPIKey, key, true) != nil || s.Settings.Set(settings.KeyTVDBPIN, pin, true) != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be saved.")
			return
		}
	}
	if req.Fill != nil && !req.Remove {
		if *req.Fill && !s.tvdbState().Set {
			writeError(w, http.StatusBadRequest, "Save a TheTVDB key first.")
			return
		}
		v := "0"
		if *req.Fill {
			v = "1"
		}
		if err := s.Settings.Set(settings.KeyTVDBFill, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.tvdbState())
}
