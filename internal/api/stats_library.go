package api

import (
	"net/http"
	"sort"
	"sync"
	"time"
)

// The Statistics page: what is in the library, at which quality, how much
// space it takes, and how downloads went over the last months. The download
// numbers come from download_stats, which counts downloads as they happen, so
// clearing the history (or letting it expire) does not change them; only
// "Reset statistics" does.

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

// statTotals is every download counted since the statistics began (or were
// reset).
type statTotals struct {
	Grabs     int   `json:"grabs"`
	Completed int   `json:"completed"`
	Failed    int   `json:"failed"`
	Bytes     int64 `json:"bytes"` // of the completed ones
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
	Totals          statTotals  `json:"totals"`
	HistoryKeptDays int         `json:"historyKeptDays"`
	// What gets watched; only while "Read what's been watched" is on.
	Watched *watchStatsPayload `json:"watched,omitempty"`
}

// fileBytes is a file's size from the shared size cache, so counting a big
// library does not ask the disk for every file each time.
func fileBytes(path string) int64 { return cachedFileSize(path, time.Now()) }

// libraryStatsFor is how long the Statistics page's answer is reused.
const libraryStatsFor = 2 * time.Minute

// libraryStatsCache keeps the last answer. The lock is held while one request
// builds the next, so several people opening the page at once share one pass
// over the library.
type libraryStatsCache struct {
	mu  sync.Mutex
	at  time.Time
	out libraryStatsPayload
}

func (s *Server) handleLibraryStats(w http.ResponseWriter, r *http.Request) {
	c := &s.libStats
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) >= libraryStatsFor {
		out, err := s.computeLibraryStats()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		c.out, c.at = out, time.Now()
	}
	writeJSON(w, http.StatusOK, c.out)
}

func (s *Server) computeLibraryStats() (libraryStatsPayload, error) {
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
		return out, err
	}
	out.Movies = len(movies)
	names := map[watchKey]statName{}
	var movieFiles, episodeFiles []statFile
	for _, m := range movies {
		title, _ := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
		names[watchKey{kind: "movie", id: m.ID}] = statName{Title: title, TMDBID: m.TMDBID}
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
		return out, err
	}
	out.Shows = len(shows)
	seen := map[string]bool{} // a multi-episode file counts once for space
	for _, sr := range shows {
		title, _ := s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
		names[watchKey{kind: "series", id: sr.ID}] = statName{Title: title, TMDBID: sr.TMDBID}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return out, err
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

	rows, err := s.db.Query(`SELECT substr(day, 1, 7) AS month, SUM(completed), SUM(failed), SUM(bytes)
		FROM download_stats GROUP BY month ORDER BY month DESC LIMIT 12`)
	if err == nil {
		for rows.Next() {
			var m statMonth
			if rows.Scan(&m.Month, &m.Completed, &m.Failed, &m.Bytes) == nil && (m.Completed > 0 || m.Failed > 0) {
				out.Months = append(out.Months, m)
			}
		}
		rows.Close()
	}
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(grabs), 0), COALESCE(SUM(completed), 0), COALESCE(SUM(failed), 0), COALESCE(SUM(bytes), 0) FROM download_stats`).
		Scan(&out.Totals.Grabs, &out.Totals.Completed, &out.Totals.Failed, &out.Totals.Bytes)
	// The share is among downloads whose protocol is known.
	var usenet, total int
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN protocol = 'usenet' THEN completed ELSE 0 END), 0), COALESCE(SUM(completed), 0) FROM download_stats WHERE protocol IN ('usenet', 'torrent')`).Scan(&usenet, &total)
	if total > 0 {
		out.UsenetShare = usenet * 100 / total
	}
	if s.watchedSyncOn() {
		played, err := s.MovieRepo.ListWatched()
		if err != nil {
			return out, err
		}
		ws := watchStats(movieFiles, episodeFiles, names, played)
		ws.LastSync = s.watchedStatus().LastSync
		out.Watched = &ws
	}
	return out, nil
}
