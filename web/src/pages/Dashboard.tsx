import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, displayName, isAdmin, type CalendarEntry, type DashboardData, type DashFolder, type DashRecent, type HealthItem, type QueueItem } from '../api'
import { useAuth } from '../AuthContext'
import CalendarGrid, { KindBadge } from '../components/CalendarGrid'
import Icon, { type IconName } from '../components/Icon'
import Loading from '../components/Loading'
import Cover from '../components/Cover'
import { PosterFallback } from '../components/PosterCard'
import { formatBytes, qualityText, timeAgo } from '../format'
import { linePlace } from '../queueView'
import { ServerInfoCard, useServerStats } from '../components/ServerStats'
import { useModules } from '../ModulesContext'
import SetupGaps from '../components/SetupGaps'
import { DashboardUpdate } from '../components/UpdateCards'
import { dismiss, factsFromDashboard, isDismissed, setupGaps } from '../setupStatus'
import { useGridColumns } from '../useGridColumns'
import { useLive } from '../useLive'
import { useUnreadErrors } from '../useProblems'

// Counts up to its target once, so the numbers feel alive when the page opens.
function useCountUp(target: number, ms = 700): number {
  const [value, setValue] = useState(0)
  const from = useRef(0)
  useEffect(() => {
    if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
      from.current = target
      setValue(target)
      return
    }
    const start = performance.now()
    const begin = from.current
    let raf = 0
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / ms)
      const eased = 1 - Math.pow(1 - t, 3)
      setValue(Math.round(begin + (target - begin) * eased))
      if (t < 1) raf = requestAnimationFrame(tick)
      else from.current = target
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [target, ms])
  return value
}

// One card per kind of media. A kind that is switched off keeps its card,
// dimmed with an "Off" badge and the count it had, so it is easy to switch back
// on; one that is not built yet says "Coming soon".
function ModuleCard({ icon, label, value, unit, sub, to, color, off, soon }: { icon: IconName; label: string; value?: number; unit?: string; sub?: string; to: string; color: string; off?: boolean; soon?: boolean }) {
  const counted = useCountUp(value ?? 0)
  const dim = off || soon
  const body = (
    <>
      <span className="mod-top">
        <span className="stat-icon">
          <Icon name={icon} size={18} />
        </span>
        <span className="mod-name">{label}</span>
        {soon ? <span className="badge mod-badge">Coming soon</span> : off ? <span className="badge mod-badge">Off</span> : null}
      </span>
      {soon ? (
        <span className="mod-value mod-empty">—</span>
      ) : (
        <span className="mod-value">
          {counted.toLocaleString()}
          {unit && <small>{unit}</small>}
        </span>
      )}
      <span className="stat-sub">{soon ? 'Not built yet' : sub}</span>
    </>
  )
  return dim ? (
    <div className="stat-card mod-card is-dim" style={{ ['--tc' as string]: color }} aria-label={soon ? `${label}: coming soon` : `${label}: switched off`}>
      {body}
    </div>
  ) : (
    <Link to={to} className="stat-card mod-card" style={{ ['--tc' as string]: color }}>
      {body}
    </Link>
  )
}

const LEVEL_ICON: Record<HealthItem['level'], IconName> = { error: 'warning', warn: 'warning', info: 'info' }

function Health({ items, showOptional, errors = 0 }: { items: HealthItem[]; showOptional: boolean; errors?: number }) {
  const problems = items.filter((i) => i.level !== 'info')
  const optional = items.filter((i) => i.level === 'info')
  if (problems.length === 0 && errors === 0 && (!showOptional || optional.length === 0)) return null
  const row = (it: HealthItem) => (
    <li key={it.id} className={`health-item level-${it.level}`}>
      <span className="health-icon">
        <Icon name={LEVEL_ICON[it.level]} size={18} />
      </span>
      <div className="health-text">
        <strong>{it.title}</strong>
        <span>{it.impact}</span>
      </div>
      {it.action && (
        <Link to={it.action.path} className="health-action">
          {it.action.label} <Icon name="open" size={14} />
        </Link>
      )}
    </li>
  )
  return (
    <section className="card health">
      {(problems.length > 0 || errors > 0) && (
        <>
          <h2>Needs your attention</h2>
          <ul>
            {errors > 0 && (
              <li className="health-item level-error">
                <span className="health-icon">
                  <Icon name="warning" size={18} />
                </span>
                <div className="health-text">
                  <strong>
                    {errors === 1 ? '1 new error' : `${errors} new errors`} in the last 24 hours
                  </strong>
                  <span>Something went wrong that Mediarium could not sort out by itself. The log says what happened and what to try.</span>
                </div>
                <Link to="/settings/logs" className="health-action">
                  Open the log <Icon name="open" size={14} />
                </Link>
              </li>
            )}
            {problems.map(row)}
          </ul>
        </>
      )}
      {optional.length > 0 && (
        <details open={showOptional}>
          <summary>
            {optional.length} optional {optional.length === 1 ? 'thing' : 'things'} you could set up
          </summary>
          <ul>{optional.map(row)}</ul>
        </details>
      )}
    </section>
  )
}

function FolderCard({ f }: { f: DashFolder }) {
  const used = f.totalBytes > 0 ? 1 - f.freeBytes / f.totalBytes : 0
  const tone = !f.exists || !f.writable ? 'bad' : used > 0.95 ? 'bad' : used > 0.85 ? 'warn' : ''
  const icon: IconName = f.key === 'movies' ? 'film' : f.key === 'tv' ? 'tv' : 'download'
  return (
    <div className={`folder-card${!f.exists || !f.writable ? ' broken' : ''}`}>
      <div className="folder-head">
        <span className="stat-icon">
          <Icon name={icon} size={18} />
        </span>
        <div style={{ minWidth: 0 }}>
          <strong>{f.label}</strong>
          {f.path && <code title={f.path}>{f.path}</code>}
        </div>
      </div>
      <div className="folder-badges">
        {!f.exists ? (
          <span className="badge failed">not found</span>
        ) : (
          <>
            <span className={`badge ${f.writable ? 'downloaded' : 'failed'}`}>{f.writable ? 'writable' : 'read-only'}</span>
            {f.mountKnown && <span className={`badge ${f.mounted ? 'downloaded' : 'missing'}`}>{f.mounted ? 'Mapped to your device' : 'not a mapped folder'}</span>}
            {f.hardlinks !== undefined && (
              <span className={`badge ${f.hardlinks ? 'downloaded' : ''}`} title={f.hardlinks ? 'Same drive as Downloads: imports use hardlinks, no extra space' : 'Different drive from Downloads: imports copy the file'}>
                {f.hardlinks ? 'hardlinks on' : 'copies on import'}
              </span>
            )}
          </>
        )}
      </div>
      {f.exists && f.totalBytes > 0 && (
        <>
          <div className={`bar ${tone}`} title={`${Math.round(used * 100)}% of the drive used`}>
            <span style={{ width: `${Math.max(2, used * 100)}%` }} />
          </div>
          <div className="folder-meta">
            <span>{formatBytes(f.freeBytes)} free of {formatBytes(f.totalBytes)}</span>
            {f.key !== 'downloads' && <span>{f.items.toLocaleString()} {f.itemsLabel} · {formatBytes(f.libraryBytes)}</span>}
            {f.key === 'downloads' && <span>{f.items} downloading</span>}
          </div>
        </>
      )}
      {f.warnings.map((w) => (
        <p key={w} className="folder-warn">
          {w}
        </p>
      ))}
    </div>
  )
}

function ActiveDownloads({ items }: { items: QueueItem[] }) {
  if (items.length === 0) {
    return (
      <div className="empty-inline">
        <Icon name="download" size={22} /> Nothing is downloading right now.
      </div>
    )
  }
  return (
    <ul className="mini-list">
      {items.slice(0, 6).map((q) => (
        <li key={q.id}>
          <div className="mini-thumb">{q.posterUrl ? <img src={q.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</div>
          <div className="mini-main">
            <div className="mini-title">
              <strong>{q.title}</strong>
              {q.subtitle && <span className="badge">{q.subtitle}</span>}
              <span className={`badge ${q.status}`}>{q.status === 'queued' ? linePlace(q.queuePosition) : q.status}</span>
            </div>
            {q.status !== 'queued' && (
              <div className="qprogress">
                <div className="bar active">
                  <span style={{ width: `${Math.max(3, q.progressPct)}%` }} />
                </div>
                <small>
                  {q.progressPct.toFixed(0)}%{q.sizeBytes ? ` · ${formatBytes(q.sizeBytes)}` : ''}
                </small>
              </div>
            )}
          </div>
        </li>
      ))}
    </ul>
  )
}

// Where a recently added or downloaded entry opens: a movie, a show, or the
// artist page of an album.
function recentLink(r: DashRecent): string {
  if (r.kind === 'movie') return `/title/${r.tmdbId}`
  if (r.kind === 'album') return `/music/album/${r.albumId ?? r.id}`
  return `/series/${r.seriesId}`
}

// One row of posters that shows as many as fit the width, never a sideways
// scrollbar: the row is a grid of equal columns and only the first row is kept.
function RecentRow({ items }: { items: DashRecent[] }) {
  const row = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(row, 6)
  return (
    <div className="recent-row" ref={row}>
      {items.slice(0, cols).map((r) => (
        <Link key={`${r.kind}-${r.id}`} className={`mini-poster kind-${r.kind === 'movie' ? 'movie' : r.kind === 'album' ? 'music' : 'tv'}`} to={recentLink(r)}>
          <span className="mini-art">{r.kind === 'album' ? <Cover src={r.posterUrl} /> : r.posterUrl ? <img src={r.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</span>
          <strong>{r.title}</strong>
          <small>
            {r.year || ''} {r.kind === 'series' ? '· TV' : r.kind === 'album' ? '· Music' : ''}
          </small>
        </Link>
      ))}
    </div>
  )
}

function greeting(): string {
  const h = new Date().getHours()
  return h < 5 ? 'Still up' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
}

function Tile({ tone, icon, title, link, to, children }: { tone?: string; icon: IconName; title: string; link?: string; to?: string; children: ReactNode }) {
  return (
    <section className={`tile${tone ? ` tone-${tone}` : ''}`}>
      <div className="tile-head">
        <span className="tile-ico">
          <Icon name={icon} size={18} />
        </span>
        <h2>{title}</h2>
        {link && to && (
          <Link className="tile-link" to={to}>
            {link} <Icon name="open" size={13} />
          </Link>
        )}
      </div>
      <div className="tile-body">{children}</div>
    </section>
  )
}

export default function Dashboard() {
  const server = useServerStats()
  const { user } = useAuth()
  const { on } = useModules()
  const [data, setData] = useState<DashboardData | null>(null)
  const [active, setActive] = useState<QueueItem[]>([])
  const [cal, setCal] = useState<CalendarEntry[]>([])
  const [error, setError] = useState('')
  const [showOptional, setShowOptional] = useState(false)
  // "Getting started" stays away once it is closed (remembered in this browser).
  const [startedClosed, setStartedClosed] = useState(() => (user ? isDismissed(user.id) : false))

  const loadDashboard = useCallback(() => {
    api
      .dashboard()
      .then((d) => {
        setData(d)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(loadDashboard, [loadDashboard])
  useLive(loadDashboard, 5000)

  useEffect(() => {
    api.calendar().then(setCal).catch(() => undefined)
  }, [])

  // Downloads move quickly, so they refresh on their own faster clock.
  const loadActive = useCallback(() => {
    api
      .listQueue()
      .then((q) => setActive(q.filter((i) => i.status === 'queued' || i.status === 'downloading' || i.status === 'importing')))
      .catch(() => undefined)
  }, [])
  useEffect(loadActive, [loadActive])
  useLive(loadActive, 3000)

  const name = displayName(user)
  // Members get a trimmed dashboard: no setup warnings, no server paths and
  // no links into settings they cannot open.
  const admin = isAdmin(user)
  const unreadErrors = useUnreadErrors(admin)

  // One failed refresh must not replace a dashboard that is already showing.
  if (error && !data) return <p className="error-text">{error}</p>
  if (!data) {
    return (
      <div className="dashboard">
        <Loading height={130}>
          <div className="skeleton" style={{ height: 130, borderRadius: 22, marginBottom: 22 }} />
          <div className="dash-cards">
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="skeleton" style={{ height: 118 }} />
            ))}
          </div>
        </Loading>
      </div>
    )
  }

  const { library: lib } = data
  const empty = lib.movies.total === 0 && lib.series.total === 0
  const music = lib.music ?? { artists: 0, albums: 0, downloaded: 0, missing: 0 }
  // The same number the Wanted page lists (monitored, not downloaded, already aired).
  const startedGaps = setupGaps(factsFromDashboard(data, { movies: on('movies'), tv: on('tv') }))
  const wanted = (lib.wanted?.movies ?? 0) + (lib.wanted?.episodes ?? 0)
  // Downloads waiting in line are not downloading yet.
  const inLine = active.filter((i) => i.status === 'queued').length
  const running = active.length - inLine
  const problems = data.health.filter((h) => h.level !== 'info').length
  const qualityTotal = lib.qualities.reduce((n, q) => n + q.count, 0)

  return (
    <div className="dashboard">
      <section className="hero-welcome">
        <div>
          <h1>
            {greeting()}, <span>{name}</span>
          </h1>
          <p>
            {empty
              ? admin
                ? 'Welcome to Mediarium. Search for a movie or show in the bar above, or import what you already have.'
                : 'Welcome to Mediarium. Search for a movie or show in the bar above to add it.'
              : running > 0
                ? `${running} ${running === 1 ? 'download is' : 'downloads are'} in progress right now${inLine > 0 ? `, ${inLine} waiting in line` : ''}.`
                : inLine > 0
                  ? `${inLine} ${inLine === 1 ? 'download is' : 'downloads are'} waiting in line.`
                : wanted > 0
                  ? `Nothing is downloading right now. ${wanted} ${wanted === 1 ? 'item is' : 'items are'} still missing.`
                  : 'Everything is up to date.'}
          </p>
        </div>
        <div className="hero-pills">
          <Link to="/queue" className="hero-pill" style={{ ['--pc' as string]: 'var(--c-activity)' }}>
            <Icon name="download" size={15} /> {running} downloading
          </Link>
          <Link to="/wanted" className="hero-pill" style={{ ['--pc' as string]: 'var(--c-wanted)' }}>
            <Icon name="bookmark" size={15} /> {wanted} wanted
          </Link>
          {!admin ? null : problems > 0 ? (
            <a href="#attention" className="hero-pill" style={{ ['--pc' as string]: 'var(--danger)' }}>
              <Icon name="warning" size={15} /> {problems} to fix
            </a>
          ) : (
            <span className="hero-pill" style={{ ['--pc' as string]: 'var(--success)' }} title="Everything Mediarium needs is set up and working.">
              <Icon name="check" size={15} /> All good
            </span>
          )}
          {data.health.some((h) => h.level === 'info') && (
            <button className="hero-pill" style={{ ['--pc' as string]: 'var(--c-settings)' }} onClick={() => setShowOptional((v) => !v)} aria-expanded={showOptional}>
              <Icon name="sliders" size={15} /> {data.health.filter((h) => h.level === 'info').length} optional
            </button>
          )}
        </div>
      </section>

      {admin && <DashboardUpdate />}

      {admin && !startedClosed && startedGaps.length > 0 && (
        <section className="card started-card" aria-labelledby="started-title">
          <div className="started-head">
            <h2 id="started-title">Getting started</h2>
            <button
              type="button"
              className="icon-btn"
              aria-label="Hide Getting started"
              title="Hide this"
              onClick={() => {
                if (user) dismiss(user.id)
                setStartedClosed(true)
              }}
            >
              <Icon name="x" size={16} />
            </button>
          </div>
          <SetupGaps gaps={startedGaps} />
        </section>
      )}

      {admin && (
        <div id="attention">
          <Health items={data.health} showOptional={showOptional} errors={unreadErrors} />
        </div>
      )}

      <div className={`dash-cards${server.denied ? ' no-server' : ''}`}>
        <ModuleCard icon="film" label="Movies" value={lib.movies.total} sub={`${lib.movies.downloaded} downloaded · ${lib.movies.missing} missing`} to="/library?kind=movie" color="var(--c-movie)" off={!on('movies')} />
        <ModuleCard icon="tv" label="TV shows" value={lib.series.total} sub={`${lib.series.episodesDownloaded} of ${lib.series.episodes} episodes`} to="/library?kind=tv" color="var(--c-tv)" off={!on('tv')} />
        <ModuleCard
          icon="music"
          label="Music"
          value={music.artists}
          unit={music.artists === 1 ? 'artist' : 'artists'}
          sub={`${music.albums} ${music.albums === 1 ? 'album' : 'albums'} · ${music.missing} missing`}
          to="/library?kind=music"
          color="var(--c-music)"
          off={!on('music')}
        />
        <ServerInfoCard stats={server.stats} denied={server.denied} to={admin ? '/settings/system' : undefined} />
      </div>
      <p className="dash-more">
        <Link to="/stats">See statistics: quality, space and downloads by month</Link>
      </p>

      {!empty && data.recentlyAdded.length > 0 && (
        <section className="rail">
          <div className="rail-head">
            <h2>Recently added</h2>
            <Link to="/library">See library</Link>
          </div>
          <RecentRow items={data.recentlyAdded} />
        </section>
      )}

      <div className="bento">
        <Tile tone="activity" icon="download" title="Downloading now" link="Activity" to="/queue">
          <ActiveDownloads items={active} />
        </Tile>

        <Tile tone="library" icon="check" title="Recently downloaded" link="History" to="/queue">
          {data.recentDownloads.length === 0 ? (
            <div className="empty-inline">
              <Icon name="check" size={22} /> Nothing has finished downloading yet.
            </div>
          ) : (
            <ul className="mini-list">
              {data.recentDownloads.slice(0, 5).map((r) => (
                <li key={r.id}>
                  <div className="mini-thumb">{r.kind === 'album' ? <Cover src={r.posterUrl} /> : r.posterUrl ? <img src={r.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</div>
                  <div className="mini-main">
                    <div className="mini-title">
                      <Link to={recentLink(r)}>
                        <strong>{r.title}</strong>
                      </Link>
                      {r.subtitle && <span className="badge">{r.subtitle}</span>}
                      {r.quality && <span className="badge">{r.quality}</span>}
                    </div>
                    <small className="qwhen">
                      {timeAgo(r.at)}
                      {r.sizeBytes ? ` · ${formatBytes(r.sizeBytes)}` : ''}
                    </small>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Tile>

        <Tile tone="settings" icon="folder" title="Connected folders" link={admin ? 'Change' : undefined} to={admin ? '/settings/media' : undefined}>
          <div className="folder-list">
            {data.folders.map((f) => (
              <FolderCard key={f.key} f={f} />
            ))}
          </div>
        </Tile>

        <Tile tone="calendar" icon="calendar" title="Coming up" link="Calendar" to="/calendar">
          {data.upcoming.length === 0 ? (
            <div className="empty-inline">
              <Icon name="calendar" size={22} /> Nothing is scheduled.
            </div>
          ) : (
            <ul className="mini-list compact">
              {data.upcoming.map((e) => (
                <li key={`${e.kind}-${e.id}`}>
                  <span className="date-chip">
                    <b>{new Date(`${e.releaseDate}T00:00:00`).getDate()}</b>
                    {new Date(`${e.releaseDate}T00:00:00`).toLocaleString(undefined, { month: 'short' })}
                  </span>
                  <KindBadge kind={e.kind} />
                  <div className="mini-main">
                    <strong>{e.title}</strong>
                    {e.subtitle && <small className="qwhen">{e.subtitle.split(' · ')[0]}</small>}
                  </div>
                  <span className={`badge ${e.status}`}>{e.status === 'missing' ? 'Waiting' : e.status === 'downloaded' ? 'Have it' : e.status === 'downloading' ? 'Downloading' : e.status}</span>
                </li>
              ))}
            </ul>
          )}
        </Tile>
      </div>

      <section className="tile tone-calendar wide-tile">
        <div className="tile-head">
          <span className="tile-ico">
            <Icon name="calendar" size={18} />
          </span>
          <h2>Calendar</h2>
          <Link className="tile-link" to="/calendar">
            Full page <Icon name="open" size={13} />
          </Link>
        </div>
        <CalendarGrid entries={cal} compact />
      </section>

      {qualityTotal > 0 && (
        <section className="tile tone-library wide-tile" style={{ marginTop: 22 }}>
          <div className="tile-head">
            <span className="tile-ico">
              <Icon name="star" size={18} />
            </span>
            <h2>Quality breakdown</h2>
            {admin && (
              <Link className="tile-link" to="/settings/quality">
                Profiles <Icon name="open" size={13} />
              </Link>
            )}
          </div>
          <ul className="quality-bars">
            {lib.qualities.slice(0, 7).map((q) => (
              <li key={q.tier}>
                <span>{qualityText(q.tier)}</span>
                <div className="bar">
                  <span style={{ width: `${(q.count / qualityTotal) * 100}%` }} />
                </div>
                <b>{q.count}</b>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
