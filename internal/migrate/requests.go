package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/library"
)

// Overseerr/Jellyseerr and Ombi hold what people asked for. Requests that
// are not downloaded yet become monitored titles in Mediarium (a movie by
// TMDB id; a show by TMDB id with the requested seasons monitored, future
// episodes included), and the item's own events say who asked. Requests
// that are already available there are skipped: the files exist and are
// found by Import existing.

// requestEntry is one requested title as an app reports it, its requests
// merged.
type requestEntry struct {
	Title     string
	Year      int
	MediaType string // movie or tv
	TMDBID    int
	TVDBID    int
	Status    string
	By        []string
	Seasons   []int
}

type requestPlan struct {
	item RequestItem
	app  string
}

// requestRank orders statuses so the most advanced of several requests for
// the same title wins.
func requestRank(status string) int {
	switch status {
	case RequestAvailable:
		return 6
	case RequestPartiallyAvailable:
		return 5
	case RequestProcessing:
		return 4
	case RequestApproved:
		return 3
	case RequestPending:
		return 2
	case RequestDeclined, RequestFailed:
		return 1
	}
	return 0
}

// mergeRequests joins several requests for the same title (by TMDB id, or by
// TVDB id when there is none) into one entry, in the order first seen.
func mergeRequests(in []requestEntry) []requestEntry {
	var out []requestEntry
	index := map[string]int{}
	for _, e := range in {
		key := fmt.Sprintf("%s:tmdb:%d", e.MediaType, e.TMDBID)
		if e.TMDBID <= 0 {
			key = fmt.Sprintf("%s:tvdb:%d", e.MediaType, e.TVDBID)
		}
		i, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, e)
			continue
		}
		m := &out[i]
		if requestRank(e.Status) > requestRank(m.Status) {
			m.Status = e.Status
		}
		if m.Title == "" {
			m.Title, m.Year = e.Title, e.Year
		}
		if m.TVDBID == 0 {
			m.TVDBID = e.TVDBID
		}
		for _, name := range e.By {
			if !contains(m.By, name) {
				m.By = append(m.By, name)
			}
		}
		for _, s := range e.Seasons {
			if !containsInt(m.Seasons, s) {
				m.Seasons = append(m.Seasons, s)
			}
		}
	}
	for i := range out {
		sort.Ints(out[i].Seasons)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsInt(list []int, n int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}

// nameList joins names the way a sentence does: "A", "A and B", "A, B and C".
func nameList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// requestedMessage is the line written to a title's events.
func requestedMessage(by []string, app string) string {
	if len(by) == 0 {
		return "Requested in " + app
	}
	return "Requested by " + nameList(by) + " in " + app
}

// nameFor is a title as TMDB names it (remembered for the whole import).
func (im *Importer) nameFor(ctx context.Context, media string, tmdbID int) (titleName, bool) {
	key := fmt.Sprintf("%s:%d", media, tmdbID)
	im.namesMu.Lock()
	n, ok := im.names[key]
	im.namesMu.Unlock()
	if ok {
		return n, n.Title != ""
	}
	if im.deps.TMDB == nil || !im.deps.TMDB().HasAPIKey() {
		return titleName{}, false
	}
	tmdb := im.deps.TMDB()
	if media == "movie" {
		if m, err := tmdb.GetMovie(ctx, tmdbID); err == nil {
			n = titleName{Title: m.Title, Year: m.Year()}
		}
	} else if s, err := tmdb.GetShow(ctx, tmdbID); err == nil {
		n = titleName{Title: s.Name, Year: s.Year()}
	}
	im.namesMu.Lock()
	im.names[key] = n
	im.namesMu.Unlock()
	return n, n.Title != ""
}

// planRequests works out what to do with each requested title of app.
// seen carries the titles already planned by another app in this import.
func (im *Importer) planRequests(ctx context.Context, p *plan, rp *RequestsPreview, app string, entries []requestEntry, st state, seen map[string]bool) {
	entries = mergeRequests(entries)
	lookupErrs := make([]error, len(entries))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, lookupWorkers)
	)
	// Shows known only by TVDB id (Ombi) are looked up on TMDB.
	for i := range entries {
		e := &entries[i]
		if e.MediaType != "tv" || e.TMDBID > 0 || e.TVDBID <= 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			e.TMDBID, lookupErrs[i] = im.tmdbForTVDB(ctx, e.TVDBID)
		}()
	}
	wg.Wait()

	// Names: from Mediarium's library when it has the title, otherwise from
	// TMDB (only for the requests that would be added, to save lookups).
	for i := range entries {
		e := &entries[i]
		if e.Title != "" || e.TMDBID == 0 {
			continue
		}
		if m, ok := st.movies[e.TMDBID]; ok && e.MediaType == "movie" {
			e.Title, e.Year = m.Title, m.Year
			continue
		}
		if s, ok := st.series[e.TMDBID]; ok && e.MediaType == "tv" {
			e.Title, e.Year = s.Title, s.Year
			continue
		}
		switch e.Status {
		case RequestPending, RequestApproved, RequestProcessing:
		default:
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if n, ok := im.nameFor(ctx, e.MediaType, e.TMDBID); ok {
				e.Title, e.Year = n.Title, n.Year
			}
		}()
	}
	wg.Wait()

	for i, e := range entries {
		item := RequestItem{
			Title: e.Title, Year: e.Year, MediaType: e.MediaType, TMDBID: e.TMDBID, TVDBID: e.TVDBID,
			Status: e.Status, RequestedBy: e.By, Seasons: e.Seasons,
		}
		if item.RequestedBy == nil {
			item.RequestedBy = []string{}
		}
		if item.Title == "" {
			switch {
			case e.TMDBID > 0 && e.MediaType == "movie":
				item.Title = fmt.Sprintf("Movie %d (TMDB)", e.TMDBID)
			case e.TMDBID > 0:
				item.Title = fmt.Sprintf("Show %d (TMDB)", e.TMDBID)
			default:
				item.Title = fmt.Sprintf("Show %d (TVDB)", e.TVDBID)
			}
		}
		_, inMovies := st.movies[e.TMDBID]
		_, inSeries := st.series[e.TMDBID]
		exists := (e.MediaType == "movie" && inMovies) || (e.MediaType == "tv" && inSeries)
		key := fmt.Sprintf("%s:%d", e.MediaType, e.TMDBID)
		switch {
		case e.Status == RequestAvailable:
			item.Action, item.Reason = ActionSkip, "already downloaded there (link its files with Import existing)"
		case e.Status == RequestPartiallyAvailable:
			item.Action, item.Reason = ActionSkip, "partly downloaded there: add it by hand, or import it from Radarr/Sonarr, so its files are found"
		case e.Status == RequestDeclined:
			item.Action, item.Reason = ActionSkip, "declined in "+app
		case e.Status == RequestFailed:
			item.Action, item.Reason = ActionSkip, "failed in "+app
		case e.Status == RequestBlocked:
			item.Action, item.Reason = ActionSkip, "blocklisted in "+app
		case lookupErrs[i] != nil:
			item.Action, item.Reason = ActionSkip, "TMDB lookup failed: "+lookupErrs[i].Error()
		case e.TMDBID == 0 && e.TVDBID > 0:
			item.Action, item.Reason = ActionSkip, fmt.Sprintf("no TMDB id: TMDB has no show with TVDB id %d", e.TVDBID)
		case e.TMDBID == 0:
			item.Action, item.Reason = ActionSkip, "no TMDB id"
		case exists:
			item.Action, item.Reason = ActionExists, "already in your library"
		case seen[key]:
			item.Action, item.Reason = ActionExists, "already added from another app in this import"
		default:
			seen[key] = true
			item.Action = ActionAdd
			switch e.Status {
			case RequestPending:
				item.Reason = "waiting for approval in " + app + "; added as wanted, no search is started"
			case RequestProcessing:
				item.Reason = "already being downloaded through " + app + "; added as wanted too, so it does not get fetched twice"
			default:
				item.Reason = "approved in " + app + " but not downloaded yet; added as wanted, no search is started"
			}
			if len(e.Seasons) > 0 && e.MediaType == "tv" {
				item.Reason += fmt.Sprintf("; only the requested seasons (%s) are monitored", seasonList(e.Seasons))
			}
		}
		rp.Summary.count(item.Action)
		rp.Items = append(rp.Items, item)
		p.requests = append(p.requests, requestPlan{item: item, app: app})
	}
}

func seasonList(seasons []int) string {
	parts := make([]string, len(seasons))
	for i, s := range seasons {
		parts[i] = fmt.Sprint(s)
	}
	return strings.Join(parts, ", ")
}

// importRequest adds one requested title as a monitored title and writes who
// asked for it to its events.
func (im *Importer) importRequest(ctx context.Context, rq requestPlan) (outcome, reason string) {
	switch rq.item.Action {
	case ActionSkip:
		return OutcomeSkipped, rq.item.Reason
	case ActionExists:
		return OutcomeExists, rq.item.Reason
	}
	lib := im.deps.Library
	if im.deps.TMDB == nil || !im.deps.TMDB().HasAPIKey() {
		return OutcomeFailed, "no TMDB API key has been added yet"
	}
	tmdb := im.deps.TMDB()
	msg := requestedMessage(rq.item.RequestedBy, rq.app)
	id := rq.item.TMDBID

	if rq.item.MediaType == "movie" {
		if _, found, err := lib.GetByTMDBID(id); err != nil {
			return OutcomeFailed, err.Error()
		} else if found {
			return OutcomeExists, "already in your library"
		}
		tm, err := tmdb.GetMovie(ctx, id)
		if err != nil {
			return OutcomeFailed, "couldn't look up the movie on TMDB: " + err.Error()
		}
		m, err := lib.Add(library.Movie{
			TMDBID: tm.TMDBID, Title: tm.Title, Year: tm.Year(), Overview: tm.Overview, PosterPath: tm.PosterPath,
			Monitored: true, ReleaseDate: tm.ReleaseDate, Genres: tmdb.MovieGenres(ctx, *tm),
		})
		if err != nil {
			return OutcomeFailed, err.Error()
		}
		im.activity(m.ID, 0, msg)
		return OutcomeAdded, "added as wanted; " + msg + "; no search started"
	}

	if _, found, err := lib.GetSeriesByTMDBID(id); err != nil {
		return OutcomeFailed, err.Error()
	} else if found {
		return OutcomeExists, "already in your library"
	}
	detail, infos, err := tmdb.GetShowEpisodes(ctx, id)
	if err != nil {
		return OutcomeFailed, "couldn't look up the show on TMDB: " + err.Error()
	}
	eps := make([]library.Episode, len(infos))
	for i, e := range infos {
		eps[i] = library.Episode{Season: e.Season, Episode: e.Episode, Title: e.Name, Overview: e.Overview, AirDate: e.AirDate}
	}
	var genreIDs []int
	var genreNames []string
	for _, g := range detail.Genres {
		genreIDs, genreNames = append(genreIDs, g.ID), append(genreNames, g.Name)
	}
	series, err := lib.AddSeries(library.Series{
		TMDBID: detail.TMDBID, Title: detail.Name, Year: detail.Year(), Overview: detail.Overview,
		PosterPath: detail.PosterPath, FirstAirDate: detail.FirstAirDate, Monitored: true,
		Genres:     tmdb.ShowGenres(ctx, detail.Show),
		SeriesType: library.GuessSeriesType(genreIDs, genreNames, detail.OriginCountry, detail.Type),
	}, eps)
	if err != nil {
		return OutcomeFailed, err.Error()
	}
	// Seasons that were not requested stay unmonitored.
	if len(rq.item.Seasons) > 0 {
		wanted := map[int]bool{}
		for _, s := range rq.item.Seasons {
			wanted[s] = true
		}
		seasons := map[int]bool{}
		for _, e := range infos {
			seasons[e.Season] = true
		}
		for n := range seasons {
			if n > 0 && !wanted[n] {
				if err := lib.SetSeasonMonitored(series.ID, n, false); err != nil {
					slog.Warn("migrate: unmonitor season", "series", series.Title, "season", n, "err", err)
				}
			}
		}
	}
	im.activity(0, series.ID, msg)
	return OutcomeAdded, "added as wanted; " + msg + "; no search started"
}
