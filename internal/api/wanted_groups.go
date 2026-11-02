package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The Wanted page and Activity show one row per movie and one per show ("Bhabiji
// Ghar Par Hain, 2,732 episodes missing"), so they ask for the list grouped
// like that, a page at a time, with the totals for the counters. A show's
// episodes are only read when someone unfolds it (?series=ID).

const (
	wantedPageDefault = 50
	wantedPageMax     = 500
)

// wantedRowPayload is one row of the grouped list: a movie, a show ("kind":
// "show", with Episodes), or an episode when it is the only one of its show.
type wantedRowPayload struct {
	wantedItemPayload
	Episodes int `json:"episodes,omitempty"`
}

type wantedPagePayload struct {
	Rows []wantedRowPayload `json:"rows"`
	// Total is how many rows match (all pages); Movies and Episodes how many
	// movies and episodes those rows hold.
	Total    int `json:"total"`
	Movies   int `json:"movies"`
	Episodes int `json:"episodes"`
	Offset   int `json:"offset"`
	Limit    int `json:"limit"`
}

// wantedFilter narrows the grouped list: media "movie" or "tv" (empty: both),
// and words the title must contain.
type wantedFilter struct {
	media string
	text  string
}

func (f wantedFilter) matches(kind, title string) bool {
	switch {
	case f.media == "movie" && kind != "movie", f.media == "tv" && kind == "movie":
		return false
	}
	return f.text == "" || strings.Contains(strings.ToLower(title), f.text)
}

// handleWantedGroups answers GET /api/wanted?group=1: the rows of one page
// (?limit=, default 50, at most 500, 0 for the totals only; ?offset=), the
// movies first, newest added first, then the shows. ?media=movie|tv and ?q=
// narrow it, as the page's chips and search box do.
func (s *Server) handleWantedGroups(w http.ResponseWriter, r *http.Request, kind string) {
	q := r.URL.Query()
	limit, offset := wantedPageDefault, 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a number, 0 or more.")
			return
		}
		limit = min(n, wantedPageMax)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a number, 0 or more.")
			return
		}
		offset = n
	}
	media := q.Get("media")
	if media != "" && media != "movie" && media != "tv" {
		writeError(w, http.StatusBadRequest, `media must be "movie" or "tv"`)
		return
	}
	filter := wantedFilter{media: media, text: strings.ToLower(strings.TrimSpace(q.Get("q")))}

	rows, err := s.wantedRows(r.Context(), kind, filter)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	out := wantedPagePayload{Rows: []wantedRowPayload{}, Total: len(rows), Offset: offset, Limit: limit}
	for _, row := range rows {
		if row.Kind == "movie" {
			out.Movies++
		} else {
			out.Episodes += max(row.Episodes, 1)
		}
	}
	if offset < len(rows) {
		page := rows[offset:min(offset+limit, len(rows))]
		if err := s.finishWantedRows(r.Context(), kind, page); err != nil {
			if r.Context().Err() == nil {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		out.Rows = page
	}
	writeJSON(w, http.StatusOK, out)
}

// wantedRows is every row of the grouped list that matches filter: the movies,
// then one row per show. The episodes are only counted, not read.
func (s *Server) wantedRows(ctx context.Context, kind string, filter wantedFilter) ([]wantedRowPayload, error) {
	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	var rows []wantedRowPayload
	if filter.media != "tv" {
		movies, err := s.wantedMovieItems(ctx, kind, profiles)
		if err != nil {
			return nil, err
		}
		for _, m := range movies {
			if filter.matches("movie", m.Title) {
				rows = append(rows, wantedRowPayload{wantedItemPayload: m})
			}
		}
	}
	if filter.media == "movie" {
		return rows, nil
	}
	shows, err := s.wantedShowRows(ctx, kind, profiles)
	if err != nil {
		return nil, err
	}
	for _, sh := range shows {
		if filter.matches("show", sh.Title) {
			rows = append(rows, sh)
		}
	}
	return rows, nil
}

// wantedShowRows is one row per show with wanted episodes, newest added first.
func (s *Server) wantedShowRows(ctx context.Context, kind string, profiles profileSet) ([]wantedRowPayload, error) {
	show := func(id int64, title string, year int, poster string, profileID, copyOf int64, n int) wantedRowPayload {
		faceTitle, facePoster := title, wantedPoster(poster)
		if sr, err := s.MovieRepo.GetSeries(id); err == nil {
			faceTitle, facePoster = s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
		}
		row := wantedRowPayload{Episodes: n, wantedItemPayload: wantedItemPayload{
			Kind: "show", ID: id, SeriesID: id, Title: faceTitle, Year: year, PosterURL: facePoster,
			ProfileName: profiles.resolveTV(profileID).Name, Copy: copyOf != 0,
		}}
		if row.Copy {
			row.Subtitle = "Second copy"
		}
		return row
	}
	var rows []wantedRowPayload
	if kind == "cutoff" {
		// Only downloaded episodes can be upgraded, and whether one should be
		// depends on its profile, so these are read and grouped here. There
		// are only as many as there are files.
		episodes, err := s.wantedEpisodeItems(ctx, kind, profiles, 0)
		if err != nil {
			return nil, err
		}
		at := map[int64]int{}
		for _, ep := range episodes {
			if i, ok := at[ep.SeriesID]; ok {
				rows[i].Episodes++
				continue
			}
			at[ep.SeriesID] = len(rows)
			row := wantedRowPayload{Episodes: 1, wantedItemPayload: ep}
			row.Kind, row.ID, row.Subtitle = "show", ep.SeriesID, ""
			row.Season, row.Episode, row.Date, row.Quality, row.Cutoff = 0, 0, "", "", ""
			if row.Copy {
				row.Subtitle = "Second copy"
			}
			rows = append(rows, row)
		}
		return rows, nil
	}

	today := time.Now().UTC().Format("2006-01-02")
	shows, err := s.MovieRepo.WantedShows(ctx, today, false)
	if err != nil {
		return nil, err
	}
	// An episode marked missing that still names a file is not wanted when
	// that file is on disk. There are few such episodes; look at them only.
	onDisk := map[int64]int{}
	withFile := false
	for _, sh := range shows {
		withFile = withFile || sh.WithFile > 0
	}
	if withFile {
		files, err := s.MovieRepo.WantedEpisodeFiles(ctx, today, false)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if fileExists(f.Path) {
				onDisk[f.SeriesID]++
			}
		}
	}
	for _, sh := range shows {
		if n := sh.Episodes - onDisk[sh.ID]; n > 0 {
			rows = append(rows, show(sh.ID, sh.Title, sh.Year, sh.PosterPath, sh.ProfileID, sh.CopyOf, n))
		}
	}
	return rows, nil
}

// finishWantedRows fills in what one page shows beyond the counts: the last
// search of each title, and for a show with a single wanted episode, that
// episode (the page lists it as the episode rather than as a show).
func (s *Server) finishWantedRows(ctx context.Context, kind string, page []wantedRowPayload) error {
	profiles, err := s.loadProfiles()
	if err != nil {
		return err
	}
	for i := range page {
		if page[i].Kind != "show" || page[i].Episodes != 1 {
			continue
		}
		eps, err := s.wantedEpisodeItems(ctx, kind, profiles, page[i].SeriesID)
		if err != nil {
			return err
		}
		if len(eps) == 1 {
			page[i] = wantedRowPayload{wantedItemPayload: eps[0]}
		}
	}
	items := make([]wantedItemPayload, len(page))
	for i := range page {
		items[i] = page[i].wantedItemPayload
	}
	s.addLastSearches(items)
	for i := range page {
		page[i].LastSearch, page[i].LastSearchAt = items[i].LastSearch, items[i].LastSearchAt
	}
	return nil
}
