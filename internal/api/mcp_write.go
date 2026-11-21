package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// The tools that change something. Each one first works out what it would do
// (reading only), and does it only when the call carries confirm: true. Without
// it the answer is a preview that tells the assistant to ask the person. The
// changes themselves go through the web app's own routes, so every check that
// applies on a page applies here too.

// mcpPlan is what a write tool would do.
type mcpPlan struct {
	Summary string
	Do      func() (any, error)
}

func writeTool[T any](name, desc string, schema map[string]any, plan func(c *mcpCtx, a T) (*mcpPlan, error)) *mcpTool {
	schema["properties"].(map[string]any)["confirm"] = pBool("Must be true to make the change. Leave it out first to get a preview, show the preview to the person and only set it once they agree.")
	return &mcpTool{name: name, desc: desc + " Changes something: call without confirm first to preview.", write: true, schema: schema, run: func(c *mcpCtx, raw json.RawMessage) (any, error) {
		var a T
		if err := decodeArgs(raw, &a); err != nil {
			return nil, err
		}
		var cf struct {
			Confirm bool `json:"confirm"`
		}
		if err := decodeArgs(raw, &cf); err != nil {
			return nil, err
		}
		p, err := plan(c, a)
		if err != nil {
			return nil, err
		}
		if !cf.Confirm {
			return map[string]any{
				"changed":   false,
				"preview":   true,
				"would_do":  p.Summary,
				"next_step": "Nothing has been changed. Tell the person what would happen and ask if they want it. Only if they say yes, call this tool again with the same arguments and confirm set to true.",
			}, nil
		}
		result, err := p.Do()
		if err != nil {
			return nil, err
		}
		return map[string]any{"changed": true, "done": p.Summary, "result": result}, nil
	}}
}

// startsDownloads marks a change tool that can end in a download without the
// person choosing a release: it is only there while an administrator has
// allowed assistants to start downloads, like grab_release.
func startsDownloads(t *mcpTool) *mcpTool {
	t.download = true
	return t
}

func mcpWriteTools() []*mcpTool {
	return []*mcpTool{
		startsDownloads(writeTool("add_movie", "Add a movie to the library by its TMDB id (find it with lookup_title). profile is a profile name or id (profiles_list); left out, the default is used. It is searched for right away unless search_now is false.",
			schemaOf(props{"tmdb_id": pInt("The movie's TMDB id."), "profile": pStr("Quality profile name or id."), "search_now": pBool("Search for it straight away. Default true."), "monitored": pBool("Keep looking for it. Default true.")}, "tmdb_id"), planAddMovie)),
		startsDownloads(writeTool("add_series", "Add a show to the library by its TMDB id or TVDB id (find it with lookup_title). monitor is all (default), future or none. profile is a profile name or id.",
			schemaOf(props{"tmdb_id": pInt("The show's TMDB id."), "tvdb_id": pInt("The show's TVDB id, when the TMDB id isn't known."), "profile": pStr("Quality profile name or id."),
				"monitor": pEnum("Which episodes to look for: all, future or none.", "all", "future", "none"), "search_now": pBool("Search for it straight away. Default true.")}), planAddSeries)),
		startsDownloads(writeTool("search_now", "Start a search for a release right now for movies, shows or single episodes that are in the library (at most 20 in one call). Use the ids from library_search or series_details.",
			schemaOf(props{"movie_ids": pInts("Movie ids."), "series_ids": pInts("Show ids (whole show)."), "episode_ids": pInts("Episode ids.")}), planSearchNow)),
		writeTool("set_movie_profile", "Put a movie on another quality profile, for example to upgrade it to 4K. If its file is below what the new profile wants, a search for a better version starts. profile is a name or id (profiles_list).",
			schemaOf(props{"movie_id": pInt("The movie's id."), "profile": pStr("Quality profile name or id.")}, "movie_id", "profile"), planSetMovieProfile),
		writeTool("set_series_profile", "Put a show on another quality profile. profile is a name or id (profiles_list).",
			schemaOf(props{"series_id": pInt("The show's id."), "profile": pStr("Quality profile name or id.")}, "series_id", "profile"), planSetSeriesProfile),
		writeTool("retry_failed_download", "Block the release of a failed download and search for another release of the same title. queue_id is from downloads_failed.",
			schemaOf(props{"queue_id": pInt("The failed download's queue id.")}, "queue_id"), planRetryDownload),
		writeTool("add_preferred_term", "Add a preferred term to a quality profile: releases whose name contains the term get the score added, so they win between releases of the same quality. A negative score avoids them. If the term is already there, its score changes.",
			schemaOf(props{"profile": pStr("Quality profile name or id."), "term": pStr("The word or tag to prefer, such as FuN or Atmos."), "score": pInt("Points to add. Default 10; negative avoids.")}, "profile", "term"), planAddPreferred),
	}
}

// ---- Profiles ----

func (c *mcpCtx) profiles() (profileListPayload, error) {
	var list profileListPayload
	err := c.call(http.MethodGet, "/api/quality-profiles", nil, &list)
	return list, err
}

// findProfile turns "4K", "Any" or "3" into one profile. An empty ref gives
// ok=false with no error: the caller uses the default.
func (c *mcpCtx) findProfile(ref string) (p profilePayload, ok bool, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return p, false, nil
	}
	list, err := c.profiles()
	if err != nil {
		return p, false, err
	}
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		for _, q := range list.Profiles {
			if q.ID == id {
				return q, true, nil
			}
		}
	}
	var partial []profilePayload
	for _, q := range list.Profiles {
		if strings.EqualFold(q.Name, ref) {
			return q, true, nil
		}
		if containsFold(q.Name, ref) {
			partial = append(partial, q)
		}
	}
	if len(partial) == 1 {
		return partial[0], true, nil
	}
	names := make([]string, 0, len(list.Profiles))
	for _, q := range list.Profiles {
		names = append(names, q.Name)
	}
	if len(partial) > 1 {
		return p, false, toolErr("More than one profile matches %q. The profiles are: %s.", ref, strings.Join(names, ", "))
	}
	return p, false, toolErr("There is no quality profile %q. The profiles are: %s.", ref, strings.Join(names, ", "))
}

func profileWords(p profilePayload, ok bool) string {
	if !ok {
		return "the default profile"
	}
	return fmt.Sprintf("the %q profile", p.Name)
}

func titleYear(title string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", title, year)
	}
	return title
}

// ---- add_movie / add_series ----

func planAddMovie(c *mcpCtx, a struct {
	TMDBID    int    `json:"tmdb_id"`
	Profile   string `json:"profile"`
	SearchNow *bool  `json:"search_now"`
	Monitored *bool  `json:"monitored"`
}) (*mcpPlan, error) {
	if a.TMDBID <= 0 {
		return nil, toolErr("tmdb_id is needed. Find it with lookup_title.")
	}
	if m, in, _ := c.s.MovieRepo.GetByTMDBID(a.TMDBID); in {
		return nil, toolErr("%s is already in the library (id %d).", m.Title, m.ID)
	}
	var d struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
	}
	if err := c.call(http.MethodGet, "/api/tmdb/movies/"+strconv.Itoa(a.TMDBID), nil, &d); err != nil {
		return nil, err
	}
	prof, hasProf, err := c.findProfile(a.Profile)
	if err != nil {
		return nil, err
	}
	search := a.SearchNow == nil || *a.SearchNow
	req := map[string]any{"tmdbId": a.TMDBID, "searchNow": search}
	if hasProf {
		req["profileId"] = prof.ID
	}
	if a.Monitored != nil {
		req["monitored"] = *a.Monitored
	}
	summary := fmt.Sprintf("Add the movie %s to the library on %s", titleYear(d.Title, d.Year), profileWords(prof, hasProf))
	if search {
		summary += " and search for it right away."
	} else {
		summary += " without searching yet."
	}
	return &mcpPlan{Summary: summary, Do: func() (any, error) {
		var out struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		}
		if err := c.call(http.MethodPost, "/api/movies", req, &out); err != nil {
			return nil, err
		}
		return map[string]any{"id": out.ID, "title": out.Title}, nil
	}}, nil
}

func planAddSeries(c *mcpCtx, a struct {
	TMDBID    int    `json:"tmdb_id"`
	TVDBID    int    `json:"tvdb_id"`
	Profile   string `json:"profile"`
	Monitor   string `json:"monitor"`
	SearchNow *bool  `json:"search_now"`
}) (*mcpPlan, error) {
	switch a.Monitor {
	case "", "all", "future", "none":
	default:
		return nil, toolErr("monitor must be all, future or none.")
	}
	tmdb := a.TMDBID
	if tmdb <= 0 && a.TVDBID > 0 {
		sr, show, found := c.s.seriesByTVDB(c.ctx, a.TVDBID)
		switch {
		case !found:
			return nil, toolErr("No show with TVDB id %d was found.", a.TVDBID)
		case show == nil:
			return nil, toolErr("%s is already in the library (id %d).", sr.Title, sr.ID)
		default:
			tmdb = show.TMDBID
		}
	}
	if tmdb <= 0 {
		return nil, toolErr("tmdb_id or tvdb_id is needed. Find them with lookup_title.")
	}
	if sr, in, _ := c.s.MovieRepo.GetSeriesByTMDBID(tmdb); in {
		return nil, toolErr("%s is already in the library (id %d).", sr.Title, sr.ID)
	}
	var d map[string]any
	if err := c.call(http.MethodGet, "/api/tmdb/tv/"+strconv.Itoa(tmdb), nil, &d); err != nil {
		return nil, err
	}
	title, _ := d["title"].(string)
	if title == "" {
		title, _ = d["name"].(string)
	}
	year, _ := d["year"].(float64)
	prof, hasProf, err := c.findProfile(a.Profile)
	if err != nil {
		return nil, err
	}
	search := a.SearchNow == nil || *a.SearchNow
	req := map[string]any{"tmdbId": tmdb, "searchNow": search}
	if hasProf {
		req["profileId"] = prof.ID
	}
	monitor := a.Monitor
	if monitor == "" {
		monitor = "all"
	}
	req["monitor"] = monitor
	summary := fmt.Sprintf("Add the show %s to the library on %s, looking for %s episodes", titleYear(title, int(year)), profileWords(prof, hasProf), map[string]string{"all": "all", "future": "future", "none": "no"}[monitor])
	if search {
		summary += " and search right away."
	} else {
		summary += " without searching yet."
	}
	return &mcpPlan{Summary: summary, Do: func() (any, error) {
		var out struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		}
		if err := c.call(http.MethodPost, "/api/series", req, &out); err != nil {
			return nil, err
		}
		return map[string]any{"id": out.ID, "title": out.Title}, nil
	}}, nil
}

// ---- search_now ----

func planSearchNow(c *mcpCtx, a struct {
	MovieIDs   []int64 `json:"movie_ids"`
	SeriesIDs  []int64 `json:"series_ids"`
	EpisodeIDs []int64 `json:"episode_ids"`
}) (*mcpPlan, error) {
	if len(a.MovieIDs)+len(a.SeriesIDs)+len(a.EpisodeIDs) == 0 {
		return nil, toolErr("Give at least one of movie_ids, series_ids or episode_ids.")
	}
	if len(a.MovieIDs)+len(a.SeriesIDs)+len(a.EpisodeIDs) > 20 {
		return nil, toolErr("At most 20 titles can be searched in one call.")
	}
	type target struct {
		label string
		path  string
		body  any
	}
	var targets []target
	for _, id := range a.MovieIDs {
		m, err := c.s.MovieRepo.Get(id)
		if err != nil {
			return nil, toolErr("There is no movie with id %d.", id)
		}
		targets = append(targets, target{"movie " + titleYear(m.Title, m.Year), fmt.Sprintf("/api/movies/%d/search-now", id), nil})
	}
	for _, id := range a.SeriesIDs {
		sr, err := c.s.MovieRepo.GetSeries(id)
		if err != nil {
			return nil, toolErr("There is no show with id %d.", id)
		}
		targets = append(targets, target{"show " + sr.Title, fmt.Sprintf("/api/series/%d/search-now", id), map[string]any{}})
	}
	for _, id := range a.EpisodeIDs {
		ep, err := c.s.MovieRepo.GetEpisodeByID(id)
		if err != nil {
			return nil, toolErr("There is no episode with id %d.", id)
		}
		sr, err := c.s.MovieRepo.GetSeries(ep.SeriesID)
		if err != nil {
			return nil, toolErr("There is no show for episode %d.", id)
		}
		season := ep.Season
		targets = append(targets, target{fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.Season, ep.Episode), fmt.Sprintf("/api/series/%d/search-now", ep.SeriesID), map[string]any{"season": &season, "episode": ep.Episode}})
	}
	labels := make([]string, len(targets))
	for i, t := range targets {
		labels[i] = t.label
	}
	return &mcpPlan{Summary: "Search the indexers right now for: " + strings.Join(labels, "; ") + ". A download starts if a suitable release is found.", Do: func() (any, error) {
		type res struct {
			Target  string `json:"target"`
			Grabbed int    `json:"downloads_started"`
			Message string `json:"message,omitempty"`
			Error   string `json:"error,omitempty"`
		}
		var out []res
		for _, t := range targets {
			var r searchNowResponse
			if err := c.call(http.MethodPost, t.path, t.body, &r); err != nil {
				out = append(out, res{Target: t.label, Error: err.Error()})
				continue
			}
			out = append(out, res{Target: t.label, Grabbed: r.Grabbed, Message: r.Message})
		}
		return out, nil
	}}, nil
}

// ---- Profiles of titles ----

func planSetMovieProfile(c *mcpCtx, a struct {
	MovieID int64  `json:"movie_id"`
	Profile string `json:"profile"`
}) (*mcpPlan, error) {
	m, err := c.s.MovieRepo.Get(a.MovieID)
	if err != nil {
		return nil, toolErr("There is no movie with id %d.", a.MovieID)
	}
	prof, ok, err := c.findProfile(a.Profile)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, toolErr("profile is needed (a name or id from profiles_list).")
	}
	if m.ProfileID == prof.ID {
		return nil, toolErr("%s is already on the %q profile.", m.Title, prof.Name)
	}
	now := c.profileNames()[m.ProfileID]
	summary := fmt.Sprintf("Move the movie %s from the %q profile to the %q profile (currently %s).", titleYear(m.Title, m.Year), now, prof.Name, haveWords(m.Status, m.Quality))
	return &mcpPlan{Summary: summary + " If the file is below what the new profile wants, a search for a better version starts.", Do: func() (any, error) {
		var out struct {
			Searching bool `json:"searching"`
		}
		if err := c.call(http.MethodPut, fmt.Sprintf("/api/movies/%d/profile", m.ID), map[string]any{"profileId": prof.ID}, &out); err != nil {
			return nil, err
		}
		return map[string]any{"searching_for_a_better_version": out.Searching}, nil
	}}, nil
}

func planSetSeriesProfile(c *mcpCtx, a struct {
	SeriesID int64  `json:"series_id"`
	Profile  string `json:"profile"`
}) (*mcpPlan, error) {
	sr, err := c.s.MovieRepo.GetSeries(a.SeriesID)
	if err != nil {
		return nil, toolErr("There is no show with id %d.", a.SeriesID)
	}
	prof, ok, err := c.findProfile(a.Profile)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, toolErr("profile is needed (a name or id from profiles_list).")
	}
	if sr.ProfileID == prof.ID {
		return nil, toolErr("%s is already on the %q profile.", sr.Title, prof.Name)
	}
	summary := fmt.Sprintf("Move the show %s from the %q profile to the %q profile.", titleYear(sr.Title, sr.Year), c.profileNames()[sr.ProfileID], prof.Name)
	return &mcpPlan{Summary: summary, Do: func() (any, error) {
		var out struct {
			Searching bool `json:"searching"`
		}
		if err := c.call(http.MethodPut, fmt.Sprintf("/api/series/%d/profile", sr.ID), map[string]any{"profileId": prof.ID}, &out); err != nil {
			return nil, err
		}
		return map[string]any{"searching_for_better_versions": out.Searching}, nil
	}}, nil
}

func haveWords(status library.Status, quality string) string {
	if status == library.StatusDownloaded && quality != "" {
		return "a " + quality + " file"
	}
	return "no file yet"
}

// ---- retry_failed_download ----

func planRetryDownload(c *mcpCtx, a struct {
	QueueID int64 `json:"queue_id"`
}) (*mcpPlan, error) {
	var items []queueItemPayload
	if err := c.call(http.MethodGet, "/api/queue", nil, &items); err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ID != a.QueueID {
			continue
		}
		if it.Status != string(queue.StatusFailed) && it.Status != string(queue.StatusStopped) {
			return nil, toolErr("That download is %s, not failed, so there is nothing to retry.", it.Status)
		}
		why := ""
		if it.Error != "" {
			why = " (" + it.Error + ")"
		}
		summary := fmt.Sprintf("Block the release %q%s so it is never picked again, and search for another release of %s.", it.ReleaseTitle, why, it.Title)
		return &mcpPlan{Summary: summary, Do: func() (any, error) {
			if err := c.call(http.MethodPost, fmt.Sprintf("/api/queue/%d/blocklist", a.QueueID), nil, nil); err != nil {
				return nil, err
			}
			return map[string]any{"blocklisted": it.ReleaseTitle, "searching_for_another": true}, nil
		}}, nil
	}
	return nil, toolErr("There is no download with queue id %d.", a.QueueID)
}

// ---- add_preferred_term ----

func planAddPreferred(c *mcpCtx, a struct {
	Profile string `json:"profile"`
	Term    string `json:"term"`
	Score   *int   `json:"score"`
}) (*mcpPlan, error) {
	term := strings.TrimSpace(a.Term)
	if term == "" {
		return nil, toolErr("term is needed.")
	}
	prof, ok, err := c.findProfile(a.Profile)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, toolErr("profile is needed (a name or id from profiles_list).")
	}
	score := 10
	if a.Score != nil {
		score = *a.Score
	}
	if score < -1000 || score > 1000 {
		return nil, toolErr("score must be between -1000 and 1000.")
	}
	updated := make([]preferredPayload, 0, len(prof.Preferred)+1)
	replaced := false
	for _, p := range prof.Preferred {
		if strings.EqualFold(p.Term, term) {
			if p.Score == score {
				return nil, toolErr("The %q profile already prefers %q with score %d.", prof.Name, p.Term, score)
			}
			updated = append(updated, preferredPayload{Term: p.Term, Score: score})
			replaced = true
			continue
		}
		updated = append(updated, p)
	}
	verb := "Add"
	if replaced {
		verb = "Change the score of"
	} else {
		updated = append(updated, preferredPayload{Term: term, Score: score})
	}
	summary := fmt.Sprintf("%s the preferred term %q with score %d on the %q profile.", verb, term, score, prof.Name)
	body := prof
	body.Preferred = updated
	return &mcpPlan{Summary: summary, Do: func() (any, error) {
		var out profilePayload
		if err := c.call(http.MethodPut, fmt.Sprintf("/api/quality-profiles/%d", prof.ID), body, &out); err != nil {
			return nil, err
		}
		return map[string]any{"profile": out.Name, "preferred_terms": out.Preferred}, nil
	}}, nil
}
