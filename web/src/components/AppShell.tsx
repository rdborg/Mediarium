import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { api } from '../api'
import { demoEnabled } from '../demo'
import { sectionFor, type SectionKey } from '../sections'
import { SETTINGS_NAV, pageTitle } from '../settingsNav'
import BrandMark from './BrandMark'
import Icon, { type IconName } from './Icon'
import ProfileMenu from './ProfileMenu'
import SearchBox from './SearchBox'
import ThemeToggle from './ThemeToggle'

const NAV: { to: string; label: string; icon: IconName; sec: SectionKey; end?: boolean }[] = [
  { to: '/', label: 'Dashboard', icon: 'grid', sec: 'dashboard', end: true },
  { to: '/discover', label: 'Discover', icon: 'compass', sec: 'discover' },
  { to: '/library', label: 'Library', icon: 'film', sec: 'library' },
  { to: '/wanted', label: 'Wanted', icon: 'bookmark', sec: 'wanted' },
  { to: '/calendar', label: 'Calendar', icon: 'calendar', sec: 'calendar' },
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

  // A small count of downloads in flight next to "Activity".
  useEffect(() => {
    let cancelled = false
    const load = () =>
      api
        .listQueue()
        .then((q) => !cancelled && setActive(q.filter((i) => i.status === 'queued' || i.status === 'downloading' || i.status === 'importing').length))
        .catch(() => undefined)
    void load()
    const t = setInterval(load, 6000)
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
        {NAV.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end} title={collapsed ? n.label : undefined} style={{ ['--nc' as string]: `var(--c-${n.sec})` }} className={n.sec === 'settings' && inSettings ? 'active parent-open' : undefined}>
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
            {SETTINGS_NAV.map((n) => (
              <NavLink key={n.to} to={n.to}>
                <span className="nav-ico">
                  <Icon name={n.icon} size={15} />
                </span>
                <span className="nav-label">{n.label}</span>
              </NavLink>
            ))}
          </div>
        )}
      </nav>
      <div className="main-area">
        <div className="topbar">
          <button className="menu-button" onClick={() => setMenuOpen((o) => !o)} aria-label="Open menu" aria-expanded={menuOpen}>
            <Icon name="menu" size={20} />
          </button>
          <h2 className="top-title">{pageTitle(location.pathname)}</h2>
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
