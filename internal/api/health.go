package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/store"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/vpn"
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

	if s.cfg.PauseAutomation {
		add("safe-mode", "warn", "Safe mode is on",
			"Automatic searching, downloading and refreshing are paused because MEDIARIUM_PAUSE_AUTOMATION is set. Remove it and restart to resume.",
			"", "")
	}

	if in := store.Mode(s.db); in.JournalMode != store.ModeWAL {
		add("database-mode", "warn", "Keep the settings folder on a local disk",
			"The database can't use its faster mode, usually because /config is on a network drive. Pages may be slow during downloads. Move /config to a local disk.",
			"", "")
	}

	if !s.TMDB().HasAPIKey() {
		add("tmdb-key", "error", "TMDB API key is missing",
			"Search, Discover, adding titles, the calendar and library import all need it.",
			"Set the TMDB key", "/settings/metadata")
	}

	instances, _ := s.IndexerRepo.List()
	torrentsOn := s.torrentReleasesOn()
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
			"Without one, searches and automatic downloads find nothing.",
			"Add an indexer", "/settings/indexers")
	}

	for _, inst := range instances {
		if inst.Enabled && inst.IsCardigann() && inst.LastTestError != "" {
			add(fmt.Sprintf("indexer-test-failed-%d", inst.ID), "info", inst.Name+" failed its last test",
				"The last test said: "+strings.TrimRight(inst.LastTestError, ". ")+". Searches may find nothing here until it's fixed.",
				"Check indexers", "/settings/indexers")
		}
	}

	s.pausedIndexerProblems(instances, add)

	if s.cfg.BundledFlareSolverr && s.flareSolverrSetting() == "" {
		if st := s.flareSolverrCheck(); !st.Running {
			add("flaresolverr-down", "warn", "The built-in Cloudflare helper isn't answering",
				"Sites with a Cloudflare check will fail until it's back. If it stays down, restart the container.",
				"Open indexers", "/settings/indexers")
		}
	}

	servers, _ := s.ClientRepo.List()
	if usenetIndexers > 0 && len(servers) == 0 {
		add("no-usenet-server", "warn", "No Usenet provider added",
			"Your Usenet indexer finds releases but there's nowhere to download them from. Add your provider's news server.",
			"Add your provider", "/settings/usenet")
	}

	// A provider that keeps saying "too many connections" holds Mediarium to
	// fewer than were set: say so, and where to find the number that works.
	for _, st := range download.ServerStates() {
		if st.Limit < st.Configured && !st.LastRefusal.IsZero() && time.Since(st.LastRefusal) < 12*time.Hour {
			add("usenet-connections-"+st.Host, "warn", fmt.Sprintf("%s allows fewer connections than you set", st.Host),
				fmt.Sprintf("Mediarium is using %d of the %d connections you set, because the provider keeps saying this login has too many open, so downloads are slower than they could be. Another program may be using the same login. Use Find the right number on your provider to get the number that works.", st.Limit, st.Configured),
				"Find the right number", "/settings/usenet")
		}
	}

	vpnStatus := s.VPNManager.Status()
	vpnConnected := vpnStatus.Connected
	routeIndexers := s.vpnForIndexers()
	if routeIndexers && vpnStatus.State == vpn.StateOff {
		add("vpn-indexers-none", "error", "Indexer searches are blocked",
			"They're set to go through the VPN, but no VPN is switched on. Switch one on, or turn off Send indexer traffic through the VPN.",
			"Open VPN settings", "/settings/vpn")
	}
	switch {
	case !torrentsOn && !routeIndexers:
		// Nothing uses the VPN: nothing to warn about VPNs or torrent indexers.
	case vpnStatus.State == vpn.StateDown:
		add("vpn-down", "error", "Your VPN isn't connected",
			vpnStatus.Label+" isn't working. "+vpnStatus.Reason+" "+vpnDownImpact(torrentsOn, routeIndexers),
			"Open VPN settings", "/settings/vpn")
	case vpnStatus.State == vpn.StateConnecting:
		// Coming up (after a restart, for example): nothing to report yet.
	case !torrentsOn:
		// The rest is about torrents.
	case s.vpnRequiredForTorrents() && !vpnConnected:
		add("vpn-required", "error", "Torrents are blocked until a VPN is connected",
			"The VPN kill switch is on, so torrents wait for a VPN. Usenet isn't affected.",
			"Open VPN settings", "/settings/vpn")
	case torrentIndexers > 0 && !vpnConnected:
		add("no-vpn", "warn", "Torrenting without a VPN",
			"Other peers can see your home IP address and your internet provider can see you're torrenting. A VPN hides both. Nothing is blocked.",
			"Set up a VPN", "/settings/vpn")
	}

	if !organizer.NewExtractor().Available() {
		add("no-7z", "info", "The 7z tool isn't installed",
			"Only .7z downloads need it, and those will fail until it's installed. The Docker image includes it. On a native install, install 7-Zip.",
			"How to install it", "/about")
	}
	if !organizer.NewRepairer().Available() {
		add("no-par2", "warn", "The repair tool (par2) is missing",
			"Without it, Usenet downloads with missing pieces fail instead of being repaired. The Docker image includes it. On a native install, install par2.",
			"How to install it", "/about")
	}

	// While subtitles are switched off in Settings, nothing here mentions them.
	if s.subtitlesEnabled() {
		src := s.subtitleSources()
		switch {
		case !src.AnySetUp():
			add("subtitles-off", "info", "Subtitles need a source",
				"Subtitles are on but no subtitle source is set up (an OpenSubtitles or SubDL key, or an add-on), so none are downloaded.",
				"Add a key", "/settings/subtitles")
		case !src.Any():
			add("subtitles-no-source", "info", "No subtitle source is switched on",
				"Every subtitle source is switched off, so no subtitles are downloaded.",
				"Choose sources", "/settings/subtitles")
		case s.autoSubtitlesEnabled():
			// Only worth mentioning when Mediarium fetches on its own: then the
			// small daily limit decides how fast a large library fills in. That
			// limit is OpenSubtitles', so it matters only while OpenSubtitles is
			// the one source doing the work.
			if src.OpenSubtitles.On() && !src.OthersThanOpenSubtitles() && !s.Subtitles().HasCredentials() {
				add("subtitles-anonymous", "info", "No OpenSubtitles account",
					"Without a free OpenSubtitles.com account you get about 5 a day (about 20 with one), so a large library fills in slowly.",
					"Add your account", "/settings/subtitles")
			}
		default:
			if titles, files, err := s.subtitleBacklog(); err == nil && titles > 0 {
				// Automatic downloading is off, so nothing is fetched until asked.
				title := fmt.Sprintf("%d downloaded titles have no subtitles yet", titles)
				if titles == 1 {
					title = "1 downloaded title has no subtitles yet"
				}
				add("subtitles-offer", "info", title, subtitleOfferImpact(src, s.subtitleQuota(), files), "Get subtitles", "/wanted?tab=subtitles")
			}
		}
	}

	// The shared keys that ship with the app have limits. When one is hit, say
	// so, and point at the free personal key that removes the limit.
	if s.usingSharedKey(serviceTrakt) && s.Usage.Stats(serviceTrakt).LimitHits >= 1 {
		add("trakt-limit", "warn", "The shared Trakt key is at its limit",
			"Trakt refused requests because the built-in key is shared and busy. Importing Trakt lists fails until it clears. A free personal Trakt key avoids this.",
			"Use my own key", "/settings/lists")
	}
	if s.subtitlesEnabled() && s.useOpenSubtitles() && s.usingSharedKey(serviceOpenSubtitles) {
		if s.Usage.Stats(serviceOpenSubtitles).LimitHits >= 1 {
			add("opensubtitles-limit", "warn", "The shared OpenSubtitles key is at its limit",
				"OpenSubtitles refused requests because the built-in key is shared and busy. Subtitle downloads fail until it clears. A free personal key avoids this.",
				"Use my own key", "/settings/subtitles")
		} else if st := s.subtitleQuota(); st.Source == subtitles.QuotaReported && st.Limit > 0 && st.Used*100 > 80*st.Limit {
			add("opensubtitles-busy", "info", "The shared OpenSubtitles key is nearly used up for today",
				fmt.Sprintf("%d of %d subtitle downloads used today. After that they wait until tomorrow. A free personal OpenSubtitles key gives you your own allowance.", st.Used, st.Limit),
				"Use my own key", "/settings/subtitles")
		}
	}

	if !s.Trakt().HasClientID() {
		add("trakt-off", "info", "Trakt isn't connected (optional)",
			"Needed only for importing Trakt lists on Discover.",
			"Set up Trakt", "/settings/lists")
	}

	if !s.automationEnabled() {
		add("automation-off", "info", "Automatic searching is turned off",
			"Missing episodes and better quality are only found when you search by hand.",
			"Turn it on", "/settings/quality")
	}

	movies, tv, dl := s.moviesRoot(), s.tvRoot(), s.downloadsRoot()
	type folderCheck struct {
		id, label, path string
		level           string // severity when the folder is unusable
		impact          string
	}
	folders := []folderCheck{
		{"movies", "Movies", movies, "error", "Movies can't be imported into the library"},
		{"tv", "TV", tv, "warn", "TV episodes can't be imported into the library"},
		{"downloads", "Downloads", dl, "error", "Nothing can be downloaded"},
	}
	for i, f := range s.movieFolders()[min(1, len(s.movieFolders())):] {
		folders = append(folders, folderCheck{"movies-" + strconv.Itoa(i+2), "Movies folder " + strconv.Itoa(i+2), f, "warn", "Movies kept in that folder can't be imported"})
	}
	for i, f := range s.tvFolders()[min(1, len(s.tvFolders())):] {
		folders = append(folders, folderCheck{"tv-" + strconv.Itoa(i+2), "TV folder " + strconv.Itoa(i+2), f, "warn", "Shows kept in that folder can't be imported"})
	}
	if c := s.movieCopiesRoot(); c != "" {
		folders = append(folders, folderCheck{"movie-copies", "Movie copies", c, "warn", "Second copies of movies can't be imported"})
	}
	if c := s.tvCopiesRoot(); c != "" {
		folders = append(folders, folderCheck{"tv-copies", "TV copies", c, "warn", "Second copies of shows can't be imported"})
	}
	for _, f := range folders {
		info := inspectCached(f.path)
		switch {
		case !info.Exists || !info.IsDir:
			add(f.id+"-folder-missing", f.level, f.label+" folder not found ("+f.path+")",
				f.impact+" until this folder exists. In Docker, map a host folder to this path.",
				"Fix folders", "/settings/media")
		case !info.Writable:
			add(f.id+"-folder-readonly", f.level, f.label+" folder isn't writable ("+f.path+")",
				f.impact+" because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID.",
				"Fix folders", "/settings/media")
		case info.MountKnown && !info.Mounted:
			add(f.id+"-folder-unmapped", "warn", f.label+" folder isn't a mapped folder ("+f.path+")",
				"Anything saved there is lost when the container is updated. Map a host folder to this path.",
				"See folders", "/settings/media")
		default:
			if len(info.Warnings) > 0 {
				add(f.id+"-folder-space", "warn", f.label+" drive is almost full ("+f.path+")",
					"Downloads and imports fail when it runs out of space.",
					"See folders", "/settings/media")
			}
		}
	}
	for _, f := range []struct{ id, label, path string }{{"movies", "Movies", movies}, {"tv", "TV", tv}} {
		if f.path == "" {
			continue
		}
		if bad, _ := unwritableInside(f.path); len(bad) > 0 {
			add(f.id+"-folders-inside", "warn", "Some folders inside "+f.label+" can't be written to",
				insideWarning(bad), "See folders", "/settings/media")
		}
	}
	if same, supported, err := organizer.SameFilesystem(dl, movies); err == nil && supported && !same {
		add("no-hardlinks", "info", "Downloads and Movies are on different drives",
			"Imports copy files instead of hardlinking them, which briefly uses double the space. Put both on the same drive to avoid that.",
			"", "")
	}

	if recent := s.recentFailures(24 * time.Hour); recent > 0 {
		add("recent-failures", "warn", plural(recent, "download")+" failed in the last 24 hours",
			"Bad releases are retried automatically. For anything else, check the reason on Activity.",
			"See what failed", "/queue")
	}

	items = append(items, s.mediaServerHealth()...)

	sort.SliceStable(items, func(i, j int) bool { return healthRank[items[i].Level] < healthRank[items[j].Level] })
	return items
}

// vpnDownImpact says what waits while the VPN is down, and how to go on
// without it.
func vpnDownImpact(torrents, indexers bool) string {
	switch {
	case torrents && indexers:
		return "Torrents and indexer searches wait until it's back. To go on without it, press Disconnect and turn off Send indexer traffic through the VPN."
	case indexers:
		return "Indexer searches wait until it's back. To search without it, turn off Send indexer traffic through the VPN."
	default:
		return "Torrents wait until it's back. Press Disconnect in the VPN settings to download without it."
	}
}

func todayUTC() string { return time.Now().UTC().Format("2006-01-02") }

func (s *Server) recentFailures(within time.Duration) int {
	list, err := s.QueueRepo.RecentFailed(time.Now().Add(-within))
	if err != nil {
		return 0
	}
	return list
}

// handleHealth lists setup problems for administrators. Members cannot fix
// any of them (they are all in Settings), so they get an empty list.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var items []healthItem
	if isAdminRequest(r) {
		items = s.collectHealth()
	}
	if items == nil {
		items = []healthItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
