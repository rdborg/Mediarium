import type { IconName } from './components/Icon'

// The settings pages, in the order they appear under Settings in the sidebar.
// Each has its own soft colour so the list is easy to scan. Pages marked
// `member` are open to every account; the rest are for administrators only.
export const SETTINGS_NAV: { to: string; label: string; icon: IconName; color: string; member?: boolean }[] = [
  { to: '/settings/modules', label: 'Media types', icon: 'grid', color: '#ff9f6b' },
  { to: '/settings/media', label: 'Folders and file names', icon: 'folder', color: '#7cc4f8' },
  { to: '/settings/quality', label: 'Quality', icon: 'star', color: '#f5c76a' },
  { to: '/settings/indexers', label: 'Indexers & Search', icon: 'search', color: '#a5b4fc' },
  { to: '/settings/downloads', label: 'Usenet and torrents', icon: 'download', color: '#86dca4' },
  { to: '/settings/vpn', label: 'VPN protection', icon: 'shield', color: '#6fd6c8' },
  { to: '/settings/subtitles', label: 'Subtitles', icon: 'chat', color: '#f4a3c8' },
  { to: '/settings/metadata', label: 'Movie info and lists', icon: 'globe', color: '#c1acf7' },
  { to: '/settings/media-servers', label: 'Media servers', icon: 'monitor', color: '#f2c14e' },
  { to: '/settings/notifications', label: 'Notifications', icon: 'mail', color: '#f7b27a' },
  { to: '/settings/profile', label: 'Accounts', icon: 'user', color: '#8fb8f5', member: true },
  { to: '/settings/system', label: 'Server and backup', icon: 'hard', color: '#b4bfcc' },
  { to: '/settings/logs', label: 'Logs and errors', icon: 'warning', color: '#f0a3a3' },
  { to: '/settings/migrate', label: 'Move from other apps', icon: 'refresh', color: '#b4bfcc' },
  { to: '/settings/about', label: 'About and credits', icon: 'info', color: '#e7a6f2', member: true },
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
  color: string
  pages: NavPage[]
}

// Settings pages on the same topic share one sidebar entry. Opening it shows
// its pages as indented items underneath, and every other entry stays closed,
// so the sidebar stays short.
const GROUPS: { key: string; label: string; icon: IconName; color: string; pages: string[] }[] = [
  { key: 'modules', label: 'Media types', icon: 'grid', color: '#ff9f6b', pages: ['/settings/modules'] },
  { key: 'library', label: 'Library', icon: 'folder', color: '#7cc4f8', pages: ['/settings/media', '/settings/quality'] },
  { key: 'indexers', label: 'Indexers & Search', icon: 'search', color: '#a5b4fc', pages: ['/settings/indexers'] },
  { key: 'downloads', label: 'Downloading', icon: 'download', color: '#86dca4', pages: ['/settings/downloads', '/settings/vpn'] },
  { key: 'metadata', label: 'Info, lists and subtitles', icon: 'globe', color: '#c1acf7', pages: ['/settings/metadata', '/settings/subtitles'] },
  { key: 'connections', label: 'Connections', icon: 'monitor', color: '#f2c14e', pages: ['/settings/media-servers', '/settings/notifications'] },
  { key: 'profile', label: 'Accounts', icon: 'user', color: '#8fb8f5', pages: ['/settings/profile'] },
  { key: 'system', label: 'System', icon: 'hard', color: '#b4bfcc', pages: ['/settings/system', '/settings/logs', '/settings/migrate', '/settings/about'] },
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
  if (pathname.startsWith('/import')) return 'Import'
  if (pathname.startsWith('/wanted') || pathname.startsWith('/calendar')) return 'Upcoming'
  if (pathname.startsWith('/queue')) return 'Activity'
  return 'Mediarium'
}
