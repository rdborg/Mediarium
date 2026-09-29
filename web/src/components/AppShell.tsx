import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { scrollToTop } from '../scrollTop'
import { api, isAdmin, type WatchLink } from '../api'
import { MediaServerMark } from './mediaServerBrand'
import { useAuth } from '../AuthContext'
import { demoEnabled } from '../demo'
import { sectionFor, type SectionKey } from '../sections'
import { pageTitle, settingsGroupsFor } from '../settingsNav'
import BrandMark from './BrandMark'
import Icon, { type IconName } from './Icon'
import ProfileMenu from './ProfileMenu'
import SearchBox from './SearchBox'
import ThemeToggle from './ThemeToggle'

const NAV: { to: string; label: string; icon: IconName; sec: SectionKey; end?: boolean }[] = [
  { to: '/', label: 'Dashboard', icon: 'grid', sec: 'dashboard', end: true },
  { to: '/discover', label: 'Discover', icon: 'compass', sec: 'discover' },
  { to: '/library', label: 'Library', icon: 'film', sec: 'library' },
  { to: '/calendar', label: 'Upcoming', icon: 'calendar', sec: 'calendar' },
  { to: '/queue', label: 'Activity', icon: 'activity', sec: 'activity' },
  { to: '/settings/media', label: 'Settings', icon: 'sliders', sec: 'settings' },
]

// Persistent left sidebar plus an always-visible search bar that looks up
// movies and TV shows live as you type. On phones the sidebar slides in from
// the left.
export default function AppShell() {
  const [menuOpen, setMenuOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem('mediarium-sidebar') === 'collapsed'
    } catch {
      return false
    }
  })
  const location = useLocation()
  const [homes, setHomes] = useState<WatchLink[]>([])
  useEffect(() => {
    api.mediaServerHomes().then(setHomes).catch(() => setHomes([]))
  }, [])
  useEffect(() => scrollToTop(), [location.pathname, location.search])
  const { user } = useAuth()
  const admin = isAdmin(user)
  // Members only have their own profile and About under Settings.
  const settingsGroups = settingsGroupsFor(admin)
  const nav = NAV.map((n) => (n.sec === 'settings' ? { ...n, to: settingsGroups[0].pages[0].to } : n))
  const inUpcoming = location.pathname.startsWith('/wanted') || location.pathname.startsWith('/calendar')

  // Close the mobile drawer whenever the page changes.
  useEffect(() => setMenuOpen(false), [location.pathname])

  function toggleCollapsed() {
    setCollapsed((c) => {
      try {
        localStorage.setItem('mediarium-sidebar', c ? 'open' : 'collapsed')
      } catch {
        // best effort only
      }
      return !c
    })
  }

  const inSettings = location.pathname.startsWith('/settings')
  const [version, setVersion] = useState('')
  useEffect(() => {
    api.version().then((v) => setVersion(v.version)).catch(() => undefined)
  }, [])

  // A small count of downloads in flight next to "Activity".
  useEffect(() => {
    let cancelled = false
    const load = () =>
      api
        .listQueue()
        .then((q) => !cancelled && setActive(q.filter((i) => i.status === 'queued' || i.status === 'downloading' || i.status === 'importing').length))
        .catch(() => undefined)
    void load()
    const t = setInterval(load, 3000)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [])

  return (
    <div className="app-shell" data-section={sectionFor(location.pathname)}>
      <div className={`sidebar-backdrop${menuOpen ? ' open' : ''}`} onClick={() => setMenuOpen(false)} />
      <nav className={`sidebar${menuOpen ? ' open' : ''}${collapsed ? ' collapsed' : ''}`} aria-label="Main">
        <div className="brand">
          <BrandMark />
          <span className="brand-name">Mediarium</span>
          <button className="collapse-btn" onClick={toggleCollapsed} aria-label={collapsed ? 'Expand the menu' : 'Collapse the menu'} title={collapsed ? 'Expand the menu' : 'Collapse to icons'}>
            <span style={{ display: 'inline-flex', transform: collapsed ? 'none' : 'rotate(180deg)' }}>
              <Icon name="open" size={15} />
            </span>
          </button>
        </div>
        {nav.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end} title={collapsed ? n.label : undefined} style={{ ['--nc' as string]: `var(--c-${n.sec})` }} className={({ isActive }) => (n.sec === 'settings' && inSettings ? 'active parent-open' : isActive || (n.sec === 'calendar' && inUpcoming) ? 'active' : '')}>
            <span className="nav-ico">
              <Icon name={n.icon} size={17} />
            </span>
            <span className="nav-label">{n.label}</span>
            {n.to === '/queue' && active > 0 && <span className="nav-count">{active}</span>}
            {n.sec === 'settings' && !collapsed && (
              <span className="nav-chev" style={{ transform: inSettings ? 'rotate(90deg)' : 'none' }}>
                <Icon name="open" size={13} />
              </span>
            )}
          </NavLink>
        ))}
        {inSettings && !collapsed && (
          <div className="subnav" style={{ ['--nc' as string]: 'var(--c-settings)' }}>
            {settingsGroups.map((g) => (
              <NavLink key={g.key} to={g.pages[0].to} style={{ ['--sc' as string]: g.color }} className={() => (g.pages.some((p) => location.pathname === p.to || location.pathname.startsWith(p.to + '/')) ? 'active' : '')}>
                <span className="nav-ico">
                  <Icon name={g.icon} size={15} />
                </span>
                <span className="nav-label">{g.label}</span>
              </NavLink>
            ))}
          </div>
        )}
        {homes.length > 0 && !collapsed && (
          <div className="side-servers">
            {homes.map((h) => (
              <a key={h.serverId} href={h.url} target="_blank" rel="noreferrer" title={`Open ${h.name}`}>
                <MediaServerMark kind={h.kind} size={18} /> <span>Open {h.name}</span> <Icon name="external" size={13} />
              </a>
            ))}
          </div>
        )}
        <div className="side-version" title={`Mediarium ${version}`}>
          <strong>Mediarium</strong>
          <span>Version {version || '…'}</span>
        </div>
      </nav>
      <div className="main-area">
        <div className="topbar">
          <button className="menu-button" onClick={() => setMenuOpen((o) => !o)} aria-label="Open menu" aria-expanded={menuOpen}>
            <Icon name="menu" size={20} />
          </button>
          <h2 className="top-title">{!admin && location.pathname.startsWith('/settings/profile') ? 'Your profile' : pageTitle(location.pathname, admin)}</h2>
          <SearchBox />
          <div className="top-actions">
            {demoEnabled() && (
              <span className="demo-chip" title="Sample data is showing. Turn it off from your profile menu.">
                <Icon name="flask" size={14} /> Demo data
              </span>
            )}
            <ThemeToggle />
            <ProfileMenu />
          </div>
        </div>
        <div className="content">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
