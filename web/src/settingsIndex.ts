import { t } from './i18n.ts'
import { settingsGroupsFor, settingsNavFor } from './settingsNav.ts'
import { rank, type Searchable } from './settingsSearch.ts'

// Everything the search bar can take you to inside Settings: every page, the
// cards on each page, and the settings people look for most. One entry per
// place. Pages come from the sidebar list itself, so a new page is found
// without anything added here; the cards and single settings are listed by
// hand below, with the words people are likely to type for them.
//
// A card or setting is found on its page by its heading, so `label` is the
// same text the page shows (the same t() phrase, so it is translated the same
// way). The extra `words` stay in English: they are only used for matching.

export interface SettingsEntry extends Searchable {
  to: string // the settings page
  anchor?: string // the heading to scroll to on that page (shown text)
  page: boolean // a whole page rather than something on it
}

type Module = 'movies' | 'tv' | 'music' | 'ebooks' | 'audiobooks'

interface Spec {
  to: string
  label: string
  words: string
  admin?: boolean // only administrators, on a page members may open too
  module?: Module[] // shown only when one of these media types is on
}

// Words people use for each page, besides its name.
const PAGE_WORDS: Record<string, string> = {
  general: 'preferences language theme dark light time zone address update',
  modules: 'movies tv shows music books ebooks audiobooks enable disable turn on off',
  media: 'paths directories library root storage movies tv music books downloads hardlinks',
  naming: 'rename naming format pattern template nfo artwork script import',
  quality: 'profiles resolution 1080p 4k upgrade cutoff custom formats size language',
  automation: 'automatic search schedule rss monitor upgrade delay',
  indexers: 'newznab torznab prowlarr jackett sites flaresolverr irc',
  usenet: 'provider server nntp newshosting connections ssl',
  torrents: 'qbittorrent debrid seeding client',
  vpn: 'wireguard openvpn proxy privacy socks',
  'download-rules': 'limits speed concurrent temp folder schedule bandwidth',
  metadata: 'tmdb info posters trailers anime numbering',
  lists: 'trakt imdb watchlist mdblist letterboxd simkl',
  subtitles: 'opensubtitles captions srt languages',
  'media-servers': 'plex jellyfin emby kodi scan',
  notifications: 'discord telegram email webhook alerts push ntfy gotify',
  'other-apps': 'overseerr jellyseerr homepage nzb360 lunasea ruddarr radarr sonarr api',
  addons: 'links apps sidebar',
  profile: 'name email password api key avatar',
  security: 'passkey sessions devices api keys',
  services: 'plex listenbrainz kobo e-reader hardcover',
  accounts: 'users people roles admin member viewer invite',
  requests: 'quota limit approve problem reports',
  'sign-in': 'sso oidc ldap active directory login proxy',
  system: 'restart cleanup disk server health memory',
  backup: 'restore export cloud copy',
  statistics: 'stats charts numbers',
  logs: 'errors problems debug support report',
  migrate: 'import radarr sonarr prowlarr sabnzbd move switch',
  about: 'version credits legal licence open source',
}

// The cards and single settings. Each label is the heading the page shows.
function cardSpecs(): Spec[] {
  return [
    // General
    { to: 'general', label: t('App language'), words: 'language translate idioma locale english french german spanish' },
    { to: 'general', label: t('Light or dark'), words: 'theme dark mode light appearance colours look' },
    { to: 'general', label: t('Time zone'), words: 'timezone clock hours tz' },
    { to: 'general', label: t('Address of Mediarium'), words: 'remote access public address url domain external reverse proxy https share link', admin: true },
    { to: 'general', label: t('Updates'), words: 'update upgrade version new release check', admin: true },
    { to: 'general', label: t('Updates pushed through the API'), words: 'push update api watchtower automatic', admin: true },
    { to: 'general', label: t('Anonymous usage numbers'), words: 'telemetry statistics privacy analytics', admin: true },
    { to: 'general', label: t('Legal notice'), words: 'legal terms disclaimer responsible use', admin: true },
    // Folders
    { to: 'media', label: t('Movies folder'), words: 'movie library path root directory', module: ['movies'] },
    { to: 'media', label: t('TV folder'), words: 'shows series library path root directory', module: ['tv'] },
    { to: 'media', label: t('Music folder'), words: 'albums artists library path root directory', module: ['music'] },
    { to: 'media', label: t('Ebooks folder'), words: 'books epub library path directory', module: ['ebooks'] },
    { to: 'media', label: t('Audiobooks folder'), words: 'audio books library path directory', module: ['audiobooks'] },
    { to: 'media', label: t('Downloads folder'), words: 'download path complete incomplete directory' },
    { to: 'media', label: t('More folders for other titles (optional)'), words: 'extra library kids second drive multiple root' },
    { to: 'media', label: t('Second copies of the same title (optional)'), words: '4k copy duplicate two qualities' },
    { to: 'media', label: t('File permissions'), words: 'chmod chown permissions puid pgid umask owner access' },
    // File names and importing
    { to: 'naming', label: t('Naming and importing'), words: 'naming format preset rename pattern template folder names import hardlink copy move' },
    { to: 'naming', label: t('NFO and artwork files'), words: 'nfo poster fanart artwork kodi metadata files' },
    { to: 'naming', label: t('Rename existing files'), words: 'rename bulk fix names' },
    { to: 'naming', label: t('Run a script after each import'), words: 'script post processing hook custom command' },
    // Quality
    { to: 'quality', label: t('Quality profiles'), words: 'profile cutoff upgrade resolution 1080p 4k allowed qualities' },
    { to: 'quality', label: t('Custom formats'), words: 'score formats trash guides hdr atmos' },
    { to: 'quality', label: t('Audio language'), words: 'language dub original multi audio' },
    { to: 'quality', label: t('Size limits'), words: 'size minimum maximum megabytes per minute file size' },
    { to: 'quality', label: t('Advanced options'), words: 'advanced custom formats scores' },
    // Automatic searching
    { to: 'automation', label: t('Automation'), words: 'automatic search interval rss monitor schedule upgrade' },
    { to: 'automation', label: t('Wait for a better protocol'), words: 'delay profile usenet torrent wait prefer' },
    // Indexers & Search
    { to: 'indexers', label: t('Your indexers'), words: 'newznab torznab prowlarr jackett sites list' },
    { to: 'indexers', label: t('Add an indexer'), words: 'new nzbgeek drunkenslug indexer add' },
    { to: 'indexers', label: t('IRC announce'), words: 'irc announce autobrr real time' },
    { to: 'indexers', label: t('How to add FlareSolverr to Docker'), words: 'flaresolverr cloudflare captcha browser check' },
    // Usenet
    { to: 'usenet', label: t('Your Usenet provider'), words: 'usenet server news provider ssl connections port nntp newshosting' },
    { to: 'usenet', label: t('Downloader'), words: 'built in downloader sabnzbd nzbget' },
    // Torrents
    { to: 'torrents', label: t('Client settings'), words: 'built in qbittorrent torrent client' },
    { to: 'torrents', label: t('Torrent options'), words: 'seeding ratio port dht upload limit' },
    { to: 'torrents', label: t('Stalled torrents'), words: 'stuck dead stalled' },
    { to: 'torrents', label: t('Debrid service'), words: 'real debrid alldebrid premiumize torbox' },
    { to: 'torrents', label: t('Use your own torrent client'), words: 'qbittorrent deluge transmission external client' },
    // VPN and proxy
    { to: 'vpn', label: t('VPN status'), words: 'vpn connected leak ip' },
    { to: 'vpn', label: t('Your VPN connections'), words: 'wireguard openvpn vpn connections' },
    { to: 'vpn', label: t('Add a VPN connection'), words: 'wireguard openvpn vpn config add' },
    { to: 'vpn', label: t('Proxy'), words: 'socks http proxy' },
    // Download rules
    { to: 'download-rules', label: t('Where to download from'), words: 'usenet torrent only both source' },
    { to: 'download-rules', label: t('Downloads at the same time'), words: 'concurrent parallel simultaneous limit' },
    { to: 'download-rules', label: t('Speed, hours and free space'), words: 'bandwidth limit throttle min free space disk' },
    { to: 'download-rules', label: t('Temporary download folder'), words: 'temp tmp incomplete cache drive scratch' },
    { to: 'download-rules', label: t('Speed schedule'), words: 'throttle night hours bandwidth' },
    { to: 'download-rules', label: t('Checks on releases'), words: 'reject password archive sample fake blocklist retention' },
    { to: 'download-rules', label: t('How old a Usenet post may be'), words: 'retention age days usenet' },
    // Movie and show info
    { to: 'metadata', label: t('Trailers'), words: 'youtube trailer videos' },
    { to: 'metadata', label: t('Anime numbering'), words: 'scene numbering absolute anidb tvdb' },
    { to: 'metadata', label: t('Hardcover (books)'), words: 'hardcover api token book info' },
    { to: 'metadata', label: t('Google Books (books)'), words: 'google books api key language cover' },
    // Import lists
    { to: 'lists', label: t('Import lists'), words: 'trakt imdb tmdb mdblist letterboxd watchlist plex lists' },
    { to: 'lists', label: t('Your Simkl account'), words: 'simkl account' },
    // Subtitles
    { to: 'subtitles', label: t('Subtitle sources'), words: 'opensubtitles providers sources' },
    { to: 'subtitles', label: t('Your OpenSubtitles.com account (optional)'), words: 'opensubtitles account login limit' },
    { to: 'subtitles', label: t('Subtitle add-ons'), words: 'addons plugins subtitle' },
    // Media servers
    { to: 'media-servers', label: t('Your media servers'), words: 'plex jellyfin emby kodi servers' },
    { to: 'media-servers', label: t('Add a media server'), words: 'plex jellyfin emby kodi add connect token' },
    { to: 'media-servers', label: t('Find my media servers'), words: 'discover scan network find' },
    { to: 'media-servers', label: t('While someone is watching'), words: 'pause playing wait scan' },
    { to: 'media-servers', label: t("What's been watched"), words: 'watched sync trakt plex played' },
    { to: 'media-servers', label: t('Cleanup rules'), words: 'delete watched remove old cleanup' },
    // Notifications
    { to: 'notifications', label: t('Your notifications'), words: 'discord telegram email webhook pushover gotify ntfy apprise' },
    { to: 'notifications', label: t('Add a notification'), words: 'discord telegram email webhook add' },
    { to: 'notifications', label: t('Quiet hours'), words: 'do not disturb silence night mute' },
    { to: 'notifications', label: t('Weekly summary'), words: 'digest email report week' },
    { to: 'notifications', label: t('Connection watch'), words: 'offline alert health down' },
    // Other apps
    { to: 'other-apps', label: t('Connect apps made for Radarr, Sonarr and Lidarr'), words: 'overseerr jellyseerr homepage homarr nzb360 lunasea ruddarr api' },
    { to: 'other-apps', label: t('The API key'), words: 'api key token integration' },
    { to: 'other-apps', label: t('Let AI assistants connect (MCP)'), words: 'mcp ai assistant chat voice home assistant llm' },
    // Your profile
    { to: 'profile', label: t('Your details'), words: 'name email avatar picture' },
    { to: 'profile', label: t('Change password'), words: 'password reset' },
    { to: 'profile', label: t('API keys'), words: 'api key token integration x-api-key' },
    { to: 'profile', label: t('Messages about your requests'), words: 'request notifications email' },
    // Security
    { to: 'security', label: t('Passkeys'), words: 'passkey webauthn fingerprint face login' },
    { to: 'security', label: t('Two-step sign-in'), words: '2fa totp authenticator mfa two factor code' },
    { to: 'security', label: t('Signed-in devices'), words: 'sessions logout devices browsers' },
    { to: 'security', label: t('Sign out idle browsers'), words: 'timeout inactivity idle logout', admin: true },
    // Linked services
    { to: 'services', label: t('Plex watchlist'), words: 'plex watchlist link' },
    { to: 'services', label: t('ListenBrainz'), words: 'scrobble listens music' },
    { to: 'services', label: t('Read on an e-reader'), words: 'kobo opds kindle e-reader books' },
    // All accounts
    { to: 'accounts', label: t('Add an account'), words: 'user invite new person create' },
    { to: 'accounts', label: t('Accounts'), words: 'users roles admin member viewer permissions' },
    // Requests and reports
    { to: 'requests', label: t('Limit requests'), words: 'quota limit per week' },
    { to: 'requests', label: t('4K requests'), words: '4k uhd requests' },
    { to: 'requests', label: t('Problem reports'), words: 'report problem broken' },
    // Ways to sign in
    { to: 'sign-in', label: t('Ways to sign in'), words: 'password passkey login methods' },
    { to: 'sign-in', label: t('Single sign-on (OpenID Connect)'), words: 'sso oidc authentik keycloak google openid' },
    { to: 'sign-in', label: t('Directory sign-in (LDAP or Active Directory)'), words: 'ldap active directory' },
    { to: 'sign-in', label: t('Sign in through your reverse proxy'), words: 'proxy header authelia forward auth' },
    // Server
    { to: 'system', label: t('Clean up'), words: 'leftovers temp files disk space' },
    { to: 'system', label: t('Restart'), words: 'reboot restart server' },
    // Backup
    { to: 'backup', label: t('Backup'), words: 'export zip database save copy' },
    { to: 'backup', label: t('Cloud backups'), words: 's3 dropbox google drive webdav onedrive' },
    { to: 'backup', label: t('Restore'), words: 'import backup file put back' },
    // Statistics
    { to: 'statistics', label: t('Downloads by month'), words: 'stats chart downloads' },
    { to: 'statistics', label: t('By quality'), words: 'stats chart quality' },
    // Logs
    { to: 'logs', label: t('Messages about errors'), words: 'email alert errors' },
    // About
    { to: 'about', label: t('Legal and responsible use'), words: 'legal terms licence' },
    { to: 'about', label: t('Credits: what Mediarium is built with'), words: 'credits open source libraries licences' },
  ]
}

// The index for one kind of account. `modulesOn` says whether a media type is
// switched on, so folders for a type that is off are not offered.
export function buildSettingsIndex(admin: boolean, viewer: boolean, modulesOn: (m: Module) => boolean = () => true): SettingsEntry[] {
  const pages = settingsNavFor(admin, viewer)
  const groups = settingsGroupsFor(admin, viewer)
  const groupOf = (to: string) => groups.find((g) => g.pages.some((p) => p.to === to))
  const trailOf = (to: string, pageLabel: string) => {
    const g = groupOf(to)
    return g && g.pages.length > 1 ? `${g.label} › ${pageLabel}` : pageLabel
  }
  const out: SettingsEntry[] = pages.map((p) => {
    const g = groupOf(p.to)
    const key = p.to.replace('/settings/', '')
    return { to: p.to, page: true, label: p.label, trail: g && g.pages.length > 1 ? g.label : t('Settings'), words: `${p.blurb} ${PAGE_WORDS[key] ?? ''}` }
  })
  for (const c of cardSpecs()) {
    const to = `/settings/${c.to}`
    const page = pages.find((p) => p.to === to)
    if (!page || (c.admin && !admin) || (c.module && !c.module.some(modulesOn))) continue
    out.push({ to, page: false, anchor: c.label, label: c.label, trail: trailOf(to, page.label), words: c.words })
  }
  return out
}

// The best matches for what was typed.
export function searchSettings(query: string, entries: readonly SettingsEntry[], limit = 5): { entry: SettingsEntry; score: number }[] {
  return rank(query, entries, limit)
}

// The route paths the entries point at (for the test that checks the router).
export function settingsRoutes(entries: readonly SettingsEntry[]): string[] {
  return [...new Set(entries.map((e) => e.to))]
}
