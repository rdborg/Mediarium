package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/fsinfo"
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
	// Dismiss is the label of a button that hides the item for good (the
	// dashboard sends the matching setting), for a notice that doesn't apply.
	Dismiss string `json:"dismiss,omitempty"`
}

var healthRank = map[string]int{"error": 0, "warn": 1, "info": 2}

// collectHealth checks everything Mediarium depends on and reports each
// missing key, service or folder together with what will not work because of
// it. The dashboard shows these, so a skipped setup step is never silent.
func (s *Server) collectHealth() []healthItem {
	var items []healthItem
	addHealth := func(id, level, title, impact, label, path string) {
		it := healthItem{ID: id, Level: level, Title: title, Impact: impact}
		if label != "" {
			it.Action = &healthAction{Label: label, Path: path}
		}
		items = append(items, it)
	}

	if s.cfg.PauseAutomation {
		addHealth("safe-mode", "warn", "Safe mode is on",
			"Automatic searching, downloading and refreshing are paused because MEDIARIUM_PAUSE_AUTOMATION is set. Remove it and restart to resume.",
			"", "")
	}

	if in := store.Mode(s.db); in.JournalMode != store.ModeWAL {
		addHealth("database-mode", "warn", "Keep the settings folder on a local disk",
			"The database can't use its faster mode, usually because /config is on a network drive. Pages may be slow during downloads. Move /config to a local disk.",
			"", "")
	} else if kind, ok := fsinfo.NetworkFilesystem(s.cfg.ConfigDir); ok {
		// The faster mode can switch on over a network share and still be slow
		// there, and a share that drops for a moment can damage the database.
		addHealth("database-network", "warn", "Keep the settings folder on a local disk",
			fmt.Sprintf("The settings folder (/config) is on a network share (%s). The database is slow there and can be damaged when the share drops for a moment. Move /config to a local disk; downloads and your library can stay on the share.", kind),
			"", "")
	}

	if in := store.Mode(s.db); len(in.NewerMigrations) > 0 {
		addHealth("database-newer", "warn", "This version is older than your database",
			store.NewerMessage(in.NewerMigrations),
			"", "")
	}

	if !s.TMDB().HasAPIKey() {
		addHealth("tmdb-key", "error", "TMDB API key is missing",
			"Search, Discover, adding titles, the calendar and library import all need it.",
			"Set the TMDB key", "/settings/metadata")
	}

	keyMismatch := CheckSecretKey(s.db, s.box) != nil
	if keyMismatch {
		addHealth("secret-key-mismatch", "error", "The saved keys can't be read",
			"The secret.key in your settings folder isn't the one this database was saved with, so indexer keys, passwords and tokens can't be read and automatic searching can't work. Put back the secret.key that belongs to this database, or re-enter your keys.",
			"", "")
	}

	// With the wrong key every secret is unreadable, which the item above says
	// once; otherwise each one that can't be read is named.
	if !keyMismatch {
		items = append(items, s.unreadableSecretItems()...)
	}

	instances, listErr := s.IndexerRepo.List()
	torrentsOn := s.torrentReleasesOn()
	var usenetIndexers, torrentIndexers, unreadable int
	for _, inst := range instances {
		if inst.SecretError != "" {
			unreadable++
			addHealth(fmt.Sprintf("indexer-key-unreadable-%d", inst.ID), "error", inst.Name+" has a key that can't be read",
				"Mediarium can't read the saved key of "+inst.Name+", so it is left out of searches. Open it and enter the key again.",
				"Check indexers", "/settings/indexers")
			continue
		}
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
	if listErr != nil {
		addHealth("indexers-unreadable", "error", "The list of indexers can't be read",
			"Mediarium couldn't read the indexers from the database, so searches and automatic downloads find nothing. The log says why.",
			"", "")
	} else if unreadable == 0 && !keyMismatch {
		indexerSetupHealth(instances, torrentsOn, time.Now(), addHealth)
	}

	for _, inst := range instances {
		if inst.Enabled && inst.IsCardigann() && inst.LastTestError != "" {
			addHealth(fmt.Sprintf("indexer-test-failed-%d", inst.ID), "info", fmt.Sprintf("%s failed its last test", inst.Name),
				fmt.Sprintf("The last test said: %s. Searches may find nothing here until it's fixed.", strings.TrimRight(inst.LastTestError, ". ")),
				"Check indexers", "/settings/indexers")
		}
	}

	s.pausedIndexerProblems(instances, addHealth)

	if s.cfg.BundledFlareSolverr && s.flareSolverrSetting() == "" {
		if st := s.flareSolverrCheck(); !st.Running {
			addHealth("flaresolverr-down", "warn", "The built-in Cloudflare helper isn't answering",
				"Sites with a Cloudflare check will fail until it's back. If it stays down, restart the container.",
				"Open indexers", "/settings/indexers")
		}
	}

	servers, _ := s.ClientRepo.ListAll() // a server with an unreadable password is named on its own Health item
	if usenetIndexers > 0 && len(servers) == 0 && !s.usenetClientOn() {
		addHealth("no-usenet-server", "warn", "No Usenet provider added",
			"Your Usenet indexer finds releases but there's nowhere to download them from. Add your provider's news server.",
			"Add your provider", "/settings/usenet")
	}

	// A provider that keeps saying "too many connections" holds Mediarium to
	// fewer than were set: say so, and where to find the number that works.
	for _, st := range download.ServerStates() {
		if st.Limit < st.Configured && !st.LastRefusal.IsZero() && time.Since(st.LastRefusal) < 12*time.Hour {
			addHealth("usenet-connections-"+st.Host, "warn", fmt.Sprintf("%s allows fewer connections than you set", st.Host),
				fmt.Sprintf("Mediarium is using %d of the %d connections you set, because the provider keeps saying this login has too many open, so downloads are slower than they could be. Another program may be using the same login. Use Find the right number on your provider to get the number that works.", st.Limit, st.Configured),
				"Find the right number", "/settings/usenet")
		}
	}

	vpnStatus := s.VPNManager.Status()
	vpnConnected := vpnStatus.Connected
	routeIndexers := s.vpnForIndexers()
	if routeIndexers && vpnStatus.State == vpn.StateOff {
		addHealth("vpn-indexers-none", "error", "Indexer searches are blocked",
			"They're set to go through the VPN, but no VPN is switched on. Switch one on, or turn off Send indexer traffic through the VPN.",
			"Open VPN settings", "/settings/vpn")
	}
	switch {
	case !torrentsOn && !routeIndexers:
		// Nothing uses the VPN: nothing to warn about VPNs or torrent indexers.
	case vpnStatus.State == vpn.StateDown:
		addHealth("vpn-down", "error", "Your VPN isn't connected",
			fmt.Sprintf("%s isn't working. %s %s", vpnStatus.Label, vpnStatus.Reason, vpnDownImpact(torrentsOn, routeIndexers)),
			"Open VPN settings", "/settings/vpn")
	case vpnStatus.State == vpn.StateConnecting:
		// Coming up (after a restart, for example): nothing to report yet.
	case !torrentsOn:
		// The rest is about torrents.
	case s.vpnRequiredForTorrents() && !vpnConnected:
		addHealth("vpn-required", "error", "Torrents are blocked until a VPN is connected",
			"The VPN kill switch is on, so torrents wait for a VPN. Usenet isn't affected.",
			"Open VPN settings", "/settings/vpn")
	case torrentIndexers > 0 && !vpnConnected && s.torrentsEnabled() && !s.vpnNoticeAcknowledged():
		// Without the built-in client, torrents run in another program (or a
		// debrid service) and this notice is not Mediarium's to give.
		addHealth("no-vpn", "warn", "Torrenting without a VPN",
			"Other peers can see your home IP address and your internet provider can see you're torrenting. A VPN hides both. Nothing is blocked.",
			"Set up a VPN", "/settings/vpn")
		items[len(items)-1].Dismiss = "My download client is already behind a VPN"
	}

	if !organizer.NewExtractor().Available() {
		addHealth("no-7z", "info", "The 7z tool isn't installed",
			"Only .7z downloads need it, and those will fail until it's installed. The Docker image includes it. On a native install, install 7-Zip.",
			"How to install it", "/about")
	}
	if !organizer.NewRepairer().Available() {
		addHealth("no-par2", "warn", "The repair tool (par2) is missing",
			"Without it, Usenet downloads with missing pieces fail instead of being repaired. The Docker image includes it. On a native install, install par2.",
			"How to install it", "/about")
	}

	// While subtitles are switched off in Settings, nothing here mentions them.
	if s.subtitlesEnabled() {
		src := s.subtitleSources()
		switch {
		case !src.AnySetUp():
			addHealth("subtitles-off", "info", "Subtitles need a source",
				"Subtitles are on but no subtitle source is set up (an OpenSubtitles or SubDL key, or an add-on), so none are downloaded.",
				"Add a key", "/settings/subtitles")
		case !src.Any():
			addHealth("subtitles-no-source", "info", "No subtitle source is switched on",
				"Every subtitle source is switched off, so no subtitles are downloaded.",
				"Choose sources", "/settings/subtitles")
		case s.autoSubtitlesEnabled():
			// Only worth mentioning when Mediarium fetches on its own: then the
			// small daily limit decides how fast a large library fills in. That
			// limit is OpenSubtitles', so it matters only while OpenSubtitles is
			// the one source doing the work.
			if src.OpenSubtitles.On() && !src.OthersThanOpenSubtitles() && !s.Subtitles().HasCredentials() {
				addHealth("subtitles-anonymous", "info", "No OpenSubtitles account",
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
				addHealth("subtitles-offer", "info", title, subtitleOfferImpact(src, s.subtitleQuota(), files), "Get subtitles", "/wanted?tab=subtitles")
			}
		}
	}

	// The shared keys that ship with the app have limits. When one is hit, say
	// so, and point at the free personal key that removes the limit.
	if s.usingSharedKey(serviceTrakt) && s.Usage.Stats(serviceTrakt).LimitHits >= 1 {
		addHealth("trakt-limit", "warn", "The shared Trakt key is at its limit",
			"Trakt refused requests because the built-in key is shared and busy. Importing Trakt lists fails until it clears. A free personal Trakt key avoids this.",
			"Use my own key", "/settings/lists")
	}
	if s.usingSharedKey(serviceSimkl) && s.Usage.Stats(serviceSimkl).LimitHits >= 1 {
		addHealth("simkl-limit", "warn", "The shared Simkl key is at its limit",
			"Simkl refused requests because the built-in key is shared and busy. Reading Simkl lists fails until it clears. A free personal Simkl key avoids this.",
			"Use my own key", "/settings/lists")
	}
	if s.subtitlesEnabled() && s.useOpenSubtitles() && s.usingSharedKey(serviceOpenSubtitles) {
		if s.Usage.Stats(serviceOpenSubtitles).LimitHits >= 1 {
			addHealth("opensubtitles-limit", "warn", "The shared OpenSubtitles key is at its limit",
				"OpenSubtitles refused requests because the built-in key is shared and busy. Subtitle downloads fail until it clears. A free personal key avoids this.",
				"Use my own key", "/settings/subtitles")
		} else if st := s.subtitleQuota(); st.Source == subtitles.QuotaReported && st.Limit > 0 && st.Used*100 > 80*st.Limit {
			addHealth("opensubtitles-busy", "info", "The shared OpenSubtitles key is nearly used up for today",
				fmt.Sprintf("%d of %d subtitle downloads used today. After that they wait until tomorrow. A free personal OpenSubtitles key gives you your own allowance.", st.Used, st.Limit),
				"Use my own key", "/settings/subtitles")
		}
	}

	// Simkl lists need the account connected; say so only when a list is waiting for it.
	var simklLists int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM import_lists WHERE source = ? AND enabled = 1`, listSimkl).Scan(&simklLists)
	if st := s.simklState(); simklLists > 0 && (!st.Connected || !st.HasClientID) && !st.Unreadable {
		addHealth("simkl-off", "warn", "Simkl isn't connected",
			"A Simkl list is set up but no Simkl account is connected, so it can't be read. Connect your account to start it again.",
			"Connect Simkl", "/settings/lists")
	}

	if !s.Trakt().HasClientID() {
		addHealth("trakt-off", "info", "Trakt isn't connected (optional)",
			"Needed only for importing Trakt lists on Discover.",
			"Set up Trakt", "/settings/lists")
	}

	if !s.automationEnabled() {
		addHealth("automation-off", "info", "Automatic searching is turned off",
			"Missing episodes and better quality are only found when you search by hand.",
			"Turn it on", "/settings/quality")
	}

	movies, tv, dl := s.moviesRoot(), s.tvRoot(), s.downloadsRoot()
	type folderCheck struct {
		id, label, path string
		level           string // severity when the folder is unusable
		// What stops working when the folder is not there, and when Mediarium
		// cannot write to it: whole sentences, so they can be translated.
		missing, readonly string
	}
	const (
		moviesMissing     = "Movies can't be imported into the library until this folder exists. In Docker, map a host folder to this path."
		moviesReadonly    = "Movies can't be imported into the library because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
		tvMissing         = "TV episodes can't be imported into the library until this folder exists. In Docker, map a host folder to this path."
		tvReadonly        = "TV episodes can't be imported into the library because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
		downloadMissing   = "Nothing can be downloaded until this folder exists. In Docker, map a host folder to this path."
		downloadReadonly  = "Nothing can be downloaded because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
		moreMovieMissing  = "Movies kept in that folder can't be imported until this folder exists. In Docker, map a host folder to this path."
		moreMovieReadonly = "Movies kept in that folder can't be imported because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
		moreTVMissing     = "Shows kept in that folder can't be imported until this folder exists. In Docker, map a host folder to this path."
		moreTVReadonly    = "Shows kept in that folder can't be imported because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
		movieCopyMissing  = "Second copies of movies can't be imported until this folder exists. In Docker, map a host folder to this path."
		movieCopyReadonly = "Second copies of movies can't be imported because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
		tvCopyMissing     = "Second copies of shows can't be imported until this folder exists. In Docker, map a host folder to this path."
		tvCopyReadonly    = "Second copies of shows can't be imported because Mediarium can't write there. Check the folder's permissions, or the PUID and PGID."
	)
	folders := []folderCheck{
		{"movies", "Movies", movies, "error", moviesMissing, moviesReadonly},
		{"tv", "TV", tv, "warn", tvMissing, tvReadonly},
		{"downloads", "Downloads", dl, "error", downloadMissing, downloadReadonly},
	}
	for i, f := range s.movieFolders()[min(1, len(s.movieFolders())):] {
		folders = append(folders, folderCheck{"movies-" + strconv.Itoa(i+2), "Movies folder " + strconv.Itoa(i+2), f, "warn", moreMovieMissing, moreMovieReadonly})
	}
	for i, f := range s.tvFolders()[min(1, len(s.tvFolders())):] {
		folders = append(folders, folderCheck{"tv-" + strconv.Itoa(i+2), "TV folder " + strconv.Itoa(i+2), f, "warn", moreTVMissing, moreTVReadonly})
	}
	if c := s.movieCopiesRoot(); c != "" {
		folders = append(folders, folderCheck{"movie-copies", "Movie copies", c, "warn", movieCopyMissing, movieCopyReadonly})
	}
	if c := s.tvCopiesRoot(); c != "" {
		folders = append(folders, folderCheck{"tv-copies", "TV copies", c, "warn", tvCopyMissing, tvCopyReadonly})
	}
	for _, f := range folders {
		info := inspectCached(f.path)
		switch {
		case !info.Exists || !info.IsDir:
			addHealth(f.id+"-folder-missing", f.level, fmt.Sprintf("%s folder not found (%s)", f.label, f.path),
				f.missing,
				"Fix folders", "/settings/media")
		case !info.Writable:
			addHealth(f.id+"-folder-readonly", f.level, fmt.Sprintf("%s folder isn't writable (%s)", f.label, f.path),
				f.readonly,
				"Fix folders", "/settings/media")
		case info.MountKnown && !info.Mounted:
			addHealth(f.id+"-folder-unmapped", "warn", fmt.Sprintf("%s folder isn't a mapped folder (%s)", f.label, f.path),
				"Anything saved there is lost when the container is updated. Map a host folder to this path.",
				"See folders", "/settings/media")
		default:
			if len(info.Warnings) > 0 {
				addHealth(f.id+"-folder-space", "warn", fmt.Sprintf("%s drive is almost full (%s)", f.label, f.path),
					"Downloads and imports fail when it runs out of space.",
					"See folders", "/settings/media")
			}
		}
	}
	// A library folder that has lost its marker file is most likely a drive that
	// isn't connected. Imports into it wait, so nothing lands on the machine's own disk.
	for i, root := range s.writableRoots() {
		if p := s.libraryRootProblem(root); p != nil && p.gone {
			addHealth("library-root-gone-"+strconv.Itoa(i), "error", fmt.Sprintf("The library folder %s looks disconnected", root),
				fmt.Sprintf("It has lost its %s file, which usually means its drive isn't connected. Nothing is imported or moved into it until it's back, so no files end up on the wrong disk. If the folder is the right one, add an empty file with that name to it.", fsinfo.RootMarker),
				"See folders", "/settings/media")
		}
	}
	// One whole heading per folder, so the screen can show it in the
	// reader's language.
	if movies != "" {
		if bad, _ := unwritableInside(movies); len(bad) > 0 {
			addHealth("movies-folders-inside", "warn", "Some folders inside the movies folder can't be written to", insideWarning(bad), "See folders", "/settings/media")
		}
	}
	if tv != "" {
		if bad, _ := unwritableInside(tv); len(bad) > 0 {
			addHealth("tv-folders-inside", "warn", "Some folders inside the TV folder can't be written to", insideWarning(bad), "See folders", "/settings/media")
		}
	}
	if same, supported, err := organizer.SameFilesystem(dl, movies); err == nil && supported && !same {
		addHealth("no-hardlinks", "info", "Downloads and Movies are on different drives",
			"Imports copy files instead of hardlinking them, which briefly uses double the space. Put both on the same drive to avoid that.",
			"", "")
	}

	if recent := s.recentFailures(24 * time.Hour); recent > 0 {
		addHealth("recent-failures", "warn", recentFailuresTitle(recent),
			"Bad releases are retried automatically. For anything else, check the reason on Activity.",
			"See what failed", "/queue")
	}

	items = append(items, s.mediaServerHealth()...)

	sort.SliceStable(items, func(i, j int) bool { return healthRank[items[i].Level] < healthRank[items[j].Level] })
	return items
}

// recentFailuresTitle is the title of the "downloads failed" line, one whole
// sentence for one download and one for several.
func recentFailuresTitle(n int) string {
	if n == 1 {
		return "1 download failed in the last 24 hours"
	}
	return fmt.Sprintf("%d downloads failed in the last 24 hours", n)
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
