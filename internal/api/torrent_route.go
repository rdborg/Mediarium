package api

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
)

// Where a torrent is downloaded. Usually the first of these that is set up:
// the download client (qBittorrent and the like), a debrid service, the
// built-in client. An indexer can send its torrents to one of them instead
// (Settings > Indexers & Search, "Torrents go to"), so a private tracker's
// torrents seed in qBittorrent while public ones go to debrid (#166).

// torrentRouteFile remembers in a download's folder where its torrent went, so
// pausing, stopping and resuming it act on the right client even after the
// indexer's choice or the setup changed.
const torrentRouteFile = ".mediarium-torrent-route"

// torrentRouteReady reports whether a route can take a torrent right now.
func (s *Server) torrentRouteReady(route string) bool {
	switch route {
	case indexers.RouteClient:
		return s.downloadClientOn()
	case indexers.RouteDebrid:
		return s.debridOn()
	case indexers.RouteBuiltin:
		return s.torrentsEnabled()
	}
	return false
}

// usualTorrentRoute is the route without an indexer's choice.
func (s *Server) usualTorrentRoute() string {
	switch {
	case s.downloadClientOn():
		return indexers.RouteClient
	case s.debridOn():
		return indexers.RouteDebrid
	}
	return indexers.RouteBuiltin
}

// torrentRouteFor is where a new torrent from the indexer goes: the indexer's
// choice while that is set up, the usual order otherwise.
func (s *Server) torrentRouteFor(indexerID sql.NullInt64) string {
	if indexerID.Valid && s.IndexerRepo != nil {
		if inst, err := s.IndexerRepo.Get(indexerID.Int64); err == nil && inst.TorrentRoute != "" && s.torrentRouteReady(inst.TorrentRoute) {
			return inst.TorrentRoute
		}
	}
	return s.usualTorrentRoute()
}

// mayGoToDebrid reports whether a torrent result could be fetched by the debrid
// service: not when its indexer sends its torrents elsewhere, so a torrent
// cached there is no reason to let a thin one through.
func (s *Server) mayGoToDebrid(res indexers.Result) bool {
	return res.TorrentRoute == "" || res.TorrentRoute == indexers.RouteDebrid || !s.torrentRouteReady(res.TorrentRoute)
}

// itemTorrentRoute is where a download's torrent went (or goes): what its
// folder remembers once it started, what it would take now before that.
func (s *Server) itemTorrentRoute(item queue.Item) string {
	if dir, ok := s.existingWorkDir(item.ID); ok {
		if route := readTorrentRoute(dir); route != "" {
			return route
		}
	}
	return s.torrentRouteFor(item.IndexerID)
}

func readTorrentRoute(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, torrentRouteFile))
	if err != nil {
		return ""
	}
	return indexers.CleanTorrentRoute(strings.TrimSpace(string(b)))
}

func writeTorrentRoute(dir, route string) {
	_ = os.WriteFile(filepath.Join(dir, torrentRouteFile), []byte(route+"\n"), 0o600)
}
