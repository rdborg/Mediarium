import type { IconName } from './components/Icon'

// The settings pages, in the order they appear under Settings in the sidebar.
// Pages marked `member` are open to every account; the rest are for administrators only.
export const SETTINGS_NAV: { to: string; label: string; icon: IconName; member?: boolean }[] = [
  { to: '/settings/modules', label: 'Media types', icon: 'grid' },
  { to: '/settings/media', label: 'Folders and file names', icon: 'folder' },
  { to: '/settings/quality', label: 'Quality', icon: 'star' },
  { to: '/settings/indexers', label: 'Indexers & Search', icon: 'search' },
  { to: '/settings/downloads', label: 'Usenet and torrents', icon: 'download' },
  { to: '/settings/vpn', label: 'VPN protection', icon: 'shield' },
  { to: '/settings/subtitles', label: 'Subtitles', icon: 'chat' },
  { to: '/settings/metadata', label: 'Movie info and lists', icon: 'globe' },
  { to: '/settings/media-servers', label: 'Media servers', icon: 'monitor' },
  { to: '/settings/notifications', label: 'Notifications', icon: 'mail' },
  { to: '/settings/profile', label: 'Accounts', icon: 'user', member: true },
  { to: '/settings/system', label: 'Server and backup', icon: 'hard' },
  { to: '/settings/logs', label: 'Logs and errors', icon: 'warning' },
  { to: '/settings/migrate', label: 'Move from other apps', icon: 'refresh' },
  { to: '/settings/about', label: 'About and credits', icon: 'info', member: true },
]

// The settings pages an account may open: everything for an administrator,
// only the member pages for everyone else.
export function settingsNavFor(admin: boolean) {
  if (admin) return SETTINGS_NAV
  // Members manage only their own profile, so the page is named for that.
  return SETTINGS_NAV.filter((s) => s.member).map((s) => (s.to === '/settings/profile' ? { ...s, label: 'Your profile' } : s))
}

type NavPage = (typeof SETTINGS_NAV)[number]

export interface SettingsGroup {
  key: string
  label: string
  icon: IconName
  pages: NavPage[]
}

// Settings pages on the same topic share one sidebar entry. Opening it shows
// its pages as indented items underneath, and every other entry stays closed,
// so the sidebar stays short.
const GROUPS: { key: string; label: string; icon: IconName; pages: string[] }[] = [
  { key: 'modules', label: 'Media types', icon: 'grid', pages: ['/settings/modules'] },
  { key: 'library', label: 'Library', icon: 'folder', pages: ['/settings/media', '/settings/quality'] },
  { key: 'indexers', label: 'Indexers & Search', icon: 'search', pages: ['/settings/indexers'] },
  { key: 'downloads', label: 'Downloading', icon: 'download', pages: ['/settings/downloads', '/settings/vpn'] },
  { key: 'metadata', label: 'Info, lists and subtitles', icon: 'globe', pages: ['/settings/metadata', '/settings/subtitles'] },
  { key: 'connections', label: 'Connections', icon: 'monitor', pages: ['/settings/media-servers', '/settings/notifications'] },
  { key: 'profile', label: 'Accounts', icon: 'user', pages: ['/settings/profile'] },
  { key: 'system', label: 'System', icon: 'hard', pages: ['/settings/system', '/settings/logs', '/settings/migrate', '/settings/about'] },
]

export const inPage = (pathname: string, to: string) => pathname === to || pathname.startsWith(to + '/')

// The groups an account sees, each with only the pages it may open. A group
// left with one page takes that page's name (for example "Your profile").
export function settingsGroupsFor(admin: boolean): SettingsGroup[] {
  const pages = settingsNavFor(admin)
  return GROUPS.map((g) => {
    const visible = g.pages.map((to) => pages.find((p) => p.to === to)).filter((p): p is NavPage => !!p)
    return { ...g, label: visible.length === 1 && g.pages.length > 1 ? visible[0].label : visible.length === 1 && !admin ? visible[0].label : g.label, pages: visible }
  }).filter((g) => g.pages.length > 0)
}

export function groupFor(pathname: string, admin: boolean): SettingsGroup | undefined {
  return settingsGroupsFor(admin).find((g) => g.pages.some((p) => inPage(pathname, p.to)))
}

// The page name shown at the left of the header. Pages in a group with several
// pages read "Group · Page" (for example "Downloading · VPN protection").
export function pageTitle(pathname: string, admin = true): string {
  if (pathname === '/') return 'Dashboard'
  const group = groupFor(pathname, admin)
  if (group) {
    const page = group.pages.find((p) => inPage(pathname, p.to))
    return page && group.pages.length > 1 ? `${group.label} · ${page.label}` : group.label
  }
  const settings = SETTINGS_NAV.find((s) => inPage(pathname, s.to))
  if (settings) return settings.label
  if (pathname.startsWith('/settings')) return 'Settings'
  if (pathname.startsWith('/discover')) return 'Discover'
  if (pathname.startsWith('/search')) return 'Search'
  if (pathname.startsWith('/title')) return 'Movie'
  if (pathname.startsWith('/show') || pathname.startsWith('/series')) return 'TV show'
  if (pathname.startsWith('/library')) return 'Library'
  if (pathname.startsWith('/music/import')) return 'Import music'
  if (pathname.startsWith('/music')) return 'Music'
  if (pathname.startsWith('/book/') || pathname.startsWith('/books/')) return 'Book'
  if (pathname.startsWith('/import')) return 'Import'
  if (pathname.startsWith('/wanted') || pathname.startsWith('/calendar')) return 'Upcoming'
  if (pathname.startsWith('/queue')) return 'Activity'
  return 'Mediarium'
}
