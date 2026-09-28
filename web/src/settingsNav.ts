import type { IconName } from './components/Icon'

// The settings pages, in the order they appear under Settings in the sidebar.
export const SETTINGS_NAV: { to: string; label: string; icon: IconName }[] = [
  { to: '/settings/media', label: 'Media Management', icon: 'folder' },
  { to: '/settings/quality', label: 'Quality', icon: 'star' },
  { to: '/settings/indexers', label: 'Indexers', icon: 'search' },
  { to: '/settings/downloads', label: 'Downloads', icon: 'download' },
  { to: '/settings/vpn', label: 'VPN', icon: 'shield' },
  { to: '/settings/subtitles', label: 'Subtitles', icon: 'chat' },
  { to: '/settings/metadata', label: 'Metadata & Lists', icon: 'globe' },
  { to: '/settings/notifications', label: 'Notifications', icon: 'mail' },
  { to: '/settings/profile', label: 'Profile', icon: 'user' },
  { to: '/settings/system', label: 'System & Backup', icon: 'hard' },
  { to: '/settings/about', label: 'About & Credits', icon: 'info' },
]

// The page name shown at the left of the header.
export function pageTitle(pathname: string): string {
  if (pathname === '/') return 'Dashboard'
  const settings = SETTINGS_NAV.find((s) => pathname.startsWith(s.to))
  if (settings) return settings.label
  if (pathname.startsWith('/settings')) return 'Settings'
  if (pathname.startsWith('/discover')) return 'Discover'
  if (pathname.startsWith('/search')) return 'Search'
  if (pathname.startsWith('/title')) return 'Movie'
  if (pathname.startsWith('/show') || pathname.startsWith('/series')) return 'TV show'
  if (pathname.startsWith('/library')) return 'Library'
  if (pathname.startsWith('/import')) return 'Import'
  if (pathname.startsWith('/wanted')) return 'Wanted'
  if (pathname.startsWith('/calendar')) return 'Calendar'
  if (pathname.startsWith('/queue')) return 'Activity'
  return 'Mediarium'
}
