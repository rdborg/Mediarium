package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/monitor"
	"github.com/rdborg/mediarium/internal/queue"
)

// metricsSlowTTL is how long the numbers that take a walk through the whole
// library to count (wanted episodes, health checks) are reused, so a scrape
// every few seconds does not repeat that walk.
const metricsSlowTTL = 30 * time.Second

// metricsSlow holds the slow-to-count numbers between scrapes.
type metricsSlow struct {
	at            time.Time
	wantedMovies  int
	wantedEpisode int
	wantedOK      bool
	healthByLevel map[string]int
}

// metricsCache is the saved metricsSlow and the lock that guards it.
type metricsCache struct {
	mu   sync.Mutex
	last metricsSlow
}

// handleMetrics gives library, queue, disk and health numbers in the
// Prometheus text format (version 0.0.4), for people who graph their server
// with Grafana. It answers at /api/metrics and /metrics, and is only for
// administrators: a session, an X-API-Key header, or an
// "Authorization: Bearer <api key>" header (see metricsBearer) that Prometheus'
// scrape config can send.
//
// Written by hand with plain text rather than pulling in the Prometheus client
// library: the exposition format is simple and a dependency would outweigh the
// few lines it saves.
//
// The first three metric names (mediarium_up, mediarium_library_movies_total,
// mediarium_queue_items_total) are the original ones and stay as they were.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	movieCounts, err := s.MovieRepo.MovieCounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queueByStatus, err := s.QueueRepo.CountByStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	moviesByStatus := map[library.Status]int{
		library.StatusMissing:     movieCounts.Missing,
		library.StatusDownloading: movieCounts.Downloading,
		library.StatusDownloaded:  movieCounts.Downloaded,
	}

	var m metricsWriter
	m.gauge("mediarium_up", "Whether the Mediarium API is up.")
	m.sample("mediarium_up", nil, 1)

	m.gauge("mediarium_build_info", "The version of Mediarium that is running. The value is always 1.")
	m.sample("mediarium_build_info", labels{"version", s.version}, 1)
	m.gauge("mediarium_start_time_seconds", "When Mediarium started, in seconds since the Unix epoch.")
	m.sample("mediarium_start_time_seconds", nil, float64(processStart.Unix()))

	m.gauge("mediarium_library_movies_total", "Movies in the library by status.")
	for _, status := range []library.Status{library.StatusMissing, library.StatusDownloading, library.StatusDownloaded} {
		m.sample("mediarium_library_movies_total", labels{"status", string(status)}, float64(moviesByStatus[status]))
	}

	m.gauge("mediarium_queue_items_total", "Download queue items by status.")
	for _, status := range []queue.Status{queue.StatusQueued, queue.StatusDownloading, queue.StatusImporting, queue.StatusPaused, queue.StatusStopped, queue.StatusCompleted, queue.StatusFailed, queue.StatusConflict} {
		m.sample("mediarium_queue_items_total", labels{"status", string(status)}, float64(queueByStatus[status]))
	}
	m.gauge("mediarium_downloads_active", "Downloads that are downloading right now.")
	m.sample("mediarium_downloads_active", nil, float64(queueByStatus[queue.StatusDownloading]))

	// Shows and episodes.
	if sc, err := s.MovieRepo.SeriesCounts(r.Context()); err == nil {
		m.gauge("mediarium_library_series", "Shows in the library.")
		m.sample("mediarium_library_series", nil, float64(sc.Series))
		m.gauge("mediarium_library_episodes", "Episodes in the library by status.")
		m.sample("mediarium_library_episodes", labels{"status", "downloaded"}, float64(sc.EpisodesDownloaded))
		m.sample("mediarium_library_episodes", labels{"status", "missing"}, float64(sc.Episodes-sc.EpisodesDownloaded))
	} else {
		slog.Warn("metrics: list series", "err", err)
	}

	// Music.
	if counts, err := s.MusicRepo.Counts(); err == nil {
		m.gauge("mediarium_library_music_artists", "Artists in the music library.")
		m.sample("mediarium_library_music_artists", nil, float64(counts.Artists))
		m.gauge("mediarium_library_music_albums", "Albums in the music library by status.")
		m.sample("mediarium_library_music_albums", labels{"status", "downloaded"}, float64(counts.Downloaded))
		m.sample("mediarium_library_music_albums", labels{"status", "missing"}, float64(counts.Missing))
	} else {
		slog.Warn("metrics: count music", "err", err)
	}

	// Books, counted by the format that is wanted.
	var ebookMissing, audioMissing int
	if ebook, audio, err := s.BookRepo.Counts(); err == nil {
		ebookMissing, audioMissing = ebook.Missing, audio.Missing
		m.gauge("mediarium_library_books", "Books in the library by format and status.")
		for _, row := range []struct {
			format string
			c      books.FormatCounts
		}{{"ebook", ebook}, {"audiobook", audio}} {
			m.sample("mediarium_library_books", labels{"format", row.format, "status", books.StatusDownloaded}, float64(row.c.Downloaded))
			m.sample("mediarium_library_books", labels{"format", row.format, "status", books.StatusMissing}, float64(row.c.Missing))
		}
	} else {
		slog.Warn("metrics: list books", "err", err)
	}

	// What is on the Wanted page, and how much room the library takes.
	slow := s.metricsSlowNumbers()
	if slow.wantedOK {
		m.gauge("mediarium_wanted_missing", "Titles on the Wanted page that are monitored and have no file yet, by kind.")
		m.sample("mediarium_wanted_missing", labels{"kind", "movie"}, float64(slow.wantedMovies))
		m.sample("mediarium_wanted_missing", labels{"kind", "episode"}, float64(slow.wantedEpisode))
		if counts, err := s.MusicRepo.Counts(); err == nil {
			m.sample("mediarium_wanted_missing", labels{"kind", "album"}, float64(counts.Missing))
		}
		m.sample("mediarium_wanted_missing", labels{"kind", "ebook"}, float64(ebookMissing))
		m.sample("mediarium_wanted_missing", labels{"kind", "audiobook"}, float64(audioMissing))
	}
	if stats, err := s.libraryStats(); err == nil {
		m.gauge("mediarium_library_size_bytes", "Size of the files in the library, by kind.")
		m.sample("mediarium_library_size_bytes", labels{"kind", "movies"}, float64(stats.movieBytes))
		m.sample("mediarium_library_size_bytes", labels{"kind", "tv"}, float64(stats.tvBytes))
	}

	// Health warnings, by level.
	m.gauge("mediarium_health_warnings", "Problems listed on the dashboard, by level (error, warn or info).")
	for _, level := range []string{"error", "warn", "info"} {
		m.sample("mediarium_health_warnings", labels{"level", level}, float64(slow.healthByLevel[level]))
	}

	// Free and total space of every folder.
	m.gauge("mediarium_disk_free_bytes", "Free space on the drive a folder is on.")
	m.gauge("mediarium_disk_total_bytes", "Total size of the drive a folder is on.")
	folders := s.metricsFolders()
	for _, f := range folders {
		info := inspectCached(f.path)
		if !info.Exists {
			continue
		}
		m.sample("mediarium_disk_free_bytes", labels{"kind", f.kind, "path", f.path}, float64(info.FreeBytes))
		m.sample("mediarium_disk_total_bytes", labels{"kind", f.kind, "path", f.path}, float64(info.TotalBytes))
	}

	// Indexers and Usenet servers, from the connection watch.
	if s.Monitor != nil {
		m.gauge("mediarium_indexer_up", "Whether the last check of an indexer worked (1) or failed (0).")
		m.gauge("mediarium_usenet_server_up", "Whether the last check of a Usenet server login worked (1) or failed (0).")
		for _, st := range s.Monitor.Statuses() {
			up := 0.0
			if st.OK {
				up = 1
			}
			switch st.Kind {
			case monitor.KindIndexer:
				m.sample("mediarium_indexer_up", labels{"indexer", st.Name}, up)
			case monitor.KindUsenet:
				m.sample("mediarium_usenet_server_up", labels{"server", st.Name}, up)
			}
		}
	}
	if list, err := s.IndexerRepo.List(); err == nil {
		enabled := 0
		for _, inst := range list {
			if inst.Enabled {
				enabled++
			}
		}
		m.gauge("mediarium_indexers", "Indexers set up, by state.")
		m.sample("mediarium_indexers", labels{"state", "enabled"}, float64(enabled))
		m.sample("mediarium_indexers", labels{"state", "disabled"}, float64(len(list)-enabled))
	}

	// Times a shared service said "too many requests" in the last 24 hours.
	if s.Usage != nil {
		m.gauge("mediarium_service_limit_hits", "Times an outside service refused a request because of a limit, in the last 24 hours.")
		for _, svc := range []string{serviceTMDB, serviceTrakt, serviceSimkl, serviceOpenSubtitles} {
			m.sample("mediarium_service_limit_hits", labels{"service", svc}, float64(s.Usage.Stats(svc).LimitHits))
		}
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(m.String()))
}

// metricsSlowNumbers returns the wanted counts and health levels, recounted at
// most every metricsSlowTTL.
func (s *Server) metricsSlowNumbers() metricsSlow {
	cache := &s.metricsCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	c := &cache.last
	if !c.at.IsZero() && time.Since(c.at) < metricsSlowTTL {
		return *c
	}
	c.wantedMovies, c.wantedEpisode, c.wantedOK = 0, 0, false
	if o, err := s.libraryOverview(context.Background()); err == nil {
		c.wantedMovies, c.wantedEpisode, c.wantedOK = o.WantedMovies, o.WantedEpisodes, true
	} else {
		slog.Warn("metrics: count wanted", "err", err)
	}
	c.healthByLevel = map[string]int{}
	for _, it := range s.collectHealth() {
		c.healthByLevel[it.Level]++
	}
	c.at = time.Now()
	return *c
}

type metricsFolder struct{ kind, path string }

// metricsFolders lists every library and download folder once, with the kind
// it holds. Switched-off media types are left out.
func (s *Server) metricsFolders() []metricsFolder {
	var out []metricsFolder
	seen := map[string]bool{}
	add := func(kind, path string) {
		if path == "" || seen[kind+"\x00"+path] {
			return
		}
		seen[kind+"\x00"+path] = true
		out = append(out, metricsFolder{kind, path})
	}
	if s.moviesEnabled() {
		for _, p := range s.movieFolders() {
			add("movies", p)
		}
	}
	if s.tvEnabled() {
		for _, p := range s.tvFolders() {
			add("tv", p)
		}
	}
	if s.musicEnabled() {
		add("music", s.musicRoot())
	}
	if s.ebooksEnabled() {
		add("ebooks", s.ebooksRoot())
	}
	if s.audiobooksEnabled() {
		add("audiobooks", s.audiobooksRoot())
	}
	if s.otherEnabled() {
		add("other", s.otherRoot())
	}
	add("downloads", s.downloadsRoot())
	return out
}

// metricsBearer lets a Prometheus scrape config authenticate with
// "Authorization: Bearer <api key>" (its `authorization` block), by handing
// the key on as X-API-Key, which is what the sign-in already understands. It
// only does that for the metrics address it wraps, and only when no
// X-API-Key came with the request.
func metricsBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" {
			if auth := r.Header.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
				if key := strings.TrimSpace(auth[7:]); key != "" {
					r2 := r.Clone(r.Context())
					r2.Header.Set("X-API-Key", key)
					r = r2
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// metricsBearerOnly applies metricsBearer to /api/metrics and leaves every
// other address alone: a bearer header is not an accepted way to sign in
// anywhere else.
func metricsBearerOnly(next http.Handler) http.Handler {
	withBearer := metricsBearer(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/metrics" {
			withBearer.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// metricsAlias serves /metrics with the /api/metrics handler, so a scrape
// config can use Prometheus' default path.
func metricsAlias(api http.Handler) http.Handler {
	return metricsBearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2 := r.Clone(r.Context())
		u := *r.URL
		u.Path, u.RawPath = "/api/metrics", ""
		r2.URL = &u
		api.ServeHTTP(w, r2)
	}))
}

// labels is a flat list of name, value, name, value.
type labels []string

// metricsWriter builds the text exposition format.
type metricsWriter struct{ b strings.Builder }

func (m *metricsWriter) gauge(name, help string) {
	fmt.Fprintf(&m.b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
}

func (m *metricsWriter) sample(name string, l labels, v float64) {
	m.b.WriteString(name)
	if len(l) > 0 {
		m.b.WriteByte('{')
		for i := 0; i+1 < len(l); i += 2 {
			if i > 0 {
				m.b.WriteByte(',')
			}
			fmt.Fprintf(&m.b, `%s="%s"`, l[i], escapeLabel(l[i+1]))
		}
		m.b.WriteByte('}')
	}
	fmt.Fprintf(&m.b, " %s\n", formatSample(v))
}

func (m *metricsWriter) String() string { return m.b.String() }

// escapeLabel escapes a label value as the exposition format asks: backslash,
// double quote and line feed.
func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

func formatSample(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}
