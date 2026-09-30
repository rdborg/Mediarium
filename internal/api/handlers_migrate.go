package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/migrate"
	"github.com/rdborg/mediarium/internal/settings"
)

// newMigrator wires the importer for the other apps (Radarr, Sonarr, Prowlarr, SABnzbd, NZBGet and more) to the
// library, indexers, Usenet servers and quality profiles.
func (s *Server) newMigrator() *migrate.Importer {
	return migrate.New(migrate.Deps{
		Library:          s.MovieRepo,
		Indexers:         s.IndexerRepo,
		Servers:          s.ClientRepo,
		Profiles:         s.QualityRepo,
		TMDB:             s.TMDB,
		MoviesRoot:       s.moviesRoot,
		TVRoot:           s.tvRoot,
		DefaultProfileID: s.defaultProfileID,
		Definition:       s.Definitions.Summary,
		TestIndexer: func(ctx context.Context, inst indexers.Instance) error {
			if res := s.testIndexerInstance(ctx, inst); !res.OK {
				return errors.New(res.Message)
			}
			return nil
		},
		SubtitleLanguages: s.subtitleLanguages,
		SetSubtitleLanguages: func(codes []string) error {
			return s.Settings.Set(settings.KeySubtitleLanguages, strings.Join(codes, ","), false)
		},
		Activity: func(movieID, seriesID int64, message string) {
			if err := s.QueueRepo.LogItemActivity(movieID, seriesID, "imported", message); err != nil {
				slog.Warn("migrate: log activity", "err", err)
			}
		},
	})
}

func hasSource(src migrate.Sources) bool { return src.Any() }

// handleMigratePreview reads the connected apps (Radarr, Sonarr, Prowlarr, SABnzbd, NZBGet and others; only GET requests, plus the read-only version and config calls to NZBGet) and reports what an import would bring over, changing nothing.
func (s *Server) handleMigratePreview(w http.ResponseWriter, r *http.Request) {
	var req migrate.Options
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if !hasSource(req.Sources) {
		writeError(w, http.StatusBadRequest, "Connect at least one app to import from (Radarr, Sonarr, Prowlarr, SABnzbd, NZBGet, Jackett, NZBHydra, Overseerr, Ombi, Bazarr, Medusa or SickChill).")
		return
	}
	if rejectBad(w, checkMigrateOptions(req)) {
		return
	}
	pv, err := s.migrator.Preview(r.Context(), req)
	if err != nil {
		slog.Error("migrate: preview", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read Mediarium's own library: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// handleMigrateRun starts importing from the connected apps in the background; GET /api/migrate/status follows it.
func (s *Server) handleMigrateRun(w http.ResponseWriter, r *http.Request) {
	var req migrate.Options
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if !hasSource(req.Sources) {
		writeError(w, http.StatusBadRequest, "Connect at least one app to import from (Radarr, Sonarr, Prowlarr, SABnzbd, NZBGet, Jackett, NZBHydra, Overseerr, Ombi, Bazarr, Medusa or SickChill).")
		return
	}
	if rejectBad(w, checkMigrateOptions(req)) {
		return
	}
	if err := s.migrator.Start(req); errors.Is(err, migrate.ErrRunning) {
		writeError(w, http.StatusConflict, "An import is already running.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, s.migrator.Status())
}

// handleMigrateStatus reports the progress and results of the current or last import.
func (s *Server) handleMigrateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.migrator.Status())
}

// Limits for an import request.
const (
	migrateMaxMappings = 200
	migrateMaxDirect   = 500
)

// checkMigrateOptions checks the addresses, keys and folder mappings of an
// import request before anything is contacted.
func checkMigrateOptions(req migrate.Options) string {
	src := req.Sources
	keyed := []struct {
		app, example, where string
		conn                *migrate.Conn
	}{
		{"Radarr", "http://192.168.1.10:7878", "Settings > General", src.Radarr},
		{"Sonarr", "http://192.168.1.10:8989", "Settings > General", src.Sonarr},
		{"Prowlarr", "http://192.168.1.10:9696", "Settings > General", src.Prowlarr},
		{"SABnzbd", "http://192.168.1.10:8080", "Config > General", src.SABnzbd},
		{"Overseerr", "http://192.168.1.10:5055", "Settings > General", src.Overseerr},
		{"Ombi", "http://192.168.1.10:3579", "Settings > Configuration", src.Ombi},
		{"Bazarr", "http://192.168.1.10:6767", "Settings > General", src.Bazarr},
		{"Medusa", "http://192.168.1.10:8081", "Config > General", src.Medusa},
		{"SickChill", "http://192.168.1.10:8081", "Config > General", src.SickChill},
	}
	if src.Jackett != nil {
		keyed = append(keyed, struct {
			app, example, where string
			conn                *migrate.Conn
		}{"Jackett", "http://192.168.1.10:9117", "the top of its dashboard", &src.Jackett.Conn})
	}
	for _, k := range keyed {
		if k.conn == nil {
			continue
		}
		if m := checkMigrateConn(k.app, k.example, k.conn.URL); m != "" {
			return m
		}
		if m := firstProblem(
			checkRequired(k.conn.APIKey, fmt.Sprintf("Add the API key for %s. You can find it in %s.", k.app, k.where)),
			checkMigrateKey(k.conn.APIKey),
		); m != "" {
			return m
		}
	}
	if h := src.NZBHydra; h != nil {
		if m := firstProblem(checkMigrateConn("NZBHydra", "http://192.168.1.10:5076", h.URL), checkMigrateKey(h.APIKey)); m != "" {
			return m
		}
	}
	if n := src.NZBGet; n != nil {
		if m := firstProblem(
			checkMigrateConn("NZBGet", "http://192.168.1.10:6789", n.URL),
			checkMaxLen(n.Username, "The NZBGet username", 200), checkNoControl(n.Username, "The NZBGet username"),
			checkMaxLen(n.Password, "The NZBGet password", 200), checkNoControl(n.Password, "The NZBGet password"),
		); m != "" {
			return m
		}
	}
	if j := src.Jackett; j != nil {
		if len(j.Direct) > migrateMaxDirect {
			return fmt.Sprintf("Pick at most %d Jackett sites to add directly.", migrateMaxDirect)
		}
		for _, id := range j.Direct {
			if m := firstProblem(checkMaxLen(id, "A Jackett site name", 100), checkNoControl(id, "A Jackett site name")); m != "" {
				return m
			}
		}
	}

	if len(req.PathMap) > migrateMaxMappings {
		return fmt.Sprintf("Use at most %d folder mappings.", migrateMaxMappings)
	}
	for _, m := range req.PathMap {
		from, to := strings.TrimSpace(m.From), strings.TrimSpace(m.To)
		if from == "" && to == "" {
			continue
		}
		if from == "" || to == "" {
			return "Fill in both sides of each folder mapping: the folder as the other app sees it, and where it is in Mediarium."
		}
		if p := firstProblem(
			checkNoControl(from, "The other app's folder"), checkMaxLen(from, "The other app's folder", maxPathLen),
			checkAbsPath(to, "/media/movies"), checkMaxLen(to, "The Mediarium folder", maxPathLen),
		); p != "" {
			return p
		}
	}
	for name, id := range req.ProfileMapping {
		if id < 0 {
			return fmt.Sprintf("The profile chosen for %q doesn't exist. Pick one from the list.", name)
		}
	}
	return ""
}

// checkMigrateConn checks the address of one app to import from.
func checkMigrateConn(app, example, address string) string {
	return firstProblem(
		checkRequired(address, fmt.Sprintf("Add the address of %s, for example %s.", app, example)),
		checkHTTPURL(address, example, false),
		checkMaxLen(address, "The address", 2000),
		checkNoControl(address, "The address"),
	)
}

// checkMigrateKey checks an API key. SickChill puts its key in the address
// path, so a key can't hold characters that would change where it points.
func checkMigrateKey(key string) string {
	if m := checkAPIKey(key); m != "" {
		return m
	}
	if strings.ContainsAny(key, `/\?#&`) {
		return "That key has characters an API key doesn't use. Copy just the key itself, with nothing around it."
	}
	return ""
}
