package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/importlists"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/simkl"
	"github.com/rdborg/mediarium/internal/trakt"
)

// Import lists (Settings > Info, lists and subtitles > Import lists): lists on
// Trakt, TMDB, IMDb, MDBList, Simkl and Letterboxd that are read on a schedule, adding
// what is on them to the library with the profile, folder, monitoring and tags
// chosen for the list. Nothing is ever removed when a title leaves a list.
// This file is the list of lists (storage and the routes); import_lists_run.go
// reads the sources and does the adding.

// defaultListMaxPerRun is how many titles one read of a new list adds unless
// the person chooses another number.
const defaultListMaxPerRun = 10

// The sources a list can read.
const (
	listTraktList      = "trakt-list"
	listTraktWatchlist = "trakt-watchlist"
	listTraktChart     = "trakt-chart"
	listTMDBList       = "tmdb-list"
	listTMDBChart      = "tmdb-chart"
	listIMDB           = "imdb"
	listMDBList        = "mdblist"
	listLetterboxd     = "letterboxd"
	listSimkl          = "simkl"
	listRSS            = "rss"
)

// listSource describes a source for the page that adds lists.
type listSource struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Media says what the source holds: "both" (movies and shows) or "movie".
	Media string `json:"media"`
	Help  string `json:"help"`
}

func listSources() []listSource {
	return []listSource{
		{listTraktList, "Trakt list", "both", "A public list on trakt.tv. Paste its address."},
		{listTraktWatchlist, "Trakt watchlist", "both", "Someone's public watchlist on Trakt. Their profile has to be public."},
		{listTraktChart, "Trakt trending, popular or anticipated", "both", "What Trakt shows on its own charts."},
		{listTMDBList, "TMDB list", "both", "A public list on themoviedb.org. Paste its address or number."},
		{listTMDBChart, "TMDB trending, popular, top rated or upcoming", "both", "TMDB's own charts, narrowed by genre or language if you like."},
		{listIMDB, "IMDb list", "both", "An IMDb list or watchlist exported as a CSV file. IMDb doesn't let programs download lists, so you export the file in your browser and upload it here."},
		{listMDBList, "MDBList list", "both", "A public list on mdblist.com. Needs your own MDBList API key."},
		{listSimkl, "Simkl list", "both", "Your own Plan to Watch, Watching or Completed list on simkl.com, movies, shows and anime. Needs your Simkl account connected."},
		{listRSS, "RSS feed", "both", "A feed of releases or titles, such as an indexer's RSS feed or a site that posts new releases. Each entry's IMDb or TMDB id, or else its name, says which movie or show it is. It can also download better releases from the feed for titles you have."},
		{listLetterboxd, "Letterboxd list", "movie", "A Letterboxd list or watchlist exported as a CSV file. Letterboxd doesn't let programs read its pages, so you export the file in your browser and upload it here. Movies only."},
	}
}

func validListSource(id string) (listSource, bool) {
	for _, s := range listSources() {
		if s.ID == id {
			return s, true
		}
	}
	return listSource{}, false
}

// listConfig is what a source needs to know. Which fields count depends on
// the source.
type listConfig struct {
	URL      string `json:"url,omitempty"`      // the list's address (or a Trakt user name, a TMDB list number)
	Chart    string `json:"chart,omitempty"`    // charts: trending, popular, anticipated, top-rated or upcoming
	Count    int    `json:"count,omitempty"`    // charts: how many titles to look at
	Genre    int    `json:"genre,omitempty"`    // TMDB charts: a TMDB genre id
	Language string `json:"language,omitempty"` // TMDB charts: original language, like "ko"
	Csv      string `json:"csv,omitempty"`      // IMDb and Letterboxd: the exported file's text
	Status   string `json:"status,omitempty"`   // Simkl: which of the account's lists to read, like "plantowatch"
	Grab     bool   `json:"grab,omitempty"`     // RSS feeds: also download the feed's releases that are wanted
}

// importList is one list as it is stored and as the page sees it. Csv and the
// API key never leave the server: HasCsv and HasKey say they are set.
type importList struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Source        string     `json:"source"`
	Media         string     `json:"media"` // movie, tv or both
	Config        listConfig `json:"config"`
	HasCsv        bool       `json:"hasCsv"`
	HasKey        bool       `json:"hasKey"`
	Enabled       bool       `json:"enabled"`
	IntervalHours int        `json:"intervalHours"`
	ProfileID     int64      `json:"profileId"`
	MovieRoot     string     `json:"movieRoot"`
	TVRoot        string     `json:"tvRoot"`
	Monitored     bool       `json:"monitored"`
	SearchNow     bool       `json:"searchNow"`
	Tags          []string   `json:"tags"`
	MinRating     float64    `json:"minRating"`
	MinYear       int        `json:"minYear"`
	MaxPerRun     int        `json:"maxPerRun"`
	StartFresh    bool       `json:"startFresh"`
	CreatedAt     string     `json:"createdAt"`
	LastRunAt     string     `json:"lastRunAt,omitempty"`
	LastStatus    string     `json:"lastStatus,omitempty"` // ok or error
	LastMessage   string     `json:"lastMessage,omitempty"`
	LastFound     int        `json:"lastFound"`
	LastAdded     int        `json:"lastAdded"`
	LastSkipped   int        `json:"lastSkipped"`
	TotalAdded    int        `json:"totalAdded"`
	NextRunAt     string     `json:"nextRunAt,omitempty"`
	Running       bool       `json:"running"`

	secret    string // the API key, encrypted
	createdBy int64
}

// importListInput is the body of create and update. Key is the person's API
// key for the source (MDBList); left out or empty keeps the one saved.
type importListInput struct {
	Name          string     `json:"name"`
	Source        string     `json:"source"`
	Media         string     `json:"media"`
	Config        listConfig `json:"config"`
	Key           string     `json:"key"`
	Enabled       *bool      `json:"enabled"`
	IntervalHours int        `json:"intervalHours"`
	ProfileID     int64      `json:"profileId"`
	MovieRoot     string     `json:"movieRoot"`
	TVRoot        string     `json:"tvRoot"`
	Monitored     *bool      `json:"monitored"`
	SearchNow     *bool      `json:"searchNow"`
	Tags          []string   `json:"tags"`
	MinRating     float64    `json:"minRating"`
	MinYear       int        `json:"minYear"`
	MaxPerRun     int        `json:"maxPerRun"`
	StartFresh    bool       `json:"startFresh"`
}

// importListState is what the runs share: one list is read at a time (so a
// service sees one polite client, not several at once), and the page can ask
// which lists are being read.
type importListState struct {
	run     sync.Mutex
	mu      sync.Mutex
	running map[int64]bool

	// Tests set these: no pauses between requests, and MDBList's address.
	noPause     bool
	mdblistBase string
}

func (st *importListState) setRunning(id int64, on bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.running == nil {
		st.running = map[int64]bool{}
	}
	if on {
		st.running[id] = true
	} else {
		delete(st.running, id)
	}
}

func (st *importListState) isRunning(id int64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.running[id]
}

// claim marks a list as being read and says false when it already is.
func (st *importListState) claim(id int64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.running[id] {
		return false
	}
	if st.running == nil {
		st.running = map[int64]bool{}
	}
	st.running[id] = true
	return true
}

const (
	listColumns = `id, name, source, media, config, secret, enabled, interval_hours, profile_id, movie_root, tv_root, monitored, search_now, tags,
		min_rating, min_year, max_per_run, start_fresh, created_by, created_at, last_run_at, last_status, last_message, last_found, last_added, last_skipped, total_added`
	maxImportLists = 50
)

func scanImportList(sc interface{ Scan(...any) error }) (importList, error) {
	var l importList
	var cfg, tags string
	var enabled, monitored, search, fresh int
	err := sc.Scan(&l.ID, &l.Name, &l.Source, &l.Media, &cfg, &l.secret, &enabled, &l.IntervalHours, &l.ProfileID, &l.MovieRoot, &l.TVRoot, &monitored, &search, &tags,
		&l.MinRating, &l.MinYear, &l.MaxPerRun, &fresh, &l.createdBy, &l.CreatedAt, &l.LastRunAt, &l.LastStatus, &l.LastMessage, &l.LastFound, &l.LastAdded, &l.LastSkipped, &l.TotalAdded)
	if err != nil {
		return l, err
	}
	l.Enabled, l.Monitored, l.SearchNow, l.StartFresh = enabled != 0, monitored != 0, search != 0, fresh != 0
	_ = json.Unmarshal([]byte(cfg), &l.Config)
	_ = json.Unmarshal([]byte(tags), &l.Tags)
	if l.Tags == nil {
		l.Tags = []string{}
	}
	return l, nil
}

func (s *Server) getImportList(id int64) (importList, error) {
	l, err := scanImportList(s.db.QueryRow(`SELECT `+listColumns+` FROM import_lists WHERE id = ?`, id))
	if err != nil {
		return l, err
	}
	return l, nil
}

func (s *Server) allImportLists() ([]importList, error) {
	rows, err := s.db.Query(`SELECT ` + listColumns + ` FROM import_lists ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list import lists: %w", err)
	}
	defer rows.Close()
	out := []importList{}
	for rows.Next() {
		l, err := scanImportList(rows)
		if err != nil {
			return nil, fmt.Errorf("read import list: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// view is the list as the page gets it: no file text, no key, and when it
// runs next.
func (s *Server) listView(l importList) importList {
	l.HasCsv = l.Config.Csv != ""
	l.Config.Csv = ""
	l.HasKey = l.secret != ""
	l.Running = s.lists.isRunning(l.ID)
	if l.Enabled {
		l.NextRunAt = nextListRun(l, time.Now())
	}
	return l
}

// nextListRun is when a list is next read: its interval after the last read,
// or as soon as the checker looks if it was never read.
func nextListRun(l importList, now time.Time) string {
	if l.LastRunAt == "" {
		return now.UTC().Format(time.RFC3339)
	}
	last, err := time.Parse(time.RFC3339, l.LastRunAt)
	if err != nil {
		return now.UTC().Format(time.RFC3339)
	}
	return last.Add(time.Duration(l.IntervalHours) * time.Hour).UTC().Format(time.RFC3339)
}

// listIsDue reports whether a list should be read now.
func listIsDue(l importList, now time.Time) bool {
	if !l.Enabled {
		return false
	}
	next, err := time.Parse(time.RFC3339, nextListRun(l, now))
	return err == nil && !next.After(now)
}

var (
	tmdbListNumber  = regexp.MustCompile(`^\d{1,10}$`)
	tmdbListAddress = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?themoviedb\.org/(?:[a-z]{2}(?:-[a-z]{2})?/)?list/(\d{1,10})(?:[-/?#].*)?$`)
)

// parseTMDBListNumber reads a list number from "8504994" or the list's address.
func parseTMDBListNumber(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	digits := raw
	if !tmdbListNumber.MatchString(raw) {
		m := tmdbListAddress.FindStringSubmatch(raw)
		if m == nil {
			return 0, errors.New("That isn't a TMDB list. Paste its address (themoviedb.org/list/8504994) or just the number")
		}
		digits = m[1]
	}
	n, _ := strconv.Atoi(digits)
	if n <= 0 {
		return 0, errors.New("That isn't a TMDB list number")
	}
	return n, nil
}

var tmdbCharts = map[string]bool{"trending": true, "popular": true, "top-rated": true, "upcoming": true}

// checkImportListInput turns a request body into a list, or says what is
// wrong in plain words. cur is the saved list when editing, else nil.
func (s *Server) checkImportListInput(in importListInput, cur *importList) (importList, string) {
	var l importList
	if cur != nil {
		l = *cur
	}
	src, ok := validListSource(in.Source)
	if !ok {
		return l, "Choose where the list comes from."
	}
	l.Source = src.ID
	l.Name = strings.TrimSpace(in.Name)
	if l.Name == "" {
		l.Name = src.Label
	}
	if len([]rune(l.Name)) > 80 {
		return l, "Give the list a shorter name (80 letters at most)."
	}
	l.Media = in.Media
	if src.Media == "movie" {
		l.Media = "movie"
	}
	if l.Media != "movie" && l.Media != "tv" && l.Media != "both" {
		return l, "Choose whether to add movies, shows or both."
	}
	if in.IntervalHours < 1 || in.IntervalHours > 168 {
		return l, "Choose how often to read the list: from every hour to once a week."
	}
	l.IntervalHours = in.IntervalHours
	l.MaxPerRun = in.MaxPerRun
	if l.MaxPerRun == 0 {
		l.MaxPerRun = defaultListMaxPerRun
	}
	if l.MaxPerRun < 1 || l.MaxPerRun > 100 {
		return l, "Choose how many titles to add at a time: between 1 and 100."
	}
	if in.MinRating < 0 || in.MinRating > 10 {
		return l, "The minimum rating is a number from 0 to 10."
	}
	if in.MinYear != 0 && (in.MinYear < 1870 || in.MinYear > 2200) {
		return l, "The minimum year isn't a year."
	}
	l.MinRating, l.MinYear, l.StartFresh = in.MinRating, in.MinYear, in.StartFresh
	l.Enabled = in.Enabled == nil || *in.Enabled
	l.Monitored = in.Monitored == nil || *in.Monitored
	l.SearchNow = in.SearchNow == nil || *in.SearchNow
	if err := s.checkProfileChoice(in.ProfileID); err != nil {
		return l, "Choose a quality profile that exists."
	}
	l.ProfileID = in.ProfileID
	var problem string
	if l.MovieRoot, problem = s.checkFolderSetting(in.MovieRoot, s.movieFolders(), "movie"); problem != "" {
		return l, problem
	}
	if l.TVRoot, problem = s.checkFolderSetting(in.TVRoot, s.tvFolders(), "show"); problem != "" {
		return l, problem
	}
	l.Tags = library.NormalizeTags(in.Tags)
	if l.Tags == nil {
		l.Tags = []string{}
	}

	cfg := in.Config
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.Chart = strings.TrimSpace(cfg.Chart)
	cfg.Language = strings.ToLower(strings.TrimSpace(cfg.Language))
	if cfg.Csv == "" && cur != nil && cur.Source == l.Source {
		cfg.Csv = cur.Config.Csv // an edit that leaves the file alone keeps it
	}
	if len(cfg.Csv) > importlists.MaxCSVBytes {
		return l, "That file is too big to be a list."
	}
	key := strings.TrimSpace(in.Key)
	if key != "" {
		enc, err := s.box.Encrypt(key)
		if err != nil {
			return l, "The key couldn't be saved."
		}
		l.secret = enc
	} else if cur == nil || cur.Source != l.Source {
		l.secret = ""
	}
	switch l.Source {
	case listTraktList:
		if _, _, err := trakt.ParseAnyListAddress(cfg.URL); err != nil {
			return l, err.Error() + "."
		}
		cfg = listConfig{URL: cfg.URL}
	case listTraktWatchlist:
		if _, err := trakt.ParseWatchlistAddress(cfg.URL); err != nil {
			return l, err.Error() + "."
		}
		cfg = listConfig{URL: cfg.URL}
	case listTraktChart:
		if !trakt.ValidChart(cfg.Chart) {
			return l, "Choose a Trakt chart: trending, popular or anticipated."
		}
		if cfg.Count == 0 {
			cfg.Count = 50
		}
		if cfg.Count < 1 || cfg.Count > 100 {
			return l, "Trakt charts can be read 100 titles deep at most."
		}
		cfg = listConfig{Chart: cfg.Chart, Count: cfg.Count}
	case listTMDBList:
		if _, err := parseTMDBListNumber(cfg.URL); err != nil {
			return l, err.Error() + "."
		}
		cfg = listConfig{URL: cfg.URL}
	case listTMDBChart:
		if !tmdbCharts[cfg.Chart] {
			return l, "Choose a TMDB chart: trending, popular, top rated or upcoming."
		}
		if cfg.Count == 0 {
			cfg.Count = 40
		}
		if cfg.Count < 1 || cfg.Count > 200 {
			return l, "A TMDB chart can be read 200 titles deep at most."
		}
		if (cfg.Genre != 0 || cfg.Language != "") && (cfg.Chart == "trending" || cfg.Chart == "upcoming") {
			return l, "Trending and upcoming can't be narrowed by genre or language. Use popular or top rated for that."
		}
		if cfg.Genre < 0 || len(cfg.Language) > 3 {
			return l, "That genre or language isn't one TMDB uses."
		}
		cfg = listConfig{Chart: cfg.Chart, Count: cfg.Count, Genre: cfg.Genre, Language: cfg.Language}
	case listIMDB, listLetterboxd:
		if cfg.Csv == "" && cfg.URL == "" {
			return l, "Upload the exported CSV file, or give an address that serves it."
		}
		if cfg.URL != "" {
			if _, err := importlists.CSVAddress(cfg.URL); err != nil {
				return l, err.Error() + "."
			}
		}
		if cfg.Csv != "" {
			if _, err := importlists.ParseCSV(cfg.Csv); err != nil {
				return l, err.Error() + "."
			}
		}
		cfg = listConfig{URL: cfg.URL, Csv: cfg.Csv}
	case listRSS:
		if _, err := importlists.FeedAddress(cfg.URL); err != nil {
			return l, err.Error() + "."
		}
		cfg = listConfig{URL: cfg.URL, Grab: cfg.Grab}
	case listSimkl:
		cfg.Status = strings.TrimSpace(cfg.Status)
		if cfg.Status == "" {
			cfg.Status = simkl.StatusPlanToWatch
		}
		if !simkl.ValidStatus(cfg.Status) {
			return l, "Choose which Simkl list to read: Plan to watch, Watching, Completed, On hold or Dropped."
		}
		cfg = listConfig{Status: cfg.Status}
	case listMDBList:
		if _, _, err := importlists.ParseMDBListAddress(cfg.URL); err != nil {
			return l, err.Error() + "."
		}
		if l.secret == "" {
			return l, "Add your MDBList API key. You make one at mdblist.com/preferences."
		}
		cfg = listConfig{URL: cfg.URL}
	}
	l.Config = cfg
	return l, ""
}

func (s *Server) saveImportList(l importList, create bool, by int64) (int64, error) {
	cfg, _ := json.Marshal(l.Config)
	tags, _ := json.Marshal(l.Tags)
	if create {
		res, err := s.db.Exec(`INSERT INTO import_lists (name, source, media, config, secret, enabled, interval_hours, profile_id, movie_root, tv_root, monitored, search_now, tags,
			min_rating, min_year, max_per_run, start_fresh, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.Name, l.Source, l.Media, string(cfg), l.secret, b2i(l.Enabled), l.IntervalHours, l.ProfileID, l.MovieRoot, l.TVRoot, b2i(l.Monitored), b2i(l.SearchNow), string(tags),
			l.MinRating, l.MinYear, l.MaxPerRun, b2i(l.StartFresh), by)
		if err != nil {
			return 0, fmt.Errorf("save import list: %w", err)
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec(`UPDATE import_lists SET name = ?, source = ?, media = ?, config = ?, secret = ?, enabled = ?, interval_hours = ?, profile_id = ?, movie_root = ?, tv_root = ?,
		monitored = ?, search_now = ?, tags = ?, min_rating = ?, min_year = ?, max_per_run = ?, start_fresh = ? WHERE id = ?`,
		l.Name, l.Source, l.Media, string(cfg), l.secret, b2i(l.Enabled), l.IntervalHours, l.ProfileID, l.MovieRoot, l.TVRoot, b2i(l.Monitored), b2i(l.SearchNow), string(tags),
		l.MinRating, l.MinYear, l.MaxPerRun, b2i(l.StartFresh), l.ID)
	if err != nil {
		return 0, fmt.Errorf("save import list: %w", err)
	}
	return l.ID, nil
}

// plexWatchlistView is a connected Plex watchlist shown in the same list: each
// account that connected one has it checked by the watchlist job.
type plexWatchlistView struct {
	UserID    int64  `json:"userId"`
	Account   string `json:"account"`
	PlexName  string `json:"plexName,omitempty"`
	AutoAdd   bool   `json:"autoAdd"`
	CheckedAt string `json:"checkedAt,omitempty"`
	LastError string `json:"lastError,omitempty"`
	Added     int    `json:"added"`
	Requested int    `json:"requested"`
}

func (s *Server) plexWatchlistViews() []plexWatchlistView {
	out := []plexWatchlistView{}
	rows, err := s.db.Query(`SELECT user_id, plex_name, auto_add, checked_at, last_error FROM plex_watchlists ORDER BY user_id`)
	if err != nil {
		return out
	}
	var found []plexWatchlistView
	for rows.Next() {
		var v plexWatchlistView
		var auto int
		if rows.Scan(&v.UserID, &v.PlexName, &auto, &v.CheckedAt, &v.LastError) == nil {
			v.AutoAdd = auto != 0
			found = append(found, v)
		}
	}
	rows.Close()
	for _, v := range found {
		if u, err := s.Auth.GetUser(v.UserID); err == nil {
			v.Account = u.DisplayName()
		}
		_ = s.db.QueryRow(`SELECT COUNT(*) FILTER (WHERE outcome = 'added'), COUNT(*) FILTER (WHERE outcome = 'requested') FROM plex_watchlist_items WHERE user_id = ?`, v.UserID).Scan(&v.Added, &v.Requested)
		out = append(out, v)
	}
	return out
}

// GET /api/import-lists
func (s *Server) handleListImportLists(w http.ResponseWriter, r *http.Request) {
	lists, err := s.allImportLists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range lists {
		lists[i] = s.listView(lists[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lists":          lists,
		"sources":        listSources(),
		"plexWatchlists": s.plexWatchlistViews(),
	})
}

func importListIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a list.")
		return 0, false
	}
	return id, true
}

func (s *Server) importListFromPath(w http.ResponseWriter, r *http.Request) (importList, bool) {
	id, ok := importListIDFromPath(w, r)
	if !ok {
		return importList{}, false
	}
	l, err := s.getImportList(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That list doesn't exist any more.")
		return l, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return l, false
	}
	return l, true
}

// POST /api/import-lists
func (s *Server) handleCreateImportList(w http.ResponseWriter, r *http.Request) {
	var in importListInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM import_lists`).Scan(&n)
	if n >= maxImportLists {
		writeError(w, http.StatusConflict, "That is as many lists as Mediarium keeps. Remove one you don't use.")
		return
	}
	l, problem := s.checkImportListInput(in, nil)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	by, _ := requester(r)
	id, err := s.saveImportList(l, true, by)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("import list added", "id", id, "source", l.Source)
	if l.Enabled {
		s.startListRun(id) // read it now, so the page shows what it found
	}
	saved, _ := s.getImportList(id)
	writeJSON(w, http.StatusCreated, s.listView(saved))
}

// PUT /api/import-lists/{id}
func (s *Server) handleUpdateImportList(w http.ResponseWriter, r *http.Request) {
	cur, ok := s.importListFromPath(w, r)
	if !ok {
		return
	}
	var in importListInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	l, problem := s.checkImportListInput(in, &cur)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	l.ID = cur.ID
	if _, err := s.saveImportList(l, false, 0); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, _ := s.getImportList(cur.ID)
	writeJSON(w, http.StatusOK, s.listView(saved))
}

// PUT /api/import-lists/{id}/enabled {"enabled": true}: the switch on the list.
func (s *Server) handleSetImportListEnabled(w http.ResponseWriter, r *http.Request) {
	l, ok := s.importListFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Say whether the list should be on.")
		return
	}
	if _, err := s.db.Exec(`UPDATE import_lists SET enabled = ? WHERE id = ?`, b2i(*body.Enabled), l.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, _ := s.getImportList(l.ID)
	writeJSON(w, http.StatusOK, s.listView(saved))
}

// DELETE /api/import-lists/{id}: forgets the list. Titles it added stay.
func (s *Server) handleDeleteImportList(w http.ResponseWriter, r *http.Request) {
	l, ok := s.importListFromPath(w, r)
	if !ok {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM import_list_items WHERE list_id = ?`, l.ID)
	if _, err := s.db.Exec(`DELETE FROM import_lists WHERE id = ?`, l.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/import-lists/{id}/run: read the list now. It answers at once;
// the page watches "running" on the list.
func (s *Server) handleRunImportList(w http.ResponseWriter, r *http.Request) {
	l, ok := s.importListFromPath(w, r)
	if !ok {
		return
	}
	if !s.startListRun(l.ID) {
		writeError(w, http.StatusConflict, "That list is being read right now.")
		return
	}
	saved, _ := s.getImportList(l.ID)
	writeJSON(w, http.StatusAccepted, s.listView(saved))
}

// POST /api/import-lists/{id}/forget: the list starts over, treating every
// title on it as new.
func (s *Server) handleForgetImportList(w http.ResponseWriter, r *http.Request) {
	l, ok := s.importListFromPath(w, r)
	if !ok {
		return
	}
	if s.lists.isRunning(l.ID) {
		writeError(w, http.StatusConflict, "That list is being read right now. Try again in a moment.")
		return
	}
	if _, err := s.db.Exec(`DELETE FROM import_list_items WHERE list_id = ?`, l.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.db.Exec(`UPDATE import_lists SET last_run_at = '', last_status = '', last_message = '', last_found = 0, last_added = 0, last_skipped = 0 WHERE id = ?`, l.ID)
	saved, _ := s.getImportList(l.ID)
	writeJSON(w, http.StatusOK, s.listView(saved))
}

// POST /api/import-lists/preview: read a list as typed, without saving it or
// adding anything, and say what is on it. In the body, id (optional) names a
// saved list whose file or key is used when the body leaves them out.
func (s *Server) handlePreviewImportList(w http.ResponseWriter, r *http.Request) {
	var body struct {
		importListInput
		ID int64 `json:"id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	var cur *importList
	if body.ID > 0 {
		if c, err := s.getImportList(body.ID); err == nil {
			cur = &c
		}
	}
	in := body.importListInput
	if in.IntervalHours == 0 {
		in.IntervalHours = 24
	}
	l, problem := s.checkImportListInput(in, cur)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	items, err := s.fetchListItems(ctx, l)
	if err != nil {
		writeError(w, http.StatusBadGateway, listErrorText(err))
		return
	}
	items = dedupeListItems(items)
	counts := map[string]int{"movie": 0, "tv": 0, "unknown": 0}
	sample := []string{}
	for _, it := range items {
		switch it.Kind {
		case "movie", "tv":
			counts[it.Kind]++
		default:
			counts["unknown"]++
		}
		if len(sample) < 8 && it.Title != "" {
			name := it.Title
			if it.Year > 0 {
				name += " (" + strconv.Itoa(it.Year) + ")"
			}
			sample = append(sample, name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": len(items), "movies": counts["movie"], "shows": counts["tv"], "unknown": counts["unknown"], "sample": sample})
}

// POST /api/import-lists/plex/{userId}/run: check one account's Plex
// watchlist now.
func (s *Server) handleRunPlexWatchlist(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil || uid <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't an account.")
		return
	}
	row, ok, err := s.watchlistRow(uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "That account hasn't connected a Plex watchlist.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	added, requested, err := s.syncWatchlist(ctx, row)
	if err != nil {
		writeError(w, http.StatusBadGateway, listErrorText(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "requested": requested})
}

// startListRun reads a list in the background. It says false when that list
// is already being read.
func (s *Server) startListRun(id int64) bool {
	if !s.lists.claim(id) {
		return false
	}
	s.background(func() {
		defer s.lists.setRunning(id, false)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		s.runImportList(ctx, id)
	})
	return true
}

// importListsJob reads every list that is due. It is cheap while no list exists.
func (s *Server) importListsJob(ctx context.Context) {
	lists, err := s.allImportLists()
	if err != nil || len(lists) == 0 {
		return
	}
	now := time.Now()
	for _, l := range lists {
		if ctx.Err() != nil {
			return
		}
		if !listIsDue(l, now) || !s.lists.claim(l.ID) {
			continue
		}
		func() {
			defer s.lists.setRunning(l.ID, false)
			s.runImportList(ctx, l.ID)
		}()
	}
}

// listsCheckInterval is how often the job looks for lists that are due.
const listsCheckInterval = 10 * time.Minute
