// Package problems keeps the log of real problems shown in Settings > System >
// Logs and errors: a download that failed, a Usenet provider that refused the
// login, a full disk. It is not a copy of the log file. Only things a person
// may want to act on are recorded, each under a stable code, and the same
// problem repeating within a few minutes is folded into one row with a count.
//
// The package sits at the bottom of the dependency order (it imports only the
// log scrubber), so any part of the app can report a problem with Record
// without importing the API. Nothing is recorded until the API has attached
// the database with SetDefault, so tests of other packages are unaffected.
package problems

// Level says how serious a problem is.
type Level string

const (
	LevelError   Level = "error"   // something failed and did not fix itself
	LevelWarning Level = "warning" // something went wrong but the app worked around it, or it may pass
)

// Area is the part of the app a problem belongs to.
type Area string

const (
	AreaDownloads     Area = "downloads"
	AreaUsenet        Area = "usenet"
	AreaSearch        Area = "search"
	AreaMediaServers  Area = "mediaservers"
	AreaImport        Area = "import"
	AreaDisk          Area = "disk"
	AreaDatabase      Area = "database"
	AreaMetadata      Area = "metadata"
	AreaUpdates       Area = "updates"
	AreaNotifications Area = "notifications"
	AreaSystem        Area = "system"
)

// AreaInfo is an area with the name shown for it.
type AreaInfo struct {
	ID    Area   `json:"id"`
	Label string `json:"label"`
}

// Areas lists every area in display order.
func Areas() []AreaInfo {
	return []AreaInfo{
		{AreaDownloads, "Downloads"},
		{AreaUsenet, "Usenet"},
		{AreaSearch, "Indexers & Search"},
		{AreaMediaServers, "Media servers"},
		{AreaImport, "Import"},
		{AreaDisk, "Disk and folders"},
		{AreaDatabase, "Database"},
		{AreaMetadata, "Movie and show info"},
		{AreaUpdates, "Updates"},
		{AreaNotifications, "Notifications"},
		{AreaSystem, "System"},
	}
}

// AreaLabel is the name shown for an area; an unknown area shows as it is.
func AreaLabel(a Area) string {
	for _, x := range Areas() {
		if x.ID == a {
			return x.Label
		}
	}
	return string(a)
}

// The codes. A code never changes once it has shipped, because the log stores
// it and the help below is looked up by it.
const (
	CodeDownloadFailed       = "download.failed"
	CodeDownloadFileFailed   = "download.file_failed" // no longer produced (see CodeIndexerFetchFailed); older log rows still use it
	CodeUsenetTooMany        = "usenet.too_many_connections"
	CodeUsenetAuthRefused    = "usenet.auth_refused"
	CodeUsenetUnreachable    = "usenet.unreachable"
	CodeUsenetQuota          = "usenet.quota_reached"
	CodeUsenetMissingParts   = "usenet.articles_missing"
	CodeUnpackFailed         = "unpack.failed"
	CodeUnpackPassword       = "unpack.password"
	CodeUnpackToolMissing    = "unpack.tool_missing"
	CodePar2Failed           = "par2.failed"
	CodeIndexerRateLimited   = "indexer.rate_limited"
	CodeIndexerAuthRefused   = "indexer.auth_refused"
	CodeIndexerUnreachable   = "indexer.unreachable"
	CodeIndexerFailed        = "indexer.failed"
	CodeIndexerFetchFailed   = "indexer.fetch_failed"
	CodeMediaServerDown      = "mediaserver.unreachable"
	CodeMediaServerAuth      = "mediaserver.auth_refused"
	CodeMediaServerRefresh   = "mediaserver.refresh_failed"
	CodeImportMoveFailed     = "import.move_failed"
	CodeImportNoVideo        = "import.no_video"
	CodeImportFileExists     = "import.file_exists"
	CodeImportFailed         = "import.failed"
	CodeDiskFull             = "disk.full"
	CodeFolderPermission     = "folder.permission_denied"
	CodeFolderMissing        = "folder.missing"
	CodeDatabaseSlow         = "database.slow"
	CodeTMDBRateLimited      = "tmdb.rate_limited"
	CodeTMDBKeyRejected      = "tmdb.key_rejected"
	CodeNotificationFailed   = "notification.send_failed"
	CodeUpdateCheckFailed    = "update.check_failed"
	CodeUpdateInstallFailed  = "update.install_failed"
	CodeAppRestartedItself   = "app.restarted_itself"
	CodeSubtitlesRateLimited = "subtitles.rate_limited"
)

// Help is the plain-language help for one code.
type Help struct {
	Code      string `json:"code"`
	Level     Level  `json:"level"` // used when the caller does not say
	Area      Area   `json:"area"`
	Title     string `json:"title"`   // a short heading
	Explain   string `json:"explain"` // what happened, in a sentence
	Try       string `json:"try"`     // what to try, in a sentence or two
	LinkLabel string `json:"linkLabel,omitempty"`
	LinkPath  string `json:"linkPath,omitempty"` // the settings page where it can be fixed
}

const (
	pathDownloads    = "/settings/downloads"
	pathIndexers     = "/settings/indexers"
	pathMediaServers = "/settings/media-servers"
	pathMedia        = "/settings/media"
	pathMetadata     = "/settings/metadata"
	pathNotify       = "/settings/notifications"
	pathSystem       = "/settings/system"
	pathSubtitles    = "/settings/subtitles"
)

// catalog is the one table of every code and what to tell a person about it.
// Keep the wording short and plain: this is what a worried person reads.
var catalog = []Help{
	{CodeDownloadFailed, LevelError, AreaDownloads,
		"A download failed",
		"The download started but did not finish.",
		"Read the message on Activity and try again. If it keeps failing, copy the report for support.",
		"Open Activity", "/queue"},
	{CodeDownloadFileFailed, LevelError, AreaDownloads,
		"Could not get the download file",
		"The indexer didn't hand over the file.",
		"Test the indexer in Settings. If it says its limit is reached, try again later.",
		"Open indexers", pathIndexers},
	{CodeUsenetTooMany, LevelWarning, AreaUsenet,
		"Too many connections to your Usenet provider",
		"Your provider says this login has too many connections open.",
		"Stop other programs (like SABnzbd) using the account, or lower the connections in Settings > Downloading > Usenet and torrents.",
		"Open Usenet settings", pathDownloads},
	{CodeUsenetAuthRefused, LevelError, AreaUsenet,
		"Your Usenet provider refused the login",
		"The username and password weren't accepted, or the subscription has ended.",
		"Check the login in Settings > Downloading > Usenet and torrents, and that your subscription is active.",
		"Open Usenet settings", pathDownloads},
	{CodeUsenetUnreachable, LevelError, AreaUsenet,
		"Can't reach your Usenet provider",
		"Mediarium couldn't connect to the news server.",
		"Check the server name and port, then press Test.",
		"Open Usenet settings", pathDownloads},
	{CodeUsenetQuota, LevelError, AreaUsenet,
		"Usenet limit reached",
		"The download limit for your plan is used up, or the account has expired.",
		"Wait for the limit to reset, or change your plan with the provider.",
		"Open Usenet settings", pathDownloads},
	{CodeUsenetMissingParts, LevelError, AreaUsenet,
		"Parts of a download are missing",
		"Your Usenet servers no longer have every piece of this release.",
		"Mediarium tries another release. A second provider from a different company often has the missing pieces.",
		"Open Usenet settings", pathDownloads},
	{CodeUnpackFailed, LevelError, AreaDownloads,
		"Could not unpack a download",
		"The archive couldn't be opened. It may be damaged.",
		"Mediarium tries another release. If it keeps happening, check the downloads folder has free space.",
		"Open folders", pathMedia},
	{CodeUnpackPassword, LevelError, AreaDownloads,
		"A download is locked with a password",
		"The archive needs a password, so it can't be opened.",
		"Nothing to fix. Mediarium tries another release.",
		"", ""},
	{CodeUnpackToolMissing, LevelError, AreaDownloads,
		"The 7z tool is missing",
		"This download is a 7z archive and the 7z tool isn't installed.",
		"The Docker image includes it. On a native install, install 7-Zip and restart.",
		"", ""},
	{CodePar2Failed, LevelError, AreaDownloads,
		"Could not repair a download",
		"The repair files couldn't fix the damaged or missing pieces.",
		"Mediarium tries another release. If the par2 tool is missing, the Health card says so.",
		"", ""},
	{CodeIndexerRateLimited, LevelWarning, AreaSearch,
		"An indexer is limiting you",
		"The site says you've made too many requests or used up your plan's limit.",
		"Wait a while. If it keeps happening, check your plan's limits on that site.",
		"Open indexers", pathIndexers},
	{CodeIndexerAuthRefused, LevelError, AreaSearch,
		"An indexer refused the key",
		"The site didn't accept your API key or login.",
		"Copy the key again from your account on that site.",
		"Open indexers", pathIndexers},
	{CodeIndexerUnreachable, LevelWarning, AreaSearch,
		"Can't reach an indexer",
		"The site didn't answer. It may be down or have moved.",
		"Press Test on the indexer. If the site has a Cloudflare check, make sure the Cloudflare helper is running.",
		"Open indexers", pathIndexers},
	{CodeIndexerFailed, LevelWarning, AreaSearch,
		"An indexer gave an error",
		"The site returned an error instead of results.",
		"Try again in a few minutes, or press Test on the indexer.",
		"Open indexers", pathIndexers},
	{CodeIndexerFetchFailed, LevelError, AreaSearch,
		"Could not get a file from an indexer",
		"The indexer didn't hand over the file. The site may be down or the link may have expired.",
		"Press Test on the indexer. If it works, retry the download in Activity.",
		"Open indexers", pathIndexers},
	{CodeMediaServerDown, LevelWarning, AreaMediaServers,
		"Can't reach your media server",
		"Mediarium couldn't ask your media server to scan for new files.",
		"Check the server is on and its address is right. New files still appear at its next scan.",
		"Open media servers", pathMediaServers},
	{CodeMediaServerAuth, LevelWarning, AreaMediaServers,
		"Your media server refused the sign-in",
		"The media server didn't accept Mediarium's key or login.",
		"Sign in to the media server again.",
		"Open media servers", pathMediaServers},
	{CodeMediaServerRefresh, LevelWarning, AreaMediaServers,
		"A media server refresh failed",
		"The server answered but wouldn't start a scan.",
		"Check the server's own log and press Test.",
		"Open media servers", pathMediaServers},
	{CodeImportMoveFailed, LevelError, AreaImport,
		"Could not move a file into your library",
		"The download finished but the file couldn't be moved to your library.",
		"Check the library folder exists, has free space and is writable. Then retry in Activity.",
		"Open folders", pathMedia},
	{CodeImportNoVideo, LevelError, AreaImport,
		"No video in a download",
		"The download had no movie or episode file inside.",
		"Nothing to fix. Mediarium tries another release.",
		"", ""},
	{CodeImportFileExists, LevelError, AreaImport,
		"A file is already there",
		"A file is already in the place this one should go.",
		"Remove the old file, or set what happens with existing files in Settings > Library > Folders and file names.",
		"Open folders", pathMedia},
	{CodeImportFailed, LevelError, AreaImport,
		"An import failed",
		"This couldn't be added to your library.",
		"Read the message on Activity and try again.",
		"Open Activity", "/queue"},
	{CodeDiskFull, LevelError, AreaDisk,
		"The disk is full",
		"There isn't enough free space.",
		"Free up some space, or run Clean up in Settings > System. Then retry in Activity.",
		"Open System settings", pathSystem},
	{CodeFolderPermission, LevelError, AreaDisk,
		"Mediarium may not write to a folder",
		"The system refused access to a folder.",
		"Give the PUID and PGID user read and write access to the folder. On a Synology, check the shared folder's permissions.",
		"Open folders", pathMedia},
	{CodeFolderMissing, LevelError, AreaDisk,
		"A folder is missing",
		"A folder Mediarium needs isn't there.",
		"Create the folder, or check its drive is connected and mounted.",
		"Open folders", pathMedia},
	{CodeDatabaseSlow, LevelWarning, AreaDatabase,
		"The database is slow",
		"A database call took several seconds. Usually that's a slow disk or a network drive.",
		"Keep the settings folder (/config) on a local disk, not a network share. It also passes when a big job finishes.",
		"Open System settings", pathSystem},
	{CodeTMDBRateLimited, LevelWarning, AreaMetadata,
		"TMDB is limiting requests",
		"TMDB says Mediarium has asked for too much for now.",
		"It clears in a moment. A free personal TMDB key removes the shared limit.",
		"Open info settings", pathMetadata},
	{CodeTMDBKeyRejected, LevelError, AreaMetadata,
		"TMDB rejected the key",
		"TMDB didn't accept the API key.",
		"Copy the key again from your TMDB account.",
		"Open info settings", pathMetadata},
	{CodeSubtitlesRateLimited, LevelWarning, AreaMetadata,
		"OpenSubtitles limit reached",
		"Today's OpenSubtitles download limit is used up.",
		"It resets tomorrow. A free OpenSubtitles account raises the limit.",
		"Open subtitle settings", pathSubtitles},
	{CodeNotificationFailed, LevelWarning, AreaNotifications,
		"A notification was not sent",
		"A message couldn't be delivered to one of your notification targets.",
		"Press Test on the target in Settings > Connections > Notifications.",
		"Open notifications", pathNotify},
	{CodeUpdateCheckFailed, LevelWarning, AreaUpdates,
		"Could not check for updates",
		"Mediarium couldn't check for a new version.",
		"It tries again daily. Check the server can reach the internet.",
		"Open System settings", pathSystem},
	{CodeUpdateInstallFailed, LevelError, AreaUpdates,
		"The update could not be installed",
		"The new version was downloaded but couldn't be put in place. The old version keeps running.",
		"Try again from Settings > System, or update the container image.",
		"Open System settings", pathSystem},
	{CodeAppRestartedItself, LevelWarning, AreaSystem,
		"Mediarium restarted itself",
		"It stopped answering for a few minutes.",
		"If it repeats, copy the report for support. Check the settings folder is on a local disk and the NAS has memory to spare.",
		"Open System settings", pathSystem},
}

// Lookup finds the help for a code.
func Lookup(code string) (Help, bool) {
	for _, h := range catalog {
		if h.Code == code {
			return h, true
		}
	}
	return Help{}, false
}

// Codes lists every known code, in catalog order.
func Codes() []string {
	out := make([]string, len(catalog))
	for i, h := range catalog {
		out[i] = h.Code
	}
	return out
}

// Catalog returns the whole help table.
func Catalog() []Help { return append([]Help(nil), catalog...) }
