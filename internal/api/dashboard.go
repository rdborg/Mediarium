package api

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ryanborg/mediarium/internal/fsinfo"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/organizer"
	"github.com/ryanborg/mediarium/internal/queue"
)

// libraryStats is the (cached) result of walking every downloaded file once:
// how much space the library uses and how it splits by quality.
type libraryStats struct {
	at         time.Time
	movieBytes int64
	tvBytes    int64
	qualities  map[string]int
}

const libraryStatsTTL = 60 * time.Second

type statsCache struct {
	mu    sync.Mutex
	stats libraryStats
}

func (s *Server) libraryStats() (libraryStats, error) {
	s.statsCache.mu.Lock()
	defer s.statsCache.mu.Unlock()
	if time.Since(s.statsCache.stats.at) < libraryStatsTTL && s.statsCache.stats.qualities != nil {
		return s.statsCache.stats, nil
	}
	st := libraryStats{at: time.Now(), qualities: map[string]int{}}
	sizeOf := func(path string) int64 {
		if path == "" {
			return 0
		}
		if fi, err := os.Stat(path); err == nil {
			return fi.Size()
		}
		return 0
	}
	movies, err := s.MovieRepo.List()
	if err != nil {
		return st, err
	}
	for _, m := range movies {
		if m.Status == library.StatusDownloaded {
			st.movieBytes += sizeOf(m.FilePath)
			if tierKnown(m.Quality) {
				st.qualities[m.Quality]++
			} else {
				st.qualities["Unknown"]++
			}
		}
	}
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		return st, err
	}
	for _, sr := range seriesList {
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return st, err
		}
		seenFiles := map[string]bool{} // a multi-episode file is one file
		for _, ep := range eps {
			if ep.Status != library.StatusDownloaded {
				continue
			}
			if ep.FilePath != "" && !seenFiles[ep.FilePath] {
				seenFiles[ep.FilePath] = true
				st.tvBytes += sizeOf(ep.FilePath)
			}
			if tierKnown(ep.Quality) {
				st.qualities[ep.Quality]++
			} else {
				st.qualities["Unknown"]++
			}
		}
	}
	s.statsCache.stats = st
	return st, nil
}

type dashFolder struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Path         string   `json:"path"`
	Exists       bool     `json:"exists"`
	Writable     bool     `json:"writable"`
	FreeBytes    uint64   `json:"freeBytes"`
	TotalBytes   uint64   `json:"totalBytes"`
	Mounted      bool     `json:"mounted"`
	MountKnown   bool     `json:"mountKnown"`
	LibraryBytes int64    `json:"libraryBytes"`
	Items        int      `json:"items"`
	ItemsLabel   string   `json:"itemsLabel"`
	Hardlinks    *bool    `json:"hardlinks,omitempty"` // shares a drive with Downloads, so imports don't copy
	Warnings     []string `json:"warnings"`
}

type dashActive struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Subtitle    string  `json:"subtitle,omitempty"`
	PosterURL   string  `json:"posterUrl,omitempty"`
	Status      string  `json:"status"`
	Protocol    string  `json:"protocol"`
	ProgressPct float64 `json:"progressPct"`
	SizeBytes   int64   `json:"sizeBytes"`
	Release     string  `json:"release"`
}

type dashRecent struct {
	Kind      string `json:"kind"`
	ID        int64  `json:"id"`
	TMDBID    int    `json:"tmdbId,omitempty"`
	SeriesID  int64  `json:"seriesId,omitempty"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Year      int    `json:"year,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	Quality   string `json:"quality,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	At        string `json:"at"`
}

type dashQuality struct {
	Tier  string `json:"tier"`
	Count int    `json:"count"`
}

type dashboardPayload struct {
	Library struct {
		Movies struct {
			Total       int `json:"total"`
			Downloaded  int `json:"downloaded"`
			Missing     int `json:"missing"`
			Downloading int `json:"downloading"`
		} `json:"movies"`
		Series struct {
			Total              int `json:"total"`
			Episodes           int `json:"episodes"`
			EpisodesDownloaded int `json:"episodesDownloaded"`
			EpisodesMissing    int `json:"episodesMissing"`
		} `json:"series"`
		SizeBytes int64         `json:"sizeBytes"`
		Qualities []dashQuality `json:"qualities"`
	} `json:"library"`
	Folders         []dashFolder           `json:"folders"`
	Active          []dashActive           `json:"active"`
	RecentlyAdded   []dashRecent           `json:"recentlyAdded"`
	RecentDownloads []dashRecent           `json:"recentDownloads"`
	Upcoming        []calendarEntryPayload `json:"upcoming"`
	Health          []healthItem           `json:"health"`
	Setup           struct {
		Indexers int  `json:"indexers"`
		Servers  int  `json:"usenetServers"`
		VPN      bool `json:"vpnConnected"`
	} `json:"setup"`
}

// handleDashboard gathers everything the landing page shows in one request:
// library totals, the connected folders and their disk space, what is
// downloading now, what was added and downloaded recently, what airs next,
// and anything that is misconfigured (with what stops working because of it).
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	var out dashboardPayload

	movies, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	moviesDownloaded := 0
	for _, m := range movies {
		out.Library.Movies.Total++
		switch m.Status {
		case library.StatusDownloaded:
			out.Library.Movies.Downloaded++
			moviesDownloaded++
		case library.StatusDownloading:
			out.Library.Movies.Downloading++
		default:
			out.Library.Movies.Missing++
		}
	}
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Library.Series.Total = len(seriesList)
	for _, sr := range seriesList {
		out.Library.Series.Episodes += sr.EpisodeCount
		out.Library.Series.EpisodesDownloaded += sr.DownloadedCount
	}
	out.Library.Series.EpisodesMissing = out.Library.Series.Episodes - out.Library.Series.EpisodesDownloaded

	stats, err := s.libraryStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Library.SizeBytes = stats.movieBytes + stats.tvBytes
	out.Library.Qualities = []dashQuality{}
	for tier, n := range stats.qualities {
		out.Library.Qualities = append(out.Library.Qualities, dashQuality{Tier: tier, Count: n})
	}
	sort.Slice(out.Library.Qualities, func(i, j int) bool {
		if out.Library.Qualities[i].Count != out.Library.Qualities[j].Count {
			return out.Library.Qualities[i].Count > out.Library.Qualities[j].Count
		}
		return out.Library.Qualities[i].Tier < out.Library.Qualities[j].Tier
	})

	// Folders.
	movieRoot, tvRoot, dlRoot := s.moviesRoot(), s.tvRoot(), s.downloadsRoot()
	mkFolder := func(key, label, path string, bytes int64, items int, itemsLabel string) dashFolder {
		f := fsinfo.Inspect(path)
		d := dashFolder{
			Key: key, Label: label, Path: path, Exists: f.Exists && f.IsDir, Writable: f.Writable,
			FreeBytes: f.FreeBytes, TotalBytes: f.TotalBytes, Mounted: f.Mounted, MountKnown: f.MountKnown,
			LibraryBytes: bytes, Items: items, ItemsLabel: itemsLabel, Warnings: f.Warnings,
		}
		if d.Warnings == nil {
			d.Warnings = []string{}
		}
		if key != "downloads" && d.Exists {
			if same, supported, err := organizer.SameFilesystem(dlRoot, path); err == nil && supported {
				d.Hardlinks = &same
			}
		}
		return d
	}
	out.Folders = []dashFolder{
		mkFolder("movies", "Movies", movieRoot, stats.movieBytes, moviesDownloaded, "movies"),
		mkFolder("tv", "TV shows", tvRoot, stats.tvBytes, out.Library.Series.EpisodesDownloaded, "episodes"),
		mkFolder("downloads", "Downloads", dlRoot, 0, 0, "active"),
	}

	// Downloads in flight.
	out.Active = []dashActive{}
	queueItems, err := s.QueueRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, it := range queueItems {
		if it.Status == queue.StatusCompleted || it.Status == queue.StatusFailed {
			continue
		}
		title, subtitle, poster := s.describeQueueItem(it)
		out.Active = append(out.Active, dashActive{
			ID: it.ID, Title: title, Subtitle: subtitle, PosterURL: poster, Status: string(it.Status),
			Protocol: string(it.Protocol), ProgressPct: it.ProgressPct, SizeBytes: it.SizeBytes, Release: it.ReleaseTitle,
		})
	}
	for i := range out.Folders {
		if out.Folders[i].Key == "downloads" {
			out.Folders[i].Items = len(out.Active)
		}
	}

	// Recently added and recently downloaded.
	out.RecentlyAdded = []dashRecent{}
	if recent, err := s.MovieRepo.RecentlyAdded(12); err == nil {
		for _, it := range recent {
			d := dashRecent{Kind: it.Kind, ID: it.ID, Title: it.Title, Year: it.Year, PosterURL: metadata.PosterURL(it.PosterPath), At: it.AddedAt}
			if it.Kind == "movie" {
				d.TMDBID = it.TMDBID
			} else {
				d.SeriesID = it.ID
			}
			out.RecentlyAdded = append(out.RecentlyAdded, d)
		}
	}
	out.RecentDownloads = []dashRecent{}
	if done, err := s.QueueRepo.RecentCompleted(8); err == nil {
		for _, c := range done {
			title, subtitle, poster := s.describeQueueItem(c.Item)
			d := dashRecent{Kind: "movie", ID: c.ID, Title: title, Subtitle: subtitle, PosterURL: poster, SizeBytes: c.SizeBytes, At: c.CompletedAt}
			if c.SeriesID > 0 {
				d.Kind, d.SeriesID = "series", c.SeriesID
			} else if m, err := s.MovieRepo.Get(c.MovieID); err == nil {
				d.TMDBID, d.Year, d.Quality = m.TMDBID, m.Year, m.Quality
			}
			out.RecentDownloads = append(out.RecentDownloads, d)
		}
	}

	// Coming up.
	out.Upcoming = []calendarEntryPayload{}
	if entries, err := s.calendarEntries(); err == nil {
		today := time.Now().UTC().Format("2006-01-02")
		for _, e := range entries {
			if e.ReleaseDate >= today && len(out.Upcoming) < 6 {
				out.Upcoming = append(out.Upcoming, e)
			}
		}
	}

	// Members see the library, downloads and what is coming up, but not setup
	// problems or where things live on the server: they cannot change either.
	if isAdminRequest(r) {
		out.Health = s.collectHealth()
	} else {
		for i := range out.Folders {
			out.Folders[i].Path, out.Folders[i].Warnings = "", []string{}
		}
	}
	if out.Health == nil {
		out.Health = []healthItem{}
	}
	if list, err := s.IndexerRepo.List(); err == nil {
		for _, inst := range list {
			if inst.Enabled {
				out.Setup.Indexers++
			}
		}
	}
	if servers, err := s.ClientRepo.List(); err == nil {
		out.Setup.Servers = len(servers)
	}
	out.Setup.VPN = s.VPNManager.Status().Connected

	writeJSON(w, http.StatusOK, out)
}

// describeQueueItem names a queue entry after the movie or show it is for
// (falling back to the release title) and finds its poster.
func (s *Server) describeQueueItem(it queue.Item) (title, subtitle, poster string) {
	title = it.ReleaseTitle
	switch {
	case it.SeriesID > 0:
		if sr, err := s.MovieRepo.GetSeries(it.SeriesID); err == nil {
			title, poster = sr.Title, metadata.PosterURL(sr.PosterPath)
			switch {
			case it.Season > 0 && it.Episode > 0:
				subtitle = fmt.Sprintf("S%02dE%02d", it.Season, it.Episode)
			case it.Season > 0:
				subtitle = fmt.Sprintf("Season %d", it.Season)
			}
		}
	case it.MovieID > 0:
		if m, err := s.MovieRepo.Get(it.MovieID); err == nil {
			title, poster = m.Title, metadata.PosterURL(m.PosterPath)
			if m.Year > 0 {
				subtitle = fmt.Sprintf("%d", m.Year)
			}
		}
	}
	return title, subtitle, poster
}
