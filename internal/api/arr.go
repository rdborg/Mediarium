package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// The Radarr, Sonarr and Lidarr-style API: the endpoints apps built for those
// (Overseerr and Jellyseerr, dashboards such as Homepage and Homarr, phone
// apps such as LunaSea, nzb360 and Ruddarr) call, answered from Mediarium's
// library. Each app gets its own address prefix, since Radarr and Sonarr use
// the same paths: <mediarium>/radarr, /sonarr and /lidarr, which the other
// app takes as its "URL base". It signs in with an administrator's
// Mediarium API key, sent as X-Api-Key, a Bearer token or ?apikey= like the real apps take it.
//
// Only what those apps use is answered, in the shapes they read; fields that
// have no meaning here (Radarr's minimum availability, Sonarr's language
// profiles, root folders) are answered with harmless values and ignored when
// sent.

type arrApp string

const (
	arrRadarr arrApp = "radarr"
	arrSonarr arrApp = "sonarr"
	arrLidarr arrApp = "lidarr"
)

// The versions reported to the apps. Some check them: Jellyseerr skips
// Sonarr's language profiles from version 4, and Ruddarr has a separate code
// path for Sonarr 3.
var arrVersions = map[arrApp]string{
	arrRadarr: "5.14.0.9383",
	arrSonarr: "4.0.11.2680",
	arrLidarr: "2.8.2.4493",
}

func (a arrApp) name() string {
	switch a {
	case arrRadarr:
		return "Radarr"
	case arrSonarr:
		return "Sonarr"
	}
	return "Lidarr"
}

func (a arrApp) apiPrefix() string {
	if a == arrLidarr {
		return "/api/v1"
	}
	return "/api/v3"
}

// arrRoutes mounts the three prefixes on root.
func (s *Server) arrRoutes(root *http.ServeMux) {
	for _, app := range []arrApp{arrRadarr, arrSonarr, arrLidarr} {
		root.Handle("/"+string(app)+"/", s.busyGuard(noStore(s.arrHandler(app))))
	}
}

// arrHandler answers one app's API: it signs the caller in, lower-cases the
// path (Overseerr asks for /qualityProfile), and routes it.
func (s *Server) arrHandler(app arrApp) http.Handler {
	mux := http.NewServeMux()
	v := app.apiPrefix()
	h := func(method, path string, fn http.HandlerFunc) { mux.HandleFunc(method+" "+v+path, fn) }

	h("GET", "/system/status", s.arrSystemStatus(app))
	h("GET", "/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	h("GET", "/tag", s.arrListTags)
	h("GET", "/tag/{id}", s.arrGetTag)
	h("POST", "/tag", s.arrCreateTag)
	h("PUT", "/tag/{id}", s.arrRenameTag)
	h("GET", "/rootfolder", s.arrRootFolders(app))
	h("GET", "/diskspace", s.arrDiskSpace)
	h("GET", "/qualityprofile", s.arrQualityProfiles(app))
	h("GET", "/qualityprofile/{id}", s.arrQualityProfile(app))
	h("GET", "/qualitydefinition", s.arrQualityDefinitions(app))
	if app != arrLidarr {
		// What Recyclarr writes: the TRaSH Guides profiles, formats and sizes.
		h("GET", "/qualityprofile/schema", s.arrProfileSchema(app))
		h("POST", "/qualityprofile", s.arrSaveProfile(app, true))
		h("PUT", "/qualityprofile/{id}", s.arrSaveProfile(app, false))
		h("DELETE", "/qualityprofile/{id}", s.arrDeleteProfile)
		h("PUT", "/qualitydefinition/update", s.arrUpdateQualityDefinitions(app))
		h("PUT", "/qualitydefinition/{id}", s.arrUpdateQualityDefinition(app))
		h("GET", "/customformat/schema", arrCustomFormatSchema(app))
		h("GET", "/customformat/{id}", s.arrGetCustomFormat(app))
		h("POST", "/customformat", s.arrCreateCustomFormat(app))
		h("PUT", "/customformat/{id}", s.arrUpdateCustomFormat(app))
		h("DELETE", "/customformat/{id}", s.arrDeleteCustomFormat(app))
	}
	h("GET", "/language", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []arrLanguage{arrEnglish}) })
	h("GET", "/languageprofile", arrLanguageProfiles)
	h("GET", "/metadataprofile", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{{"id": 1, "name": "Standard"}})
	})
	h("POST", "/command", s.arrCommand(app))
	h("GET", "/command", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	h("GET", "/command/{id}", arrCommandStatus)
	h("GET", "/queue", s.arrQueue(app))
	h("GET", "/queue/status", s.arrQueueStatus(app))
	h("GET", "/queue/details", s.arrQueueDetails(app))
	h("DELETE", "/queue/{id}", s.arrDeleteQueueItem(app))
	h("DELETE", "/queue/bulk", s.arrBulkDeleteQueue)
	h("POST", "/queue/grab/{id}", s.arrGrabQueueItem)
	h("GET", "/filesystem", s.arrFilesystem)
	h("GET", "/filesystem/type", s.arrFilesystemType)
	h("GET", "/filesystem/mediafiles", s.arrFilesystemMediaFiles)
	h("GET", "/log/file", arrEmptyList)
	h("GET", "/wanted/missing", s.arrWantedMissing(app))
	h("GET", "/calendar", s.arrCalendar(app))
	h("GET", "/indexer", s.arrIndexers)
	h("GET", "/downloadclient", arrDownloadClients)
	if app == arrLidarr {
		h("GET", "/customformat", s.arrCustomFormats)
	} else {
		h("GET", "/customformat", s.arrCustomFormatsFor(app))
	}
	h("GET", "/customfilter", arrEmptyList)
	h("GET", "/wanted/cutoff", s.arrWantedCutoff(app))
	h("GET", "/tag/detail", s.arrTagDetails(app))
	h("GET", "/ui/config", arrUIConfig)
	h("GET", "/config/ui", arrUIConfig)
	h("GET", "/config/host", arrHostConfig(app))
	h("GET", "/remotepathmapping", arrEmptyList)
	h("GET", "/notification", arrEmptyList)
	h("GET", "/importlist", arrEmptyList)
	h("GET", "/metadata", arrEmptyList)
	h("GET", "/update", arrEmptyList)
	h("GET", "/system/task", arrEmptyList)
	h("GET", "/log", arrLogPage)
	h("GET", "/history", s.arrHistory(app))
	h("GET", "/history/since", s.arrHistoryList(app))
	h("GET", "/parse", s.arrParse(app))
	h("GET", "/rename", arrEmptyList)
	h("GET", "/manualimport", arrEmptyList)

	app.extraRoutes(s, h)

	switch app {
	case arrRadarr:
		h("GET", "/movie", s.arrListMovies)
		h("GET", "/movie/lookup", s.arrMovieLookup)
		h("GET", "/movie/lookup/tmdb", s.arrMovieLookupTMDB)
		h("GET", "/movie/{id}", s.arrGetMovie)
		h("POST", "/movie", s.arrAddMovie)
		h("PUT", "/movie", s.arrUpdateMovie)
		h("PUT", "/movie/editor", s.arrMovieEditor)
		h("PUT", "/movie/{id}", s.arrUpdateMovie)
		h("DELETE", "/movie/{id}", s.arrDeleteMovie)
		h("GET", "/moviefile", s.arrMovieFiles)
		h("GET", "/moviefile/{id}", s.arrGetMovieFile)
		h("DELETE", "/moviefile/bulk", s.arrDeleteMovieFilesBulk)
		h("DELETE", "/moviefile/{id}", s.arrDeleteMovieFile)
		h("GET", "/history/movie", s.arrHistoryList(arrRadarr))
		h("GET", "/credit", s.arrCredits)
		h("GET", "/mediacover/{id}/{file}", s.arrMediaCover(arrRadarr))
		mux.HandleFunc("GET /mediacover/{id}/{file}", s.arrMediaCover(arrRadarr))
		h("GET", "/release", s.arrListReleases(arrRadarr))
		h("POST", "/release", s.arrGrabRelease(arrRadarr))
		h("POST", "/release/push", arrPushRelease)
	case arrSonarr:
		h("GET", "/series", s.arrListSeries)
		h("GET", "/series/lookup", s.arrSeriesLookup)
		h("GET", "/series/{id}", s.arrGetSeries)
		h("POST", "/series", s.arrAddSeries)
		h("PUT", "/series", s.arrUpdateSeries)
		h("PUT", "/series/editor", s.arrSeriesEditor)
		h("PUT", "/series/{id}", s.arrUpdateSeries)
		h("DELETE", "/series/{id}", s.arrDeleteSeries)
		h("GET", "/episode", s.arrListEpisodes)
		h("GET", "/episode/{id}", s.arrGetEpisode)
		h("PUT", "/episode/{id}", s.arrUpdateEpisode)
		h("PUT", "/episode/monitor", s.arrMonitorEpisodes)
		h("GET", "/episodefile", s.arrEpisodeFiles)
		h("GET", "/episodefile/{id}", s.arrGetEpisodeFile)
		h("DELETE", "/episodefile/bulk", s.arrDeleteEpisodeFilesBulk)
		h("DELETE", "/episodefile/{id}", s.arrDeleteEpisodeFile)
		h("GET", "/history/series", s.arrHistoryList(arrSonarr))
		h("GET", "/mediacover/{id}/{file}", s.arrMediaCover(arrSonarr))
		mux.HandleFunc("GET /mediacover/{id}/{file}", s.arrMediaCover(arrSonarr))
		h("GET", "/release", s.arrListReleases(arrSonarr))
		h("POST", "/release", s.arrGrabRelease(arrSonarr))
		h("POST", "/release/push", arrPushRelease)
	case arrLidarr:
		h("GET", "/artist", s.arrListArtists)
		h("GET", "/artist/lookup", s.arrArtistLookup)
		h("GET", "/artist/{id}", s.arrGetArtist)
		h("POST", "/artist", s.arrAddArtist)
		h("GET", "/album", s.arrListAlbums)
		h("GET", "/album/{id}", s.arrGetAlbum)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := s.arrUser(r)
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Use an administrator's Mediarium API key."})
			return
		}
		// A read-only key only reads here as well.
		if user.ReadOnly() && r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": readOnlyKeyMessage})
			return
		}
		if !s.arrAppOn(app) {
			arrNotFound(w)
			return
		}
		path := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/"+string(app)))
		path = strings.TrimRight(path, "/")
		if path == "/ping" || path == app.apiPrefix()+"/ping" {
			// The health check some apps make before anything else.
			writeJSON(w, http.StatusOK, map[string]string{"status": "OK"})
			return
		}
		if path == "/api" || path == "" {
			// Homarr's connection test, and what the real apps answer there.
			writeJSON(w, http.StatusOK, map[string]any{"current": strings.TrimPrefix(app.apiPrefix(), "/api/"), "deprecated": []string{}})
			return
		}
		r2 := r.WithContext(auth.WithUser(r.Context(), user))
		u := *r.URL
		u.Path, u.RawPath = path, ""
		r2.URL = &u
		// A request nothing here answers is logged (the path and the names of
		// the query values, never the values: one is the API key), so an app
		// that says "something went wrong" can be followed, and it gets the
		// JSON "not found" the real apps send rather than plain text.
		if _, pattern := mux.Handler(r2); pattern == "" {
			names := make([]string, 0, len(r.URL.Query()))
			for k := range r.URL.Query() {
				if !strings.EqualFold(k, "apikey") {
					names = append(names, k)
				}
			}
			slog.Info("arr: an app asked for something Mediarium does not answer", "app", string(app), "method", r.Method, "path", r.URL.Path, "query", strings.Join(names, ","))
			arrNotFound(w)
			return
		}
		sw := &arrStatusWriter{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(sw, r2)
		// The query is shown too, as the settings of the request (sort, page,
		// filters), so an empty card can be matched to what the app asked for.
		var asked []string
		for k, v := range r.URL.Query() {
			if !strings.EqualFold(k, "apikey") && len(v) > 0 && len(v[0]) <= 60 {
				asked = append(asked, k+"="+v[0])
			}
		}
		sort.Strings(asked)
		slog.Debug("arr: request answered", "app", string(app), "method", r.Method, "path", r.URL.Path, "query", strings.Join(asked, "&"), "status", sw.status)
	})
}

// arrStatusWriter remembers the status an answer was sent with, for the log.
type arrStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *arrStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// arrUser is the account behind the API key, from the X-Api-Key header (in
// any casing) or the apikey query parameter.
func (s *Server) arrUser(r *http.Request) *auth.User {
	key := r.Header.Get("X-Api-Key")
	if key == "" {
		// Radarr and Sonarr 5 and 4 also take "Authorization: Bearer <key>".
		if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			key = strings.TrimSpace(h[7:])
		}
	}
	if key == "" {
		key = r.URL.Query().Get("apikey")
	}
	if key == "" {
		return nil
	}
	u, err := s.Auth.UserForAPIKey(key)
	if err != nil {
		return nil
	}
	return u
}

func (s *Server) arrAppOn(app arrApp) bool {
	switch app {
	case arrRadarr:
		return s.moviesEnabled()
	case arrSonarr:
		return s.tvEnabled()
	}
	return s.musicEnabled()
}

// --- small shared shapes and helpers ---

// arrPage is a paged list.
type arrPage struct {
	Page          int    `json:"page"`
	PageSize      int    `json:"pageSize"`
	SortKey       string `json:"sortKey,omitempty"`
	SortDirection string `json:"sortDirection,omitempty"`
	TotalRecords  int    `json:"totalRecords"`
	Records       any    `json:"records"`
}

// paging reads page and pageSize (pageSize -1 or 0 means everything up to
// limit).
func paging(r *http.Request, def int) (page, size int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ = strconv.Atoi(r.URL.Query().Get("pagesize"))
	if size == 0 {
		size, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	}
	if size <= 0 {
		size = def
	}
	if size > 1000 {
		size = 1000
	}
	return page, size
}

// pageOf cuts one page out of n items.
func pageOf(n, page, size int) (from, to int) {
	size = max(size, 1)
	// A huge page number must not overflow the offset: it is past the end.
	if page > 1 && page-1 > n/size {
		return n, n
	}
	from = max(page-1, 0) * size
	if from > n {
		from = n
	}
	to = from + size
	if to > n {
		to = n
	}
	return from, to
}

func arrNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "NotFound"})
}

// arrBadRequest answers a validation problem the way the apps expect it.
func arrBadRequest(w http.ResponseWriter, property, message string) {
	writeJSON(w, http.StatusBadRequest, []map[string]string{{"propertyName": property, "errorMessage": message, "severity": "error"}})
}

// arrAlreadyAdded is the answer for a title that is already in the library,
// with the error code the real apps use: some apps (Overseerr, matcharr)
// read it as "done" rather than as a failure.
func arrAlreadyAdded(w http.ResponseWriter, property, message, code string) {
	writeJSON(w, http.StatusBadRequest, []map[string]string{{"propertyName": property, "errorMessage": message, "errorCode": code, "severity": "error"}})
}

func arrPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		arrNotFound(w)
		return 0, false
	}
	return id, true
}

// arrTime turns "YYYY-MM-DD" or an RFC 3339 time into a full UTC date-time
// ("" when there's nothing usable): some apps refuse a bare date.
func arrTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format("2006-01-02T15:04:05Z")
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02T15:04:05Z")
	}
	if len(s) == 7 { // a month, as some albums have
		if t, err := time.Parse("2006-01", s); err == nil {
			return t.Format("2006-01-02T15:04:05Z")
		}
	}
	if len(s) == 4 {
		if t, err := time.Parse("2006", s); err == nil {
			return t.Format("2006-01-02T15:04:05Z")
		}
	}
	return ""
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

// arrRange reads the calendar's start and end (dates or date-times); the
// default is today to two days on, as in the real apps.
func arrRange(r *http.Request) (start, end string) {
	read := func(key string, def time.Time) string {
		v := r.URL.Query().Get(key)
		if v != "" {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t.UTC().Format("2006-01-02")
			}
			if len(v) >= 10 {
				if t, err := time.Parse("2006-01-02", v[:10]); err == nil {
					return t.Format("2006-01-02")
				}
			}
		}
		return def.Format("2006-01-02")
	}
	now := time.Now().UTC()
	return read("start", now), read("end", now.AddDate(0, 0, 2))
}

func queryBool(r *http.Request, key string) bool {
	v, _ := strconv.ParseBool(r.URL.Query().Get(key))
	return v
}

// arrSlug is a title as a URL-friendly word ("night-harbour").
func arrSlug(title string, id int) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(title) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return strconv.Itoa(id)
	}
	return out
}

// arrCleanTitle is a title with only letters and digits, as the apps sort by.
func arrCleanTitle(title string) string {
	return strings.ReplaceAll(arrSlug(title, 0), "-", "")
}

// arrSortTitle drops a leading "the", "a" or "an".
func arrSortTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, p := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, p) {
			return strings.TrimSpace(t[len(p):])
		}
	}
	return t
}

// --- system ---

func (s *Server) arrSystemStatus(app arrApp) http.HandlerFunc {
	started := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"appName":                app.name(),
			"instanceName":           "Mediarium",
			"version":                arrVersions[app],
			"buildTime":              started,
			"isDebug":                false,
			"isProduction":           true,
			"isAdmin":                false,
			"isUserInteractive":      false,
			"startupPath":            "/app",
			"appData":                "/config",
			"osName":                 "linux",
			"isDocker":               fsinfo.InDocker(),
			"isLinux":                true,
			"isOsx":                  false,
			"isWindows":              false,
			"mode":                   "console",
			"branch":                 "main",
			"authentication":         "external",
			"urlBase":                s.cfg.URLBase + "/" + string(app),
			"runtimeName":            "Mediarium",
			"runtimeVersion":         s.version,
			"startTime":              started,
			"packageVersion":         s.version,
			"packageAuthor":          "Mediarium",
			"packageUpdateMechanism": "docker",
		})
	}
}

// --- tags ---

type arrTag struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

func (s *Server) arrListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.MovieRepo.ArrTags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]arrTag, 0, len(tags))
	for _, t := range tags {
		out = append(out, arrTag{ID: t.ID, Label: t.Label})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) arrGetTag(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	labels, err := s.MovieRepo.ArrTagLabels([]int64{id})
	if err != nil || len(labels) == 0 {
		arrNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, arrTag{ID: id, Label: labels[0]})
}

func (s *Server) arrCreateTag(w http.ResponseWriter, r *http.Request) {
	var req arrTag
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "Label", "Send the tag as {\"label\": \"...\"}.")
		return
	}
	t, err := s.MovieRepo.ArrTagFor(req.Label)
	if errors.Is(err, library.ErrInvalidTag) {
		arrBadRequest(w, "Label", "A tag needs a name.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, arrTag{ID: t.ID, Label: t.Label})
}

func (s *Server) arrRenameTag(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	var req arrTag
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "Label", "Send the tag as {\"label\": \"...\"}.")
		return
	}
	t, err := s.MovieRepo.RenameArrTag(id, req.Label)
	switch {
	case errors.Is(err, library.ErrInvalidTag):
		arrBadRequest(w, "Label", strings.TrimPrefix(err.Error(), library.ErrInvalidTag.Error()+": "))
		return
	case err != nil:
		arrNotFound(w)
		return
	}
	writeJSON(w, http.StatusAccepted, arrTag{ID: t.ID, Label: t.Label})
}

// tagNumbers maps tag names (lower case) to their numbers.
func (s *Server) tagNumbers() map[string]int64 {
	out := map[string]int64{}
	tags, err := s.MovieRepo.ArrTags()
	if err != nil {
		return out
	}
	for _, t := range tags {
		out[strings.ToLower(t.Label)] = t.ID
	}
	return out
}

func tagIDs(labels []string, numbers map[string]int64) []int64 {
	out := []int64{}
	for _, l := range labels {
		if id, ok := numbers[strings.ToLower(l)]; ok {
			out = append(out, id)
		}
	}
	return out
}

// setArrTags gives a title the tags with these numbers, replacing its own.
func (s *Server) setArrTags(k library.TitleKind, id int64, tmdbID int, ids []int64) {
	labels, err := s.MovieRepo.ArrTagLabels(ids)
	if err != nil {
		return
	}
	added, removed, err := s.MovieRepo.SetTitleTags(k, id, labels)
	if err != nil {
		return
	}
	s.pushTags(k, tmdbID, added, removed)
}

// --- folders and disks ---

type arrRootFolder struct {
	ID              int      `json:"id"`
	Path            string   `json:"path"`
	Name            string   `json:"name,omitempty"`
	Accessible      bool     `json:"accessible"`
	FreeSpace       int64    `json:"freeSpace"`
	TotalSpace      int64    `json:"totalSpace"`
	UnmappedFolders []string `json:"unmappedFolders"`
}

func (s *Server) arrRoots(app arrApp) []string {
	switch app {
	case arrRadarr:
		return s.movieFolders()
	case arrSonarr:
		return s.tvFolders()
	}
	return []string{s.musicRoot()}
}

func (s *Server) arrRootFolders(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := []arrRootFolder{}
		for i, p := range s.arrRoots(app) {
			if p == "" {
				continue
			}
			f := fsinfo.Inspect(p)
			out = append(out, arrRootFolder{ID: i + 1, Path: p, Name: p, Accessible: f.Exists && f.IsDir, FreeSpace: int64(f.FreeBytes),
				TotalSpace: int64(f.TotalBytes), UnmappedFolders: []string{}})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) arrDiskSpace(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, p := range append(append(s.movieFolders(), s.tvFolders()...), s.musicRoot()) {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		f := fsinfo.Inspect(p)
		out = append(out, map[string]any{"path": p, "label": p, "freeSpace": int64(f.FreeBytes), "totalSpace": int64(f.TotalBytes)})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- qualities and profiles ---

type arrQualityName struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Resolution int    `json:"resolution"`
	Modifier   string `json:"modifier,omitempty"`
}

type arrQualityModel struct {
	Quality  arrQualityName `json:"quality"`
	Revision arrRevision    `json:"revision"`
}

type arrRevision struct {
	Version  int  `json:"version"`
	Real     int  `json:"real"`
	IsRepack bool `json:"isRepack"`
}

// arrQualityOf names a Mediarium quality the way Radarr or Sonarr does.
func arrQualityOf(app arrApp, t quality.Tier) arrQualityName {
	type q = arrQualityName
	if app == arrSonarr {
		switch t {
		case quality.TierSDTV:
			return q{1, "SDTV", "television", 480, ""}
		case quality.TierDVD:
			return q{2, "DVD", "dvd", 480, ""}
		case quality.TierWebDL480p:
			return q{8, "WEBDL-480p", "web", 480, ""}
		case quality.TierHDTV720p:
			return q{4, "HDTV-720p", "television", 720, ""}
		case quality.TierWebDL720p:
			return q{5, "WEBDL-720p", "web", 720, ""}
		case quality.TierBluray720p:
			return q{6, "Bluray-720p", "bluray", 720, ""}
		case quality.TierHDTV1080p:
			return q{9, "HDTV-1080p", "television", 1080, ""}
		case quality.TierWebDL1080p:
			return q{3, "WEBDL-1080p", "web", 1080, ""}
		case quality.TierBluray1080p:
			return q{7, "Bluray-1080p", "bluray", 1080, ""}
		case quality.TierRemux1080p:
			return q{20, "Bluray-1080p Remux", "blurayRaw", 1080, ""}
		case quality.TierHDTV2160p:
			return q{16, "HDTV-2160p", "television", 2160, ""}
		case quality.TierWebDL2160p:
			return q{18, "WEBDL-2160p", "web", 2160, ""}
		case quality.TierBluray2160p:
			return q{19, "Bluray-2160p", "bluray", 2160, ""}
		case quality.TierRemux2160p:
			return q{21, "Bluray-2160p Remux", "blurayRaw", 2160, ""}
		}
		return q{0, "Unknown", "unknown", 0, ""}
	}
	switch t {
	case quality.TierPreRelease:
		return q{26, "TELESYNC", "telesync", 720, "none"}
	case quality.TierSDTV:
		return q{1, "SDTV", "tv", 480, "none"}
	case quality.TierDVD:
		return q{2, "DVD", "dvd", 480, "none"}
	case quality.TierWebDL480p:
		return q{8, "WEBDL-480p", "webdl", 480, "none"}
	case quality.TierHDTV720p:
		return q{4, "HDTV-720p", "tv", 720, "none"}
	case quality.TierWebDL720p:
		return q{5, "WEBDL-720p", "webdl", 720, "none"}
	case quality.TierBluray720p:
		return q{6, "Bluray-720p", "bluray", 720, "none"}
	case quality.TierHDTV1080p:
		return q{9, "HDTV-1080p", "tv", 1080, "none"}
	case quality.TierWebDL1080p:
		return q{3, "WEBDL-1080p", "webdl", 1080, "none"}
	case quality.TierBluray1080p:
		return q{7, "Bluray-1080p", "bluray", 1080, "none"}
	case quality.TierRemux1080p:
		return q{30, "Remux-1080p", "bluray", 1080, "remux"}
	case quality.TierHDTV2160p:
		return q{16, "HDTV-2160p", "tv", 2160, "none"}
	case quality.TierWebDL2160p:
		return q{18, "WEBDL-2160p", "webdl", 2160, "none"}
	case quality.TierBluray2160p:
		return q{19, "Bluray-2160p", "bluray", 2160, "none"}
	case quality.TierRemux2160p:
		return q{31, "Remux-2160p", "bluray", 2160, "remux"}
	}
	return q{0, "Unknown", "unknown", 0, "none"}
}

func arrQualityModelOf(app arrApp, t quality.Tier) arrQualityModel {
	return arrQualityModel{Quality: arrQualityOf(app, t), Revision: arrRevision{Version: 1}}
}

type arrLanguage struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

var arrEnglish = arrLanguage{ID: 1, Name: "English"}

// arrProfileItem is one entry of a profile's quality list: a quality, or (with
// Name, ID and Items set and no Quality) a group of them, as Radarr and Sonarr
// show it.
type arrProfileItem struct {
	ID      int             `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Quality *arrQualityName `json:"quality"`
	Items   []any           `json:"items"`
	Allowed bool            `json:"allowed"`
}

// arrGroupIDBase is where the ids of quality groups start, above every
// quality id, as Radarr and Sonarr number theirs.
const arrGroupIDBase = 1000

type arrProfile struct {
	ID                int64            `json:"id"`
	Name              string           `json:"name"`
	UpgradeAllowed    bool             `json:"upgradeAllowed"`
	Cutoff            int              `json:"cutoff"`
	Items             []arrProfileItem `json:"items"`
	MinFormatScore    int              `json:"minFormatScore"`
	CutoffFormatScore int              `json:"cutoffFormatScore"`
	// MinUpgradeFormatScore is the smallest score gain that makes a
	// release an upgrade. Mediarium keeps none of its own; what an app wrote
	// is given back, else 1 as the apps default it.
	MinUpgradeFormatScore int              `json:"minUpgradeFormatScore"`
	FormatItems           []map[string]any `json:"formatItems"`
	Language              *arrLanguage     `json:"language,omitempty"`
}

func toArrProfile(app arrApp, p quality.Profile) arrProfile {
	allowed := map[quality.Tier]bool{}
	for _, t := range p.Allowed {
		allowed[t] = true
	}
	out := arrProfile{ID: p.ID, Name: p.Name, UpgradeAllowed: p.UpgradeAllowed, Cutoff: arrQualityOf(app, p.Cutoff).ID,
		MinFormatScore: p.MinFormatScore, CutoffFormatScore: p.CutoffFormatScore, FormatItems: []map[string]any{}}
	single := func(t quality.Tier, ok bool) arrProfileItem {
		q := arrQualityOf(app, t)
		return arrProfileItem{Quality: &q, Items: []any{}, Allowed: ok}
	}
	for _, t := range quality.AllTiers() {
		if app == arrSonarr && t == quality.TierPreRelease {
			continue
		}
		if p.HasCustomOrder() && p.ItemOf(t) >= 0 {
			continue // listed below, in the profile's own order
		}
		out.Items = append(out.Items, single(t, allowed[t]))
	}
	// Radarr and Sonarr list the worst quality first, so the profile's order
	// (best first) is written backwards.
	for i := len(p.QualityOrder) - 1; i >= 0; i-- {
		it := p.QualityOrder[i]
		if !it.IsGroup() {
			if app == arrSonarr && it.Tiers[0] == quality.TierPreRelease {
				continue
			}
			out.Items = append(out.Items, single(it.Tiers[0], true))
			continue
		}
		id := arrGroupIDBase + len(p.QualityOrder) - i
		group := arrProfileItem{ID: id, Name: it.Name, Items: []any{}, Allowed: true}
		for j := len(it.Tiers) - 1; j >= 0; j-- {
			if app == arrSonarr && it.Tiers[j] == quality.TierPreRelease {
				continue
			}
			group.Items = append(group.Items, single(it.Tiers[j], true))
		}
		out.Items = append(out.Items, group)
		if ci := p.ItemOf(p.Cutoff); ci == i {
			out.Cutoff = id
		}
	}
	for _, f := range p.Formats {
		if f.Format.App != "" && string(f.Format.App) != string(app) {
			continue // the other app's formats are not this app's to see
		}
		out.FormatItems = append(out.FormatItems, map[string]any{"format": f.Format.ID, "name": f.Format.Name, "score": f.Score})
	}
	if app == arrRadarr {
		out.Language = &arrEnglish
	}
	applyArrExtra(app, p, &out)
	return out
}

// arrMusicProfiles answers Lidarr's quality profiles from the music ones.
func (s *Server) arrMusicProfiles() []map[string]any {
	out := []map[string]any{}
	list, err := s.MusicRepo.ListProfiles()
	if err != nil {
		return out
	}
	for _, p := range list {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "upgradeAllowed": false, "cutoff": 0, "items": []any{}})
	}
	return out
}

func (s *Server) arrQualityProfiles(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app == arrLidarr {
			writeJSON(w, http.StatusOK, s.arrMusicProfiles())
			return
		}
		ps, err := s.loadProfiles()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		list, err := s.QualityRepo.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]arrProfile, 0, len(list))
		for _, p := range list {
			out = append(out, toArrProfile(app, ps.resolve(p.ID)))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) arrQualityProfile(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := arrPathID(w, r)
		if !ok {
			return
		}
		if app == arrLidarr {
			for _, p := range s.arrMusicProfiles() {
				if p["id"] == id {
					writeJSON(w, http.StatusOK, p)
					return
				}
			}
			arrNotFound(w)
			return
		}
		ps, err := s.loadProfiles()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, ok := ps.byID[id]; !ok {
			arrNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, toArrProfile(app, ps.resolve(id)))
	}
}

func (s *Server) arrQualityDefinitions(app arrApp) http.HandlerFunc {
	if app != arrLidarr {
		return s.arrQualityDefinitionList(app)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		out := []map[string]any{}
		for i, t := range quality.AllTiers() {
			q := arrQualityOf(app, t)
			out = append(out, map[string]any{"id": i + 1, "quality": q, "title": q.Name, "weight": i + 1, "minSize": 0, "maxSize": 0})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func arrLanguageProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{{
		"id": 1, "name": "English", "upgradeAllowed": false, "cutoff": arrEnglish,
		"languages": []map[string]any{{"language": arrEnglish, "allowed": true}},
	}})
}

// arrProfileID is the profile a title uses as the apps should see it: its
// own, or the default.
func (s *Server) arrProfileID(own, def int64) int64 {
	if own != 0 {
		return own
	}
	return def
}

// arrPickProfile checks a profile number an app sent; 0 means the default.
func (s *Server) arrPickProfile(id int64) int64 {
	if id <= 0 {
		return 0
	}
	if _, err := s.QualityRepo.Get(id); err != nil {
		return 0
	}
	return id
}

// --- commands ---

const (
	// arrMaxCommandIDs is how many movies, episodes or albums one command may
	// name.
	arrMaxCommandIDs = 200
	// arrMaxWaitingCommands is how many commands may wait for their turn.
	arrMaxWaitingCommands = 20
)

// arrLine runs the commands the apps send one after another, and within a
// command its titles one after another. Before, a MoviesSearch for 1,000 ids
// started 1,000 searches at once and a person's own search waited behind them.
type arrLine struct {
	mu      sync.Mutex
	waiting [][]func()
	running bool
}

// arrLines holds each server's line (tests run several servers in one process).
var arrLines sync.Map // *Server -> *arrLine

func (s *Server) arrCommandLine() *arrLine {
	l, _ := arrLines.LoadOrStore(s, &arrLine{})
	return l.(*arrLine)
}

// add puts one command's jobs at the back of the line and starts working
// through it if nothing is. It reports false, adding nothing, when the line is
// full.
func (l *arrLine) add(s *Server, jobs []func()) bool {
	l.mu.Lock()
	if len(l.waiting) >= arrMaxWaitingCommands {
		l.mu.Unlock()
		return false
	}
	l.waiting = append(l.waiting, jobs)
	start := !l.running
	l.running = true
	l.mu.Unlock()
	if start {
		s.background(l.drain)
	}
	return true
}

// drain runs the waiting commands until none is left.
func (l *arrLine) drain() {
	for {
		l.mu.Lock()
		if len(l.waiting) == 0 {
			l.running = false
			l.mu.Unlock()
			return
		}
		next := l.waiting[0]
		l.waiting = l.waiting[1:]
		l.mu.Unlock()
		for _, job := range next {
			job()
		}
	}
}

// arrCommand starts what the app asks for. Searches run in the background;
// commands that mean nothing here are accepted and done at once.
func (s *Server) arrCommand(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name         string  `json:"name"`
			MovieID      int64   `json:"movieId"`
			MovieIDs     []int64 `json:"movieIds"`
			SeriesID     int64   `json:"seriesId"`
			SeasonNumber *int    `json:"seasonNumber"`
			EpisodeIDs   []int64 `json:"episodeIds"`
			ArtistID     int64   `json:"artistId"`
			AlbumIDs     []int64 `json:"albumIds"`
		}
		if err := decodeJSON(r, &req); err != nil || req.Name == "" {
			arrBadRequest(w, "Name", "Send the command as {\"name\": \"...\"}.")
			return
		}
		for _, list := range []struct {
			name string
			ids  []int64
		}{{"MovieIds", req.MovieIDs}, {"EpisodeIds", req.EpisodeIDs}, {"AlbumIds", req.AlbumIDs}} {
			if len(list.ids) > arrMaxCommandIDs {
				arrBadRequest(w, list.name, fmt.Sprintf("Send at most %d at a time.", arrMaxCommandIDs))
				return
			}
		}
		// Every command runs in one line, one after another, so a long list
		// from an app never crowds out the searches a person starts.
		var jobs []func()
		switch strings.ToLower(req.Name) {
		case "moviessearch", "moviesearch":
			for _, id := range req.MovieIDs {
				jobs = append(jobs, func() { s.searchMovieInBackground(id) })
			}
		case "rescanmovie", "refreshmovie":
			// What apps like Tdarr send after they change a file: look at the
			// files again and tell the media servers (see arr_rescan.go).
			ids := append([]int64{}, req.MovieIDs...)
			if req.MovieID > 0 {
				ids = append(ids, req.MovieID)
			}
			for _, id := range ids {
				jobs = append(jobs, func() { s.rescanMovieFiles(id, false) })
			}
		case "rescanseries":
			if req.SeriesID > 0 {
				id := req.SeriesID
				jobs = append(jobs, func() { s.rescanSeriesFiles(id, false) })
			}
		case "missingmoviessearch", "cutoffunmetmoviessearch":
			jobs = append(jobs, s.arrSearchMissingMovies)
		case "seriessearch", "missingepisodesearch":
			sc := tvScope{seriesID: max(req.SeriesID, 0)}
			jobs = append(jobs, func() { s.arrSearchTV(sc) })
		case "seasonsearch":
			if req.SeriesID > 0 && req.SeasonNumber != nil {
				sc := tvScope{seriesID: req.SeriesID, season: *req.SeasonNumber, specials: *req.SeasonNumber == 0}
				jobs = append(jobs, func() { s.arrSearchTV(sc) })
			}
		case "episodesearch":
			for _, id := range req.EpisodeIDs {
				jobs = append(jobs, func() {
					ep, err := s.MovieRepo.GetEpisodeByID(id)
					if err != nil {
						return
					}
					s.arrSearchTV(tvScope{seriesID: ep.SeriesID, season: ep.Season, episode: ep.Episode, specials: ep.Season == 0, force: true})
				})
			}
		case "refreshseries":
			if req.SeriesID > 0 {
				id := req.SeriesID
				jobs = append(jobs, func() {
					if sr, err := s.MovieRepo.GetSeries(id); err == nil {
						ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
						defer cancel()
						_ = s.refreshSeries(ctx, sr)
					}
					s.rescanSeriesFiles(id, false)
				})
			}
		case "artistsearch":
			if req.ArtistID > 0 {
				id := req.ArtistID
				jobs = append(jobs, func() { s.searchArtistAlbums(context.Background(), id) })
			}
		case "albumsearch":
			for _, id := range req.AlbumIDs {
				jobs = append(jobs, func() { s.arrSearchAlbum(id) })
			}
		}
		if len(jobs) > 0 && !s.arrCommandLine().add(s, jobs) {
			writeJSON(w, http.StatusServiceUnavailable, []map[string]string{{"propertyName": "Name", "errorMessage": "Too many commands are waiting already. Try again once they have finished.", "severity": "error"}})
			return
		}
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": time.Now().UnixMilli() % 1_000_000, "name": req.Name, "commandName": req.Name, "message": "Started",
			"priority": "normal", "status": "started", "queued": now, "started": now, "trigger": "manual",
			"stateChangeTime": now, "sendUpdatesToClient": false, "updateScheduledTask": false,
		})
	}
}

func arrCommandStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": "", "status": "completed", "result": "successful", "queued": now, "ended": now})
}

// arrSearchTV runs a search for the shows in scope (all wanted episodes when
// the scope is empty), like Search now.
func (s *Server) arrSearchTV(scope tvScope) {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt, scope.force), "search-now", profiles, scope)
}

// arrSearchMissingMovies searches for monitored, released movies that are
// still missing, at most as many as one scheduled search would.
func (s *Server) arrSearchMissingMovies() {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	movies, err := s.MovieRepo.ListWithCopies()
	if err != nil {
		return
	}
	blocked := s.searchSkipKeys()
	searched := 0
	for _, m := range movies {
		if !m.Monitored || m.Status != library.StatusMissing || (m.ReleaseDate != "" && m.ReleaseDate > today()) {
			continue
		}
		if searched++; searched > maxBulkSearch {
			return
		}
		s.huntMovie(context.Background(), m, instances, profiles, blocked, false)
	}
}

func (s *Server) arrSearchAlbum(albumID int64) {
	album, err := s.MusicRepo.GetAlbum(albumID)
	if err != nil {
		return
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profile, err := s.albumProfile(artist)
	if err != nil {
		return
	}
	s.huntAlbum(context.Background(), artist, album, profile, instances, s.searchSkipKeys(), true)
}

// arrImage is one picture of a title.
type arrImage struct {
	CoverType string `json:"coverType"`
	URL       string `json:"url"`
	RemoteURL string `json:"remoteUrl"`
}

func posterImages(posterURL string) []arrImage {
	if posterURL == "" {
		return []arrImage{}
	}
	return []arrImage{{CoverType: "poster", URL: posterURL, RemoteURL: posterURL}}
}

// arrQueryTerm splits a lookup term such as "tmdb:603" into its kind and id.
func arrQueryTerm(r *http.Request) (kind, value string) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		return "", ""
	}
	if i := strings.Index(term, ":"); i > 0 {
		k := strings.ToLower(term[:i])
		switch k {
		case "tmdb", "tvdb", "imdb", "lidarr", "mbid":
			return k, strings.TrimSpace(term[i+1:])
		}
	}
	// r.URL.Query() has already decoded the term once; decoding again would
	// turn a real "+" into a space and choke on a real "%".
	return "", term
}
