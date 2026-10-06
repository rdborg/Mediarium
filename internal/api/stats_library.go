package api

import (
	"net/http"
	"os"
	"sort"
)

// The Statistics page: what is in the library, at which quality, how much
// space it takes, and how downloads went over the last months (as far back
// as the history is kept).

type statCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes,omitempty"`
}

type statMonth struct {
	Month     string `json:"month"` // "2026-10"
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Bytes     int64  `json:"bytes"` // of the completed ones
}

type libraryStatsPayload struct {
	Movies          int         `json:"movies"`
	MoviesHave      int         `json:"moviesHave"`
	Shows           int         `json:"shows"`
	Episodes        int         `json:"episodes"`
	EpisodesHave    int         `json:"episodesHave"`
	Albums          int         `json:"albums"`
	AlbumsHave      int         `json:"albumsHave"`
	MovieBytes      int64       `json:"movieBytes"`
	EpisodeBytes    int64       `json:"episodeBytes"`
	Quality         []statCount `json:"quality"` // movies and episodes together, by quality
	Months          []statMonth `json:"months"`
	UsenetShare     int         `json:"usenetShare"` // % of completed downloads that came from Usenet
	HistoryKeptDays int         `json:"historyKeptDays"`
	// What gets watched; only while "Read what's been watched" is on.
	Watched *watchStatsPayload `json:"watched,omitempty"`
}

func fileBytes(path string) int64 {
	if path == "" {
		return 0
	}
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

func (s *Server) handleLibraryStats(w http.ResponseWriter, r *http.Request) {
	out := libraryStatsPayload{HistoryKeptDays: s.historyRetentionDays(), Quality: []statCount{}, Months: []statMonth{}}
	byQuality := map[string]*statCount{}
	addQuality := func(q string, size int64) {
		if q == "" {
			q = "Unknown"
		}
		c := byQuality[q]
		if c == nil {
			c = &statCount{Label: q}
			byQuality[q] = c
		}
		c.Count++
		c.Bytes += size
	}

	movies, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Movies = len(movies)
	names := map[watchKey]statName{}
	var movieFiles, episodeFiles []statFile
	for _, m := range movies {
		names[watchKey{kind: "movie", id: m.ID}] = statName{Title: m.Title, TMDBID: m.TMDBID}
		if m.FilePath == "" {
			continue
		}
		out.MoviesHave++
		size := fileBytes(m.FilePath)
		out.MovieBytes += size
		addQuality(m.Quality, size)
		movieFiles = append(movieFiles, statFile{TitleID: m.ID, Bytes: size})
	}

	shows, err := s.MovieRepo.ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Shows = len(shows)
	seen := map[string]bool{} // a multi-episode file counts once for space
	for _, sr := range shows {
		names[watchKey{kind: "series", id: sr.ID}] = statName{Title: sr.Title}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, ep := range eps {
			if ep.Season == 0 {
				continue
			}
			out.Episodes++
			if ep.FilePath == "" {
				continue
			}
			out.EpisodesHave++
			size := int64(0)
			if !seen[ep.FilePath] {
				seen[ep.FilePath] = true
				size = fileBytes(ep.FilePath)
				out.EpisodeBytes += size
			}
			addQuality(ep.Quality, size)
			episodeFiles = append(episodeFiles, statFile{TitleID: sr.ID, Season: ep.Season, Episode: ep.Episode, Bytes: size})
		}
	}

	if s.musicEnabled() {
		_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'downloaded' THEN 1 ELSE 0 END), 0) FROM albums WHERE monitored = 1`).Scan(&out.Albums, &out.AlbumsHave)
	}

	for _, c := range byQuality {
		out.Quality = append(out.Quality, *c)
	}
	sort.Slice(out.Quality, func(i, j int) bool { return out.Quality[i].Count > out.Quality[j].Count })

	rows, err := s.db.Query(`SELECT substr(COALESCE(completed_at, added_at), 1, 7) AS month,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
			COALESCE(SUM(CASE WHEN status = 'completed' THEN size_bytes ELSE 0 END), 0)
		FROM download_queue GROUP BY month ORDER BY month DESC LIMIT 12`)
	if err == nil {
		for rows.Next() {
			var m statMonth
			if rows.Scan(&m.Month, &m.Completed, &m.Failed, &m.Bytes) == nil && (m.Completed > 0 || m.Failed > 0) {
				out.Months = append(out.Months, m)
			}
		}
		rows.Close()
	}
	var usenet, total int
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN protocol = 'usenet' THEN 1 ELSE 0 END), 0), COUNT(*) FROM download_queue WHERE status = 'completed'`).Scan(&usenet, &total)
	if total > 0 {
		out.UsenetShare = usenet * 100 / total
	}
	if s.watchedSyncOn() {
		played, err := s.MovieRepo.ListWatched()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ws := watchStats(movieFiles, episodeFiles, names, played)
		ws.LastSync = s.watchedStatus().LastSync
		out.Watched = &ws
	}
	writeJSON(w, http.StatusOK, out)
}
