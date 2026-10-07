import { useCallback, useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { scrollToTop } from '../scrollTop'
import { api, isAdmin, type WatchLink } from '../api'
import { MEDIA_SERVER_BRAND, MediaServerMark } from './mediaServerBrand'
import { useAuth } from '../AuthContext'
import { demoEnabled } from '../demo'
import { titleFor, useSpecificTitle } from '../documentTitle'
import { sectionFor, type SectionKey } from '../sections'
import { inPage, pageTitle, settingsGroupsFor } from '../settingsNav'
import { useLive } from '../useLive'
import { useUnreadErrors } from '../useProblems'
import { useModules } from '../ModulesContext'
import { openBookApp } from '../bookshelf/open'
import BrandMark from './BrandMark'
import BusyNotice from './BusyNotice'
import ImportBanner from './ImportBanner'
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
  // A small, hideable link to support the project (administrators only). Hidden per browser.
  const [showSupport] = useState(() => {
    try {
      return localStorage.getItem('mediarium-hide-support') !== '1'
    } catch {
      return true
    }
  })
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
  const { on: moduleOn } = useModules()
  const booksOn = moduleOn('ebooks') || moduleOn('audiobooks')
  const inUpcoming = location.pathname.startsWith('/wanted') || location.pathname.startsWith('/calendar')
  // Errors from the last day that nobody has marked as read (administrators only).
  const unreadErrors = useUnreadErrors(admin)
  const errorDot = <span className="nav-dot" role="img" aria-label="New errors" title="New errors. See Settings, System, Logs and errors." />

  // Close the mobile drawer whenever the page changes, and open the settings
  // group that holds the new page.
  const [folded, setFolded] = useState(false)
  useEffect(() => {
    setMenuOpen(false)
    setFolded(false)
  }, [location.pathname])

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

  // The page name in the top bar, in the browser tab and (when the page has no
  // heading of its own) as the page's one <h1>.
  const heading = !admin && location.pathname.startsWith('/settings/profile') ? 'Your profile' : pageTitle(location.pathname, admin)
  const specificTitle = useSpecificTitle()
  useEffect(() => {
    document.title = titleFor(specificTitle || heading)
  }, [specificTitle, heading])
  const mainRef = useRef<HTMLElement>(null)
  const [pageHasHeading, setPageHasHeading] = useState(false)
  useEffect(() => {
    const el = mainRef.current
    if (!el) return
    const check = () => setPageHasHeading(el.querySelector('h1') !== null)
    check()
    const watch = new MutationObserver(check)
    watch.observe(el, { childList: true, subtree: true })
    return () => watch.disconnect()
  }, [location.pathname])
  const [version, setVersion] = useState('')
  useEffect(() => {
    api.version().then((v) => setVersion(v.version)).catch(() => undefined)
  }, [])
  // A dot on the version when a newer release is waiting (administrators only).
  const [updateReady, setUpdateReady] = useState(false)
  const checkUpdate = useCallback(() => {
    if (!admin) return
    api
      .updateLatest()
      .then((n) => setUpdateReady(n.available))
      .catch(() => undefined)
  }, [admin])
  useEffect(checkUpdate, [checkUpdate])
  useLive(checkUpdate, 10 * 60 * 1000, admin)

  // A small count of downloads in flight next to "Activity".
  const loadActive = useCallback(() => {
    api
      .listQueue()
      .then((q) => setActive(q.filter((i) => i.status === 'queued' || i.status === 'downloading' || i.status === 'importing').length))
      .catch(() => undefined)
  }, [])
  useEffect(loadActive, [loadActive])
  useLive(loadActive, 3000)

  return (
    <div className="app-shell" data-section={sectionFor(location.pathname)}>
      <a
        className="skip-link"
        href="#main"
        onClick={(e) => {
          e.preventDefault()
          mainRef.current?.focus()
        }}
      >
        Skip to content
      </a>
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
          <NavLink key={n.to} to={n.to} end={n.end} title={collapsed ? n.label : undefined} className={({ isActive }) => (n.sec === 'settings' && inSettings ? 'active parent-open' : isActive || (n.sec === 'calendar' && inUpcoming) ? 'active' : '')}>
            <span className="nav-ico">
              <Icon name={n.icon} size={17} />
            </span>
            <span className="nav-label">{n.label}</span>
            {n.to === '/queue' && active > 0 && <span className="nav-count">{active}</span>}
          </NavLink>
        ))}
        <div className="subnav">
          {settingsGroups.map((g) => {
            const current = g.pages.some((p) => inPage(location.pathname, p.to))
            if (g.pages.length === 1) {
              return (
                <NavLink key={g.key} to={g.pages[0].to} title={collapsed ? g.label : undefined} className={() => (current ? 'active' : '')}>
                  <span className="nav-ico">
                    <Icon name={g.icon} size={15} />
                  </span>
                  <span className="nav-label">{g.label}</span>
                </NavLink>
              )
            }
            // The group holding the open page is open; the rest stay closed.
            // Pressing the open group folds it up (and back down) without leaving the page.
            const open = current && !folded
            return (
              <div key={g.key} className={`sn-group${open ? ' open' : ''}`}>
                <NavLink
                  to={g.pages[0].to}
                  title={collapsed ? `${g.label}: ${g.pages.map((p) => p.label).join(', ')}` : undefined}
                  aria-expanded={open}
                  className={() => (current ? 'sn-head current' : 'sn-head')}
                  onClick={(e) => {
                    if (current) {
                      e.preventDefault()
                      setFolded((f) => !f)
                    }
                  }}
                >
                  <span className="nav-ico">
                    <Icon name={g.icon} size={15} />
                  </span>
                  <span className="nav-label">{g.label}</span>
                  {g.key === 'system' && unreadErrors > 0 && errorDot}
                  <span className="nav-chev" style={{ transform: open ? 'rotate(90deg)' : 'none' }}>
                    <Icon name="open" size={12} />
                  </span>
                </NavLink>
                {open && (
                  <div className="sn-pages">
                    {g.pages.map((p) => (
                      <NavLink key={p.to} to={p.to} title={p.label} className={() => (inPage(location.pathname, p.to) ? 'sn-page active' : 'sn-page')}>
                        <span className="sn-ico">
                          <Icon name={p.icon} size={13} />
                        </span>
                        <span className="nav-label">{p.label}</span>
                        {p.to === '/settings/logs' && unreadErrors > 0 && <span className="sn-count">{unreadErrors}</span>}
                      </NavLink>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </nav>
      <div className="main-area">
        <header className="topbar">
          <button className="menu-button" onClick={() => setMenuOpen((o) => !o)} aria-label="Open menu" aria-expanded={menuOpen}>
            <Icon name="menu" size={20} />
          </button>
          {pageHasHeading ? <p className="top-title">{heading}</p> : <h1 className="top-title">{heading}</h1>}
          <SearchBox />
          <div className="top-actions">
            {booksOn && (
              <a
                className="top-link"
                href="/bookshelf/"
                onClick={(e) => {
                  e.preventDefault()
                  openBookApp()
                }}
                title="Read and listen in Mediarium Books (opens in its own window)"
              >
                <Icon name="book" size={15} /> <span>eBooks/Audiobooks Player</span>
              </a>
            )}
            {homes.map((h) => (
              <a key={h.serverId} className="top-link" href={h.url} target="_blank" rel="noreferrer" title={`Open ${MEDIA_SERVER_BRAND[h.kind].label} (${h.name}) in a new tab`}>
                <MediaServerMark kind={h.kind} size={16} /> <span>{MEDIA_SERVER_BRAND[h.kind].label}</span>
              </a>
            ))}
            {admin && showSupport && (
              <a className="top-link icon-only support" href="https://ko-fi.com/ryanborg" target="_blank" rel="noreferrer" title="Support Mediarium" aria-label="Support Mediarium">
                <Icon name="heart" size={15} />
              </a>
            )}
            {demoEnabled() && (
              <span className="demo-chip" title="Sample data is showing. Turn it off from your profile menu.">
                <Icon name="flask" size={14} /> Demo data
              </span>
            )}
            {version &&
              (admin ? (
                <NavLink to="/settings/system" className="version-chip" title={updateReady ? `Mediarium ${version}. A newer version is available.` : `Mediarium ${version}`}>
                  <span className="version-name">Mediarium</span>
                  <span className="version-num">
                    v{version}
                    {updateReady && <span className="version-dot" aria-label="A newer version is available" />}
                  </span>
                </NavLink>
              ) : (
                <span className="version-chip" title={`Mediarium ${version}`}>
                  <span className="version-name">Mediarium</span>
                  <span className="version-num">v{version}</span>
                </span>
              ))}
            <ThemeToggle />
            <ProfileMenu />
          </div>
        </header>
        <BusyNotice />
        <ImportBanner />
        <main id="main" className="content" ref={mainRef} tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
