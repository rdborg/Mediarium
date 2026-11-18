package api

import (
	"net/http"
	"sync"
	"time"
)

// More of what the phone apps load on their home and settings screens, with
// the shapes the real apps give. Where Mediarium keeps nothing for it, the
// answer is an empty list or the settings of an app with its defaults.

// arrRecsTTL is how long the recommended movies are kept before TMDB is
// asked again.
const arrRecsTTL = time.Hour

// arrRecommended keeps the last list of recommended movies.
type arrRecommended struct {
	mu      sync.Mutex
	at      time.Time
	movies  []arrMovie
	fetched bool
}

// arrRecommendedMovie is a movie of /importlist/movie: the full lookup
// resource with the flags the apps read to tell a recommendation from a movie
// of the library.
type arrRecommendedMovie struct {
	arrMovie
	IsRecommendation bool  `json:"isRecommendation"`
	IsExisting       bool  `json:"isExisting"`
	IsExcluded       bool  `json:"isExcluded"`
	Lists            []int `json:"lists"`
}

// arrImportListMovies answers GET /importlist/movie, the movies Radarr
// recommends from its import lists and TMDB. Here that is what's trending on
// TMDB, minus what the library already has, at most 20, kept for an hour.
func (s *Server) arrImportListMovies(w http.ResponseWriter, r *http.Request) {
	s.arrRecs.mu.Lock()
	defer s.arrRecs.mu.Unlock()
	if !s.arrRecs.fetched || time.Since(s.arrRecs.at) > arrRecsTTL {
		fresh := []arrMovie{}
		if c := s.TMDB(); c != nil {
			trending, err := c.TrendingMovies(r.Context())
			if err != nil {
				// Asked again next time; the app gets an empty list now.
				writeJSON(w, http.StatusOK, fresh)
				return
			}
			for _, m := range trending {
				fresh = append(fresh, tmdbArrMovie(m))
			}
		}
		s.arrRecs.movies, s.arrRecs.at, s.arrRecs.fetched = fresh, time.Now(), true
	}
	// Owned movies are left out each time, so adding one drops it at once.
	out := []arrRecommendedMovie{}
	for _, m := range s.arrRecs.movies {
		if len(out) >= 20 {
			break
		}
		if _, ok, _ := s.MovieRepo.GetByTMDBID(m.TMDBID); ok {
			continue
		}
		out = append(out, arrRecommendedMovie{arrMovie: m, IsRecommendation: true, Lists: []int{}})
	}
	writeJSON(w, http.StatusOK, out)
}

// arrEmptyPage answers a paged list with nothing in it.
func arrEmptyPage(w http.ResponseWriter, r *http.Request) {
	page, size := paging(r, 20)
	q := r.URL.Query()
	writeJSON(w, http.StatusOK, arrPage{Page: page, PageSize: size, SortKey: q.Get("sortKey"), SortDirection: q.Get("sortDirection"), TotalRecords: 0, Records: []any{}})
}

// arrMediaManagementConfig answers GET /config/mediamanagement.
func (s *Server) arrMediaManagementConfig(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"id": 1, "autoUnmonitorPreviouslyDownloadedMovies": false, "recycleBin": "", "recycleBinCleanupDays": 7,
		"downloadPropersAndRepacks": "preferAndUpgrade", "createEmptyMovieFolders": false, "deleteEmptyFolders": false,
		"fileDate": "none", "rescanAfterRefresh": "always", "setPermissionsLinux": false, "chmodFolder": "755",
		"chownGroup": "", "skipFreeSpaceCheckWhenImporting": false, "minimumFreeSpaceWhenImporting": 100,
		"copyUsingHardlinks": true, "useScriptImport": false, "scriptImportPath": "", "importExtraFiles": false,
		"extraFileExtensions": "srt", "enableMediaInfo": true, "autoRenameFolders": false, "pathsDefaultStatic": false,
		"autoUnmonitorPreviouslyDownloadedEpisodes": false, "watchLibraryForChanges": true,
	}
	if v, ok := s.arrConfigEcho()["downloadPropersAndRepacks"].(string); ok {
		out["downloadPropersAndRepacks"] = v
	}
	writeJSON(w, http.StatusOK, out)
}

// arrDownloadClientConfig answers GET /config/downloadclient.
func arrDownloadClientConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id": 1, "downloadClientWorkingFolders": "_UNPACK_|_FAILED_", "enableCompletedDownloadHandling": true,
		"autoRedownloadFailed": true, "autoRedownloadFailedFromInteractiveSearch": true, "checkForFinishedDownloadInterval": 1,
	})
}

// arrIndexerConfig answers GET /config/indexer.
func arrIndexerConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": 1, "minimumAge": 0, "maximumSize": 0, "retention": 0, "rssSyncInterval": 60, "preferIndexerFlags": false, "availabilityDelay": 0, "allowHardcodedSubs": false, "whitelistedHardcodedSubs": ""})
}

// arrDelayProfiles answers GET /delayprofile with the one default profile.
func arrDelayProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{{
		"id": 1, "enableUsenet": true, "enableTorrent": true, "preferredProtocol": "usenet", "usenetDelay": 0,
		"torrentDelay": 0, "bypassIfHighestQuality": false, "bypassIfAboveCustomFormatScore": false,
		"minimumCustomFormatScore": 0, "order": 2147483647, "tags": []int{},
	}})
}

// extraRoutes mounts the home and settings screen answers above.
func (a arrApp) extraRoutes(s *Server, h func(method, path string, fn http.HandlerFunc)) {
	for _, p := range []string{"/exclusions", "/importexclusion", "/importlistexclusion", "/autotagging", "/releaseprofile", "/system/backup"} {
		h("GET", p, arrEmptyList)
	}
	h("GET", "/exclusions/paged", arrEmptyPage)
	h("GET", "/importexclusion/paged", arrEmptyPage)
	h("GET", "/importlistexclusion/paged", arrEmptyPage)
	h("GET", "/config/mediamanagement", s.arrMediaManagementConfig)
	h("GET", "/config/naming", s.arrNamingConfig)
	if a != arrLidarr {
		h("PUT", "/config/naming/{id}", s.arrUpdateNamingConfig(a))
		h("PUT", "/config/mediamanagement/{id}", s.arrUpdateMediaManagement)
	}
	h("GET", "/config/downloadclient", arrDownloadClientConfig)
	h("GET", "/config/indexer", arrIndexerConfig)
	h("GET", "/delayprofile", arrDelayProfiles)
	if a == arrRadarr {
		h("GET", "/importlist/movie", s.arrImportListMovies)
	}
}
