import type { IconName } from './components/Icon'
import { t, tc } from './i18n.ts'

// The settings pages, in the order they appear under Settings in the sidebar
// (the order of the groups below, page by page).
// Each has its own soft colour so the list is easy to scan. Pages marked
// `member` are open to every account; the rest are for administrators only.
// `notViewer` pages are left out of the list for view-only accounts, which
// have nothing to set there. Every page is one job, named by its title, with
// one honest sentence under it (`blurb`).
export const SETTINGS_NAV: { to: string; label: string; icon: IconName; color: string; blurb: string; member?: boolean; notViewer?: boolean; help?: string }[] = [
  { to: '/settings/general', label: t('General'), icon: 'sliders', color: '#e7a6f2', blurb: t('The app as a whole: its language, look, address and updates.'), member: true },
  { to: '/settings/modules', label: t('Media types'), icon: 'grid', color: '#ff9f6b', blurb: t('Switch movies, shows, music, ebooks and audiobooks on or off.'), help: 'getting-started#media-types' },
  { to: '/settings/media', label: t('Folders'), icon: 'folder', color: '#7cc4f8', blurb: t('Where your movies, shows, music, books and downloads are kept.'), help: 'install-docker#why-one-data-folder-hardlinks' },
  { to: '/settings/naming', label: t('File names and importing'), icon: 'list', color: '#7cc4f8', blurb: t('How imported files are named, the info files saved next to them, and what runs after an import.'), help: 'library#file-names' },
  { to: '/settings/metadata', label: t('Movie and show info'), icon: 'globe', color: '#c1acf7', blurb: t('Where titles, posters, episode numbers and trailers come from.') },
  { to: '/settings/lists', label: t('Import lists'), icon: 'list', color: '#c1acf7', blurb: t('Add movies and shows by themselves from lists on Trakt, TMDB, IMDb, MDBList and Letterboxd.'), help: 'adding#import-lists' },
  { to: '/settings/subtitles', label: t('Subtitles'), icon: 'chat', color: '#f4a3c8', blurb: t('Where subtitles come from and which languages to keep.'), help: 'subtitles' },
  { to: '/settings/indexers', label: t('Indexers & Search'), icon: 'search', color: '#a5b4fc', blurb: t('The sites Mediarium searches for releases.'), help: 'indexers' },
  { to: '/settings/quality', label: t('Quality'), icon: 'star', color: '#f5c76a', blurb: t('What counts as good enough, which release wins, and which audio language you want.'), help: 'quality' },
  { to: '/settings/automation', label: t('Automatic searching'), icon: 'sliders', color: '#f0b46a', blurb: t('When Mediarium looks for things by itself, and whether it waits for Usenet or torrents.'), help: 'quality#automatic-searching' },
  { to: '/settings/usenet', label: t('Usenet'), icon: 'server', color: '#86dca4', blurb: t('Your Usenet provider and the built-in downloader.'), help: 'downloads#usenet-connections' },
  { to: '/settings/torrents', label: t('Torrents'), icon: 'magnet', color: '#86dca4', blurb: t('The built-in torrent client and its options, a debrid service, or a client you already run.'), help: 'downloads#controls-of-the-built-in-torrent-client' },
  { to: '/settings/vpn', label: t('VPN and proxy'), icon: 'shield', color: '#6fd6c8', blurb: t('Keep torrent traffic behind a VPN, or send it through a proxy.'), help: 'downloads#vpn-protection' },
  { to: '/settings/download-rules', label: t('Download rules'), icon: 'download', color: '#86dca4', blurb: t('Where to download from, how many at once, how fast and when, and which releases to skip.'), help: 'downloads#checks-on-releases' },
  { to: '/settings/media-servers', label: t('Media servers'), icon: 'monitor', color: '#f2c14e', blurb: t('Plex, Jellyfin and the rest: scan after a download and link to them.'), help: 'media-servers' },
  { to: '/settings/notifications', label: t('Notifications'), icon: 'mail', color: '#f7b27a', blurb: t('Messages when something is added, downloaded or goes wrong.'), help: 'notifications' },
  { to: '/settings/other-apps', label: t('Other apps'), icon: 'server', color: '#9fd4a8', blurb: t('Let Overseerr, Homepage and phone apps talk to Mediarium.'), help: 'media-servers#overseerr-dashboards-and-phone-apps' },
  { to: '/settings/addons', label: t('Add-ons'), icon: 'grid', color: '#9fd4a8', blurb: t('Other programs you run next to Mediarium, with a link to each.'), help: 'media-servers#add-ons' },
  { to: '/settings/profile', label: t('Your profile'), icon: 'user', color: '#8fb8f5', blurb: t('Your name, email and password, and the messages you get about your requests.'), member: true },
  { to: '/settings/security', label: t('Security'), icon: 'shield', color: '#8fb8f5', blurb: t('Passkeys, two-step sign-in, a linked single sign-on account and your API keys.'), member: true, help: 'accounts#two-step-sign-in' },
  { to: '/settings/services', label: t('Linked services'), icon: 'external', color: '#8fb8f5', blurb: t('Your Plex watchlist, ListenBrainz and your e-reader.'), member: true, notViewer: true },
  { to: '/settings/accounts', label: t('All accounts'), icon: 'user', color: '#8fb8f5', blurb: t('Add people and choose what each of them may do.'), help: 'accounts' },
  { to: '/settings/requests', label: t('Requests and reports'), icon: 'chat', color: '#8fb8f5', blurb: t('What people may ask for, how many requests they get, and problem reports.'), help: 'accounts#requests' },
  { to: '/settings/sign-in', label: t('Ways to sign in'), icon: 'key', color: '#8fb8f5', blurb: t('Passwords, passkeys, single sign-on, a directory, or a login proxy in front of Mediarium.'), help: 'accounts#ways-to-sign-in' },
  { to: '/settings/system', label: t('Server'), icon: 'hard', color: '#b4bfcc', blurb: t('How the server is doing, restarting it and cleaning up leftovers.'), help: 'problems' },
  { to: '/settings/backup', label: t('Backup and restore'), icon: 'hard', color: '#b4bfcc', blurb: t('Save a copy of everything, in the cloud too, and put it back.'), help: 'backups-updates#backup-and-restore' },
  { to: '/settings/statistics', label: t('Statistics'), icon: 'activity', color: '#8fe0c4', blurb: t('How big the library is and what was downloaded.'), member: true, help: 'library#statistics' },
  { to: '/settings/logs', label: t('Logs and errors'), icon: 'warning', color: '#f0a3a3', blurb: t('Errors Mediarium noticed, what to do about them, and a report to copy when you ask for help.'), help: 'problems' },
  { to: '/settings/migrate', label: t('Move from other apps'), icon: 'refresh', color: '#b4bfcc', blurb: t('Bring your library and settings over from Radarr, Sonarr and others.'), help: 'adding#moving-from-radarr-sonarr-and-other-apps' },
  { to: '/settings/about', label: t('About and credits'), icon: 'info', color: '#e7a6f2', blurb: t('Version, credits and the legal notice.'), member: true },
]

// Pages that moved or were split. The old address still opens, on the page
// that now holds most of what was there, so bookmarks, links in the docs and
// links from the server keep working.
export const SETTINGS_REDIRECTS: [from: string, to: string][] = [
  ['/settings/downloads', '/settings/usenet'],
  ['/settings/language', '/settings/general'],
]

// The settings pages an account may open: everything for an administrator,
// only the member pages for everyone else.
export function settingsNavFor(admin: boolean, viewer = false) {
  const pages = admin ? SETTINGS_NAV : SETTINGS_NAV.filter((s) => s.member)
  return viewer ? pages.filter((s) => !s.notViewer) : pages
}

type NavPage = (typeof SETTINGS_NAV)[number]

export interface SettingsGroup {
  key: string
  label: string
  blurb: string
  icon: IconName
  color: string
  pages: NavPage[]
}

// Settings pages are grouped in the order a person sets things up: the basics,
// then the library, then finding and downloading releases, then the apps around
// Mediarium, then people, then the system itself. Each group is one sidebar
// entry. The entries stay open in the sidebar; one entry at a time shows its
// pages as indented items underneath, so the sidebar stays short. `blurb` is
// the line shown for the group on the Settings overview.
const GROUPS: { key: string; label: string; blurb: string; icon: IconName; color: string; pages: string[] }[] = [
  { key: 'basics', label: t('Basics'), blurb: t('Language, look, address and which media types you use.'), icon: 'sliders', color: '#e7a6f2', pages: ['/settings/general', '/settings/modules'] },
  { key: 'library', label: t('Library and files'), blurb: t('Folders, file names, movie and show info, import lists and subtitles.'), icon: 'folder', color: '#7cc4f8', pages: ['/settings/media', '/settings/naming', '/settings/metadata', '/settings/lists', '/settings/subtitles'] },
  { key: 'releases', label: t('Finding releases'), blurb: t('Indexers, quality profiles and when Mediarium searches by itself.'), icon: 'search', color: '#a5b4fc', pages: ['/settings/indexers', '/settings/quality', '/settings/automation'] },
  { key: 'downloads', label: tc('section', 'Downloading'), blurb: t('Usenet, torrents, a VPN and the rules for downloads.'), icon: 'download', color: '#86dca4', pages: ['/settings/usenet', '/settings/torrents', '/settings/vpn', '/settings/download-rules'] },
  { key: 'connections', label: t('Media servers and apps'), blurb: t('Plex, Jellyfin, notifications and the apps that connect to Mediarium.'), icon: 'monitor', color: '#f2c14e', pages: ['/settings/media-servers', '/settings/notifications', '/settings/other-apps', '/settings/addons'] },
  { key: 'profile', label: t('People and access'), blurb: t('Your profile, sign-in security, accounts and requests.'), icon: 'user', color: '#8fb8f5', pages: ['/settings/profile', '/settings/security', '/settings/services', '/settings/accounts', '/settings/requests', '/settings/sign-in'] },
  { key: 'system', label: t('System'), blurb: t('The server, backups, statistics, logs and moving from other apps.'), icon: 'hard', color: '#b4bfcc', pages: ['/settings/system', '/settings/backup', '/settings/statistics', '/settings/logs', '/settings/migrate', '/settings/about'] },
]

export const inPage = (pathname: string, to: string) => pathname === to || pathname.startsWith(to + '/')

// The groups an account sees, each with only the pages it may open. A group
// left with one page takes that page's name.
export function settingsGroupsFor(admin: boolean, viewer = false): SettingsGroup[] {
  const pages = settingsNavFor(admin, viewer)
  return GROUPS.map((g) => {
    const visible = g.pages.map((to) => pages.find((p) => p.to === to)).filter((p): p is NavPage => !!p)
    return { ...g, label: visible.length === 1 && g.pages.length > 1 ? visible[0].label : visible.length === 1 && !admin ? visible[0].label : g.label, pages: visible }
  }).filter((g) => g.pages.length > 0)
}

export function groupFor(pathname: string, admin: boolean): SettingsGroup | undefined {
  return settingsGroupsFor(admin).find((g) => g.pages.some((p) => inPage(pathname, p.to)))
}

// The page name shown at the left of the header. Pages in a group with several
// pages read "Group · Page" (for example "Library · Quality"); the group part is
// shown smaller and left out on a phone, where the page name alone is clearer.
export function pageParts(pathname: string, admin = true): { group?: string; page: string } {
  const group = groupFor(pathname, admin)
  if (group) {
    const page = group.pages.find((p) => inPage(pathname, p.to))
    return page && group.pages.length > 1 ? { group: group.label, page: page.label } : { page: group.label }
  }
  return { page: pageTitle(pathname, admin) }
}

// What the page is for, one line, shown under the name of a settings page.
export function pageBlurb(pathname: string, admin = true): string {
  return settingsNavFor(admin).find((p) => inPage(pathname, p.to))?.blurb ?? ''
}

// The user guide topic that explains the page, if there is one.
export function pageHelp(pathname: string, admin = true): string {
  return settingsNavFor(admin).find((p) => inPage(pathname, p.to))?.help ?? ''
}

export function pageTitle(pathname: string, admin = true): string {
  if (pathname === '/') return t('Dashboard')
  const group = groupFor(pathname, admin)
  if (group) {
    const page = group.pages.find((p) => inPage(pathname, p.to))
    return page && group.pages.length > 1 ? `${group.label} · ${page.label}` : group.label
  }
  const settings = SETTINGS_NAV.find((s) => inPage(pathname, s.to))
  if (settings) return settings.label
  if (pathname.startsWith('/settings')) return t('Settings')
  if (pathname.startsWith('/discover')) return t('Discover')
  if (pathname.startsWith('/search')) return tc('noun', 'Search')
  if (pathname.startsWith('/title')) return t('Movie')
  if (pathname.startsWith('/show') || pathname.startsWith('/series')) return t('TV show')
  if (pathname.startsWith('/person')) return t('Person')
  if (pathname.startsWith('/library')) return t('Library')
  if (pathname.startsWith('/music/import')) return t('Import music')
  if (pathname.startsWith('/music')) return t('Music')
  if (pathname.startsWith('/book/') || pathname.startsWith('/books/')) return t('Book')
  if (pathname.startsWith('/import')) return t('Import')
  if (pathname.startsWith('/wanted') || pathname.startsWith('/calendar')) return t('Upcoming')
  if (pathname.startsWith('/queue')) return t('Activity')
  if (pathname.startsWith('/stats')) return t('Statistics')
  return 'Mediarium'
}
