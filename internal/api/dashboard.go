package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/queue"
)

// libraryStats is the (cached) result of walking every downloaded file once:
// how much space the library uses and how it splits by quality.
type libraryStats struct {
	at         time.Time
	movieBytes int64
	tvBytes    int64
	qualities  map[string]int
	// What is in each movie and TV folder (the main one and the extra ones),
	// by the folder's path: the space its files take and how many movies or
	// episodes are in it. A file outside every folder counts under "".
	movieBytesByRoot map[string]int64
	moviesByRoot     map[string]int
	tvBytesByRoot    map[string]int64
	episodesByRoot   map[string]int
}

const libraryStatsTTL = 60 * time.Second

type statsCache struct {
	mu         sync.Mutex
	stats      libraryStats
	refreshing bool
	first      *statsWalk // the very first walk, while it runs
}

// statsWalk is one walk that several callers wait for together.
type statsWalk struct {
	done  chan struct{}
	stats libraryStats
	err   error
}

// libraryStats returns the library totals. They come from a walk over every
// downloaded file, which on a big library (or a slow disk) takes a while, so
// the answer is kept for libraryStatsTTL and, once it is older, still handed
// out at once while a fresh one is made in the background. Only the very
// first call waits for the walk.
func (s *Server) libraryStats() (libraryStats, error) {
	s.statsCache.mu.Lock()
	cached := s.statsCache.stats
	have := cached.qualities != nil
	if have && time.Since(cached.at) < libraryStatsTTL {
		s.statsCache.mu.Unlock()
		return cached, nil
	}
	if have {
		if !s.statsCache.refreshing {
			s.statsCache.refreshing = true
			go func() {
				st, err := s.walkLibraryStats()
				s.statsCache.mu.Lock()
				if err == nil {
					s.statsCache.stats = st
				}
				s.statsCache.refreshing = false
				s.statsCache.mu.Unlock()
			}()
		}
		s.statsCache.mu.Unlock()
		return cached, nil
	}
	// Nothing is kept yet. Callers that arrive while the first walk runs (a
	// few browser tabs after a restart, a monitoring scrape) wait for that one
	// instead of each starting a walk of their own.
	return s.statsCache.firstWalk(s.walkLibraryStats)
}

// firstWalk runs walk, or waits for the walk another caller already started,
// and keeps its result. The caller holds c.mu; firstWalk releases it.
func (c *statsCache) firstWalk(walk func() (libraryStats, error)) (libraryStats, error) {
	if w := c.first; w != nil {
		c.mu.Unlock()
		<-w.done
		return w.stats, w.err
	}
	w := &statsWalk{done: make(chan struct{}), err: errors.New("the statistics could not be worked out")}
	c.first = w
	c.mu.Unlock()
	defer func() { // also when walk panics, so nobody waits for ever
		c.mu.Lock()
		if w.err == nil {
			c.stats = w.stats
		}
		c.first = nil
		c.mu.Unlock()
		close(w.done)
	}()
	w.stats, w.err = walk()
	return w.stats, w.err
}

// walkLibraryStats adds up the size and quality of every downloaded file.
func (s *Server) walkLibraryStats() (libraryStats, error) {
	st := libraryStats{
		at: time.Now(), qualities: map[string]int{},
		movieBytesByRoot: map[string]int64{}, moviesByRoot: map[string]int{}, tvBytesByRoot: map[string]int64{}, episodesByRoot: map[string]int{},
	}
	movieRoots, tvRoots := s.movieFolders(), s.tvFolders()
	sizeOf := func(path string) int64 {
		if path == "" {
			return 0
		}
		if fi, err := os.Stat(path); err == nil {
			return fi.Size()
		}
		return 0
	}
	count := func(q string) {
		if tierKnown(q) {
			st.qualities[q]++
		} else {
			st.qualities["Unknown"]++
		}
	}
	movies, err := s.MovieRepo.DownloadedMovieFiles()
	if err != nil {
		return st, err
	}
	for _, m := range movies {
		size, root := sizeOf(m.Path), rootContaining(movieRoots, m.Path)
		st.movieBytes += size
		st.movieBytesByRoot[root] += size
		st.moviesByRoot[root]++
		count(m.Quality)
	}
	episodes, err := s.MovieRepo.DownloadedEpisodeFiles()
	if err != nil {
		return st, err
	}
	seenFiles := map[string]bool{} // a multi-episode file is one file
	for _, ep := range episodes {
		root := rootContaining(tvRoots, ep.Path)
		if ep.Path != "" && !seenFiles[ep.Path] {
			seenFiles[ep.Path] = true
			size := sizeOf(ep.Path)
			st.tvBytes += size
			st.tvBytesByRoot[root] += size
		}
		st.episodesByRoot[root]++
		count(ep.Quality)
	}
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
	// Off: that media type is switched off (Settings > Media types).
	Off bool `json:"off,omitempty"`
	// NewTitles: titles of this kind added without a folder choice go here
	// right now (only sent when the kind has more than one folder).
	NewTitles bool `json:"newTitles,omitempty"`
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

type dashBookCounts struct {
	Books      int `json:"books"`
	Downloaded int `json:"downloaded"`
	Missing    int `json:"missing"`
}

// recentShown is how many recently added titles each list holds; recentScan
// how many of the newest are looked at to fill both halves.
const (
	recentShown = 12
	recentScan  = 150
)

type dashRecent struct {
	Kind      string `json:"kind"`
	ID        int64  `json:"id"`
	TMDBID    int    `json:"tmdbId,omitempty"`
	SeriesID  int64  `json:"seriesId,omitempty"`
	AlbumID   int64  `json:"albumId,omitempty"` // kind "album" (music module)
	BookID    int64  `json:"bookId,omitempty"`  // kind "book"
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Year      int    `json:"year,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	Quality   string `json:"quality,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	At        string `json:"at"`
	// State (recently added only): downloaded, downloading, partial, missing or unmonitored.
	State string `json:"state,omitempty"`
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
		// Wanted is what the Wanted page lists under Missing: monitored, no
		// file and (for episodes) already aired. Movies.Missing and
		// Series.EpisodesMissing count everything without a file.
		Wanted struct {
			Movies   int `json:"movies"`
			Episodes int `json:"episodes"`
		} `json:"wanted"`
		// Music is counted from the database whether or not the music module is
		// switched on, so the dashboard can always show it (zeros for none).
		Music struct {
			Artists    int `json:"artists"`
			Albums     int `json:"albums"`
			Downloaded int `json:"downloaded"` // albums with their files
			Missing    int `json:"missing"`    // albums neither downloaded nor downloading
		} `json:"music"`
		// Ebooks and Audiobooks count the books wanted in each format.
		Ebooks     dashBookCounts `json:"ebooks"`
		Audiobooks dashBookCounts `json:"audiobooks"`
		SizeBytes  int64          `json:"sizeBytes"`
		Qualities  []dashQuality  `json:"qualities"`
	} `json:"library"`
	Folders       []dashFolder `json:"folders"`
	Active        []dashActive `json:"active"`
	RecentlyAdded []dashRecent `json:"recentlyAdded"`
	// The same, split: titles with files in the library (downloaded, or a
	// show with some episodes), and titles still to come (waiting for a
	// release, downloading, or not monitored). Up to 12 each.
	RecentInLibrary []dashRecent           `json:"recentInLibrary"`
	RecentToCome    []dashRecent           `json:"recentToCome"`
	RecentDownloads []dashRecent           `json:"recentDownloads"`
	Upcoming        []calendarEntryPayload `json:"upcoming"`
	Health          []healthItem           `json:"health"`
	Setup           struct {
		Indexers int  `json:"indexers"`
		Servers  int  `json:"usenetServers"`
		VPN      bool `json:"vpnConnected"`
		// Torrents says torrent downloads can work: the torrent client is on
		// and a torrent indexer is enabled. MediaServers is how many
		// Plex, Jellyfin or Emby servers are connected. The "Getting started"
		// list uses both.
		Torrents     bool `json:"torrentsReady"`
		MediaServers int  `json:"mediaServers"`
	} `json:"setup"`
}

// handleDashboard gathers everything the landing page shows in one request:
// library totals, the connected folders and their disk space, what is
// downloading now, what was added and downloaded recently, what airs next,
// and anything that is misconfigured (with what stops working because of it).
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	var out dashboardPayload

	// The totals are shared between callers and kept until the library
	// changes, and the queries stop when the request gives up.
	overview, err := s.libraryOverview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	movieCounts, seriesCounts := overview.Movies, overview.Series
	out.Library.Movies.Total, out.Library.Movies.Downloaded = movieCounts.Total, movieCounts.Downloaded
	out.Library.Movies.Downloading, out.Library.Movies.Missing = movieCounts.Downloading, movieCounts.Missing
	moviesDownloaded := movieCounts.Downloaded
	out.Library.Series.Total = seriesCounts.Series
	out.Library.Series.Episodes = seriesCounts.Episodes
	out.Library.Series.EpisodesDownloaded = seriesCounts.EpisodesDownloaded
	out.Library.Series.EpisodesMissing = out.Library.Series.Episodes - out.Library.Series.EpisodesDownloaded
	out.Library.Wanted.Movies, out.Library.Wanted.Episodes = overview.WantedMovies, overview.WantedEpisodes

	counts, err := s.MusicRepo.Counts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Library.Music.Artists, out.Library.Music.Albums = counts.Artists, counts.Albums
	out.Library.Music.Downloaded, out.Library.Music.Missing = counts.Downloaded, counts.Missing

	ebooks, audiobooks, err := s.BookRepo.Counts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Library.Ebooks = dashBookCounts{Books: ebooks.Books, Downloaded: ebooks.Downloaded, Missing: ebooks.Missing}
	out.Library.Audiobooks = dashBookCounts{Books: audiobooks.Books, Downloaded: audiobooks.Downloaded, Missing: audiobooks.Missing}

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
	dlRoot := s.downloadsRoot()
	mkFolder := func(key, label, path string, bytes int64, items int, itemsLabel string) dashFolder {
		f := inspectCached(path)
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
	// Every media type has its folder here, the switched-off ones marked so
	// the page can dim them.
	offFolder := func(key, label, path string) dashFolder {
		return dashFolder{Key: key, Label: label, Path: path, Warnings: []string{}, Off: true}
	}
	folderFor := func(on bool, key, label, path string, bytes int64, items int, itemsLabel string) dashFolder {
		if !on {
			return offFolder(key, label, path)
		}
		return mkFolder(key, label, path, bytes, items, itemsLabel)
	}
	// A kind with more folders (Settings > Library > Folders) gets a card for
	// each: the main one keeps the totals less what the extra ones hold, and
	// the one new titles go to right now is marked.
	kindFolders := func(on bool, kind, label, moreLabel string, roots []string, totalBytes int64, totalItems int, itemsLabel string, bytesByRoot map[string]int64, itemsByRoot map[string]int, newFolder string) []dashFolder {
		main := ""
		if len(roots) > 0 {
			main = roots[0]
		}
		if !on {
			return []dashFolder{offFolder(kind, label, main)}
		}
		var extra []dashFolder
		for i, root := range roots[min(1, len(roots)):] {
			d := mkFolder(fmt.Sprintf("%s-extra-%d", kind, i+1), moreLabel, root, bytesByRoot[root], itemsByRoot[root], itemsLabel)
			d.NewTitles = newFolder == root
			totalBytes -= bytesByRoot[root]
			totalItems -= itemsByRoot[root]
			extra = append(extra, d)
		}
		first := mkFolder(kind, label, main, max(0, totalBytes), max(0, totalItems), itemsLabel)
		first.NewTitles = len(roots) > 1 && newFolder == ""
		return append([]dashFolder{first}, extra...)
	}
	out.Folders = kindFolders(s.moviesEnabled(), "movies", "Movies", "More movies", s.movieFolders(), stats.movieBytes, moviesDownloaded, "movies", stats.movieBytesByRoot, stats.moviesByRoot, s.newMovieFolder())
	out.Folders = append(out.Folders, kindFolders(s.tvEnabled(), "tv", "TV shows", "More TV shows", s.tvFolders(), stats.tvBytes, out.Library.Series.EpisodesDownloaded, "episodes", stats.tvBytesByRoot, stats.episodesByRoot, s.newShowFolder())...)
	out.Folders = append(out.Folders,
		folderFor(s.musicEnabled(), "music", "Music", s.musicRoot(), 0, out.Library.Music.Downloaded, "albums"),
		folderFor(s.ebooksEnabled(), "ebooks", "Ebooks", s.ebooksRoot(), 0, out.Library.Ebooks.Downloaded, "books"),
		folderFor(s.audiobooksEnabled(), "audiobooks", "Audiobooks", s.audiobooksRoot(), 0, out.Library.Audiobooks.Downloaded, "books"),
		mkFolder("downloads", "Downloads", dlRoot, 0, 0, "active"),
	)

	// Downloads in flight.
	out.Active = []dashActive{}
	queueItems, err := s.QueueRepo.ListUnfinished()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recentDone, recentErr := s.QueueRepo.RecentCompleted(8)
	doneItems := make([]queue.Item, len(recentDone))
	for i, c := range recentDone {
		doneItems[i] = c.Item
	}
	namer := s.newQueueNamer(queueItems, doneItems)
	for _, it := range queueItems {
		title, subtitle, poster := namer.describe(it)
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
	out.RecentlyAdded, out.RecentInLibrary, out.RecentToCome = []dashRecent{}, []dashRecent{}, []dashRecent{}
	if recent, err := s.MovieRepo.RecentlyAdded(recentScan); err == nil {
		var arrived []dashRecent
		for _, it := range recent {
			title, poster := s.shownFace(it.Kind, it.TMDBID, it.Title, it.PosterPath)
			d := dashRecent{Kind: it.Kind, ID: it.ID, Title: title, Year: it.Year, PosterURL: poster, At: it.AddedAt, State: it.State}
			if it.Kind == "movie" {
				d.TMDBID = it.TMDBID
			} else {
				d.SeriesID = it.ID
			}
			if len(out.RecentlyAdded) < recentShown {
				out.RecentlyAdded = append(out.RecentlyAdded, d)
			}
			switch {
			case it.State == "downloaded" || it.State == "partial":
				arrived = append(arrived, d)
			case len(out.RecentToCome) < recentShown:
				out.RecentToCome = append(out.RecentToCome, d) // newest added first
			}
		}
		out.RecentInLibrary = s.latestArrivals(arrived)
	}
	out.RecentDownloads = []dashRecent{}
	if recentErr == nil {
		for _, c := range recentDone {
			title, subtitle, poster := namer.describe(c.Item)
			d := dashRecent{Kind: "movie", ID: c.ID, Title: title, Subtitle: subtitle, PosterURL: poster, SizeBytes: c.SizeBytes, At: c.CompletedAt}
			if c.SeriesID > 0 {
				d.Kind, d.SeriesID = "series", c.SeriesID
			} else if c.AlbumID > 0 {
				d.Kind, d.AlbumID = "album", c.AlbumID
			} else if c.BookID > 0 {
				d.Kind, d.BookID = "book", c.BookID
			} else if m, ok := namer.movie(c.MovieID); ok {
				d.TMDBID, d.Year, d.Quality = m.TMDBID, m.Year, m.Quality
			}
			out.RecentDownloads = append(out.RecentDownloads, d)
		}
	}

	// Coming up.
	out.Upcoming = []calendarEntryPayload{}
	// One day back as well: an episode dated yesterday can air today, on
	// the viewer's clock, once its exact time is known.
	now := time.Now().UTC()
	if entries, err := s.calendarEntriesSince(now.AddDate(0, 0, -1).Format("2006-01-02")); err == nil {
		out.Upcoming = upcomingEntries(entries, now, 6)
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
	torrentSource := false
	if list, err := s.IndexerRepo.List(); err == nil {
		for _, inst := range list {
			if inst.Enabled {
				out.Setup.Indexers++
				if inst.Protocol == indexers.ProtocolTorrent {
					torrentSource = true
				}
			}
		}
	}
	if servers, err := s.ClientRepo.List(); err == nil {
		out.Setup.Servers = len(servers)
	}
	if s.usenetClientOn() {
		out.Setup.Servers = max(out.Setup.Servers, 1) // the downloader stands in for a provider
	}
	out.Setup.VPN = s.VPNManager.Status().Connected
	out.Setup.Torrents = torrentSource && s.torrentReleasesOn()
	if list, err := s.MediaServers.ListAll(); err == nil {
		out.Setup.MediaServers = len(list)
	}

	writeJSON(w, http.StatusOK, out)
}

// describeQueueItem names a queue entry after the movie or show it is for
// (falling back to the release title) and finds its poster. A list of entries
// uses a queueNamer instead, so the lookups are shared (queue_describe.go).
func (s *Server) describeQueueItem(it queue.Item) (title, subtitle, poster string) {
	return s.newQueueNamer([]queue.Item{it}).describe(it)
}
