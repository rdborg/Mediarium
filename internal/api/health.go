package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/ryanborg/mediarium/internal/fsinfo"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/organizer"
)

type healthAction struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// healthItem is one thing the user should know about: what is wrong, and —
// in plain words — which features stop working because of it.
type healthItem struct {
	ID     string        `json:"id"`
	Level  string        `json:"level"` // "error" (something is broken), "warn" (worth fixing), "info" (optional)
	Title  string        `json:"title"`
	Impact string        `json:"impact"`
	Action *healthAction `json:"action,omitempty"`
}

var healthRank = map[string]int{"error": 0, "warn": 1, "info": 2}

// collectHealth checks everything Mediarium depends on and reports each
// missing key, service or folder together with what will not work because of
// it. The dashboard shows these, so a skipped setup step is never silent.
func (s *Server) collectHealth() []healthItem {
	var items []healthItem
	add := func(id, level, title, impact, label, path string) {
		it := healthItem{ID: id, Level: level, Title: title, Impact: impact}
		if label != "" {
			it.Action = &healthAction{Label: label, Path: path}
		}
		items = append(items, it)
	}

	if !s.TMDB().HasAPIKey() {
		add("tmdb-key", "error", "TMDB API key is missing",
			"Searching, Discover, adding movies and shows, the calendar and importing an existing library all need TMDB, so none of them will work until it is set.",
			"Set the TMDB key", "/settings/metadata")
	}

	instances, _ := s.IndexerRepo.List()
	torrentsOn := s.torrentsEnabled()
	var usenetIndexers, torrentIndexers int
	for _, inst := range instances {
		if !inst.Enabled {
			continue
		}
		if inst.Protocol == indexers.ProtocolTorrent {
			if torrentsOn { // with torrents off these are never searched
				torrentIndexers++
			}
		} else {
			usenetIndexers++
		}
	}
	if usenetIndexers+torrentIndexers == 0 {
		add("no-indexers", "error", "No indexer is set up",
			"Indexers are the search engines for releases. Without one, Mediarium can't find anything to download, so searching, automatic downloads and upgrades will all come back empty.",
			"Add an indexer", "/settings/indexers")
	}

	servers, _ := s.ClientRepo.List()
	if usenetIndexers > 0 && len(servers) == 0 {
		add("no-usenet-server", "warn", "No Usenet provider added",
			"Your Usenet indexer can find releases, but the built-in downloader has nothing to download them from. Add the news-server account from your Usenet provider and NZB downloads will start working.",
			"Add your provider", "/settings/downloads")
	}

	vpnConnected := s.VPNManager.Status().Connected
	switch {
	case !torrentsOn:
		// Torrents are off: nothing to warn about VPNs or torrent indexers.
	case s.vpnRequiredForTorrents() && !vpnConnected:
		add("vpn-required", "error", "Torrents are blocked: VPN required but not connected",
			"You turned on the VPN kill switch, so torrent downloads won't start until a VPN is connected. Usenet downloads are not affected.",
			"Open VPN settings", "/settings/vpn")
	case torrentIndexers > 0 && !vpnConnected:
		add("no-vpn", "warn", "Torrenting without a VPN",
			"Torrents work, but everyone in the swarm can see your home IP address and your internet provider can see you are torrenting. A VPN hides both. This is only a recommendation; nothing is blocked.",
			"Set up a VPN", "/settings/vpn")
	}

	if !organizer.NewExtractor().Available() {
		add("no-7z", "info", "The 7z tool isn't installed (only needed for .7z archives)",
			"RAR and ZIP archives are unpacked by Mediarium itself. The 7z tool is only used for the rarer .7z format, so downloads packed that way will fail at the last step until it is installed. In the official Docker image it is included; on a native install, install 7-Zip if you ever need it (see the installation guide).",
			"How to install it", "/about")
	}
	if !organizer.NewRepairer().Available() {
		add("no-par2", "warn", "The repair tool (par2) is missing",
			"Usenet downloads with a few missing pieces cannot be repaired without it, so damaged downloads will fail instead of being fixed. In the official Docker image it is included; on a native install, install par2 (see the installation guide).",
			"How to install it", "/about")
	}

	if !s.Subtitles().HasAPIKey() {
		add("subtitles-off", "info", "Subtitle downloads are off",
			"There is no OpenSubtitles key, so no subtitles will be downloaded. Everything else works.",
			"Enable subtitles", "/settings/subtitles")
	} else if s.autoSubtitlesEnabled() && !s.Subtitles().HasCredentials() {
		add("subtitles-anonymous", "info", "No OpenSubtitles account",
			"Subtitles will download, but without a (free) OpenSubtitles.com account you are limited to about 5 downloads per day, so a large library will fill in slowly.",
			"Add your account", "/settings/subtitles")
	}

	if !s.Trakt().HasClientID() {
		add("trakt-off", "info", "Trakt isn't connected (optional)",
			"You can't import Trakt lists on the Discover page. Nothing else depends on it.",
			"Set up Trakt", "/settings/metadata")
	}

	if !s.automationEnabled() {
		add("automation-off", "info", "Automatic searching is turned off",
			"Mediarium won't look for missing episodes or better quality on its own; you'd have to search by hand.",
			"Turn it on", "/settings/quality")
	}

	movies, tv, dl := s.moviesRoot(), s.tvRoot(), s.downloadsRoot()
	for _, f := range []struct {
		id, label, path string
		level           string // severity when the folder is unusable
		impact          string
	}{
		{"movies", "Movies", movies, "error", "Movies can't be imported into the library"},
		{"tv", "TV", tv, "warn", "TV episodes can't be imported into the library"},
		{"downloads", "Downloads", dl, "error", "Nothing can be downloaded"},
	} {
		info := fsinfo.Inspect(f.path)
		switch {
		case !info.Exists || !info.IsDir:
			add(f.id+"-folder-missing", f.level, f.label+" folder not found ("+f.path+")",
				f.impact+" until this folder exists. In Docker, map a folder from your host to this path (see the installation guide).",
				"Fix folders", "/settings/media")
		case !info.Writable:
			add(f.id+"-folder-readonly", f.level, f.label+" folder isn't writable ("+f.path+")",
				f.impact+" because Mediarium can't write there. Check the folder's permissions, or the container's PUID and PGID.",
				"Fix folders", "/settings/media")
		case info.MountKnown && !info.Mounted:
			add(f.id+"-folder-unmapped", "warn", f.label+" folder isn't a mapped folder ("+f.path+")",
				"It's part of the container itself, so anything saved there disappears when the container is updated or recreated. Map a folder from your host to this path.",
				"See folders", "/settings/media")
		default:
			if len(info.Warnings) > 0 {
				add(f.id+"-folder-space", "warn", f.label+" drive is almost full ("+f.path+")",
					"Downloads and imports will start failing when it runs out of space.",
					"See folders", "/settings/media")
			}
		}
	}
	if same, supported, err := organizer.SameFilesystem(dl, movies); err == nil && supported && !same {
		add("no-hardlinks", "info", "Downloads and Movies are on different drives",
			"Each import copies the file instead of hardlinking it, so it briefly uses double the space (the download is cleaned up afterwards). Putting both under one shared folder on the same drive avoids that.",
			"", "")
	}

	if recent := s.recentFailures(24 * time.Hour); recent > 0 {
		add("recent-failures", "warn", fmt.Sprintf("%d download(s) failed in the last 24 hours", recent),
			"Failed grabs are retried automatically when the release itself was bad; otherwise check the reason on the Activity page.",
			"See what failed", "/queue")
	}

	sort.SliceStable(items, func(i, j int) bool { return healthRank[items[i].Level] < healthRank[items[j].Level] })
	return items
}

func todayUTC() string { return time.Now().UTC().Format("2006-01-02") }

func (s *Server) recentFailures(within time.Duration) int {
	list, err := s.QueueRepo.RecentFailed(time.Now().Add(-within))
	if err != nil {
		return 0
	}
	return list
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	items := s.collectHealth()
	if items == nil {
		items = []healthItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
