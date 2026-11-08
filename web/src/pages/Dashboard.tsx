import KindGlyph, { type GlyphKind } from '../components/KindGlyph'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, displayName, isAdmin, type LibraryStats, type CalendarEntry, type DashboardData, type DashFolder, type DashRecent, type HealthItem, type QueueItem } from '../api'
import { useAuth } from '../AuthContext'
import CalendarGrid, { KindBadge } from '../components/CalendarGrid'
import Icon, { type IconName } from '../components/Icon'
import InfoTip from '../components/InfoTip'
import HelpLink from '../components/HelpLink'
import Loading from '../components/Loading'
import PosterCard, { PosterFallback } from '../components/PosterCard'
import { describeState, type ItemStateKey } from '../components/state'
import { formatBytes, qualityText, timeAgo } from '../format'
import { locale, plural, st, t, tx } from '../i18n'
import { linePlace } from '../queueView'
import { ServerInfoCard, useServerStats } from '../components/ServerStats'
import { useModules } from '../ModulesContext'
import SetupGaps from '../components/SetupGaps'
import { DashboardUpdate } from '../components/UpdateCards'
import { entryDay, entryTime, readZonePref, resolveZone } from '../airTime'
import { dismiss, factsFromDashboard, isDismissed, setupGaps } from '../setupStatus'
import { useGridColumns } from '../useGridColumns'
import { useLive } from '../useLive'
import { anyMoving, pollEvery } from '../pollPlan'
import { useUnreadErrors } from '../useProblems'
import TwoStepTip from '../components/TwoStepTip'
import LeavingSoon from '../components/LeavingSoon'

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
function ModuleCard({ icon, glyph, label, value, unit, sub, to, color, off, soon }: { icon: IconName; glyph?: GlyphKind; label: string; value?: number; unit?: string; sub?: string; to: string; color: string; off?: boolean; soon?: boolean }) {
  const counted = useCountUp(value ?? 0)
  const dim = off || soon
  const body = (
    <>
      <span className="mod-top">
        <span className={`stat-icon${glyph ? ' has-glyph' : ''}`}>{glyph ? <KindGlyph kind={glyph} size={34} variant="solid" /> : <Icon name={icon} size={18} />}</span>
        <span className="mod-name">{label}</span>
        {soon ? <span className="badge mod-badge">{t('Coming soon')}</span> : off ? <span className="badge mod-badge">{t('Off')}</span> : null}
      </span>
      {soon ? (
        <span className="mod-value mod-empty">—</span>
      ) : (
        <span className="mod-value">
          {counted.toLocaleString(locale())}
          {unit && <small>{unit}</small>}
        </span>
      )}
      <span className="stat-sub">{soon ? t('Not built yet') : sub}</span>
    </>
  )
  // The wrapper draws the outline (it follows the notch in the corner); the card inside is the cut shape.
  const kind = glyph ?? 'movie'
  return (
    <div className={`mod-wrap mod-k-${kind}${dim ? ' dim' : ''}`} style={{ ['--tc' as string]: color }}>
      {dim ? (
        <div className="stat-card mod-card is-dim" aria-label={soon ? t('{label}: coming soon', { label }) : t('{label}: switched off', { label })}>
          {body}
        </div>
      ) : (
        <Link to={to} className="stat-card mod-card">
          {body}
        </Link>
      )}
    </div>
  )
}

const LEVEL_ICON: Record<HealthItem['level'], IconName> = { error: 'warning', warn: 'warning', info: 'info' }

function Health({ items, showOptional, errors = 0, onChanged }: { items: HealthItem[]; showOptional: boolean; errors?: number; onChanged?: () => void }) {
  const problems = items.filter((i) => i.level !== 'info')
  const optional = items.filter((i) => i.level === 'info')
  if (problems.length === 0 && errors === 0 && (!showOptional || optional.length === 0)) return null
  const row = (it: HealthItem) => (
    <li key={it.id} className={`health-item level-${it.level}`}>
      <span className="health-icon">
        <Icon name={LEVEL_ICON[it.level]} size={18} />
      </span>
      <div className="health-text">
        <strong>{st(it.title)}</strong>
        <span>{st(it.impact)}</span>
      </div>
      {it.action && (
        <Link to={it.action.path} className="health-action">
          {st(it.action.label)} <Icon name="open" size={14} />
        </Link>
      )}
      {it.dismiss && it.id === 'no-vpn' && (
        <button
          type="button"
          className="health-action"
          onClick={() => api.putSettings({ vpnNoticeAcknowledged: true }).then(() => onChanged?.()).catch(() => undefined)}
        >
          {t('My download client is already behind a VPN')}
        </button>
      )}
    </li>
  )
  return (
    <section className="card health">
      {(problems.length > 0 || errors > 0) && (
        <>
          <h2>{t('Needs your attention')}</h2>
          <ul>
            {errors > 0 && (
              <li className="health-item level-error">
                <span className="health-icon">
                  <Icon name="warning" size={18} />
                </span>
                <div className="health-text">
                  <strong>
                    {plural(errors, '{n} new error in the last 24 hours', '{n} new errors in the last 24 hours')}
                  </strong>
                  <span>{t('Something went wrong that Mediarium could not sort out by itself. The log says what happened and what to try.')}</span>
                </div>
                <Link to="/settings/logs" className="health-action">
                  {t('Open the log')} <Icon name="open" size={14} />
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
            {plural(optional.length, '{n} optional thing you could set up', '{n} optional things you could set up')}
          </summary>
          <ul>{optional.map(row)}</ul>
        </details>
      )}
    </section>
  )
}

// A card's media kind: "movies" for the main movies folder and for every
// extra movie folder (movies-extra-1 and so on).
const folderKind = (f: DashFolder) => f.key.split('-')[0]
const isExtraFolder = (f: DashFolder) => f.key.includes('-extra-')

// A folder's name and what it holds, in the language in use. A folder the
// page doesn't know keeps what the server calls it.
function folderLabel(f: DashFolder): string {
  if (isExtraFolder(f)) return folderKind(f) === 'movies' ? t('More movies') : t('More TV shows')
  switch (f.key) {
    case 'movies':
      return t('Movies')
    case 'tv':
      return t('TV shows')
    case 'music':
      return t('Music')
    case 'ebooks':
      return t('Ebooks')
    case 'audiobooks':
      return t('Audiobooks')
    case 'downloads':
      return t('Downloads')
  }
  return f.label
}

function folderItems(f: DashFolder): string {
  switch (folderKind(f)) {
    case 'movies':
      return plural(f.items, '{n} movie', '{n} movies')
    case 'tv':
      return plural(f.items, '{n} episode', '{n} episodes')
    case 'music':
      return plural(f.items, '{n} album', '{n} albums')
    case 'ebooks':
    case 'audiobooks':
      return plural(f.items, '{n} book', '{n} books')
  }
  return `${f.items.toLocaleString(locale())} ${f.itemsLabel}`
}

function FolderCard({ f }: { f: DashFolder }) {
  const used = f.totalBytes > 0 ? 1 - f.freeBytes / f.totalBytes : 0
  const tone = !f.exists || !f.writable ? 'bad' : used > 0.95 ? 'bad' : used > 0.85 ? 'warn' : ''
  const kind = folderKind(f)
  const icon: IconName = kind === 'movies' ? 'film' : kind === 'tv' ? 'tv' : kind === 'music' ? 'music' : kind === 'ebooks' ? 'book' : kind === 'audiobooks' ? 'headphones' : 'download'
  return (
    <div className={`folder-card${!f.exists || !f.writable ? ' broken' : ''}`}>
      <div className="folder-head">
        <span className="stat-icon">
          <Icon name={icon} size={18} />
        </span>
        <div style={{ minWidth: 0 }}>
          <strong>{folderLabel(f)}</strong>
          {f.path && <code title={f.path}>{f.path}</code>}
        </div>
      </div>
      <div className="folder-badges">
        {!f.exists ? (
          <span className="badge failed">{t('not found')}</span>
        ) : (
          <>
            <span className={`badge ${f.writable ? 'downloaded' : 'failed'}`}>{f.writable ? t('writable') : t('read-only')}</span>
            {f.mountKnown && <span className={`badge ${f.mounted ? 'downloaded' : 'missing'}`}>{f.mounted ? t('Mapped to your device') : t('not a mapped folder')}</span>}
            {f.hardlinks !== undefined && (
              <span className={`badge ${f.hardlinks ? 'downloaded' : ''}`} title={f.hardlinks ? t('Same drive as Downloads: imports use hardlinks, no extra space') : t('Different drive from Downloads: imports copy the file')}>
                {f.hardlinks ? t('hardlinks on') : t('copies on import')}
              </span>
            )}
          </>
        )}
        {f.newTitles && (
          <span className="badge downloading" title={t('A title added without a folder choice goes here (Settings > Library > Folders).')}>
            {t('new titles go here')}
          </span>
        )}
      </div>
      {f.exists && f.totalBytes > 0 && (
        <>
          <div className={`bar ${tone}`} title={t('{pct}% of the drive used', { pct: Math.round(used * 100) })}>
            <span style={{ width: `${Math.max(2, used * 100)}%` }} />
          </div>
          <div className="folder-meta">
            <span>{t('{free} free of {total}', { free: formatBytes(f.freeBytes), total: formatBytes(f.totalBytes) })}</span>
            {f.key !== 'downloads' && (
              <span>
                {folderItems(f)}
                {f.libraryBytes > 0 ? ` · ${formatBytes(f.libraryBytes)}` : ''}
              </span>
            )}
            {f.key === 'downloads' && <span>{t('{n} downloading', { n: f.items })}</span>}
          </div>
        </>
      )}
      {f.warnings.map((w) => (
        <p key={w} className="folder-warn">
          {st(w)}
        </p>
      ))}
    </div>
  )
}

function ActiveDownloads({ items }: { items: QueueItem[] }) {
  if (items.length === 0) {
    return (
      <div className="empty-inline">
        <Icon name="download" size={22} /> {t('Nothing is downloading right now.')}
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
// The day an item in "Coming up" sits on: an episode with a known air time goes by the viewer's own day.
function chipDay(e: CalendarEntry): Date {
  return new Date(`${entryDay(e, resolveZone(readZonePref()))}T00:00:00`)
}

function recentLink(r: DashRecent): string {
  if (r.kind === 'movie') return `/title/${r.tmdbId}`
  if (r.kind === 'album') return `/music/album/${r.albumId ?? r.id}`
  if (r.kind === 'book') return `/book/${r.bookId ?? r.id}`
  return `/series/${r.seriesId}`
}

// One row of posters that shows as many as fit the width, never a sideways
// scrollbar: the row is a grid of equal columns and only the first row is kept.
// Where a recently added title stands, on its poster.
// The poster states the dashboard's recent rows map onto.
const RECENT_KEY: Record<NonNullable<DashRecent['state']>, ItemStateKey> = { downloaded: 'downloaded', downloading: 'downloading', partial: 'partial', missing: 'searching', unmonitored: 'added' }

const RECENT_STATE: Record<NonNullable<DashRecent['state']>, string> = {
  downloaded: t('In library'),
  downloading: t('Downloading'),
  partial: t('Some episodes'),
  missing: t('Waiting for a release'),
  unmonitored: t('Not monitored'),
}

function RecentRow({ items, hide, when }: { items: DashRecent[]; hide?: DashRecent['state']; when?: boolean }) {
  const row = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(row, 6)
  return (
    <div className="recent-row" ref={row}>
      {items.slice(0, cols).map((r) => {
        const kind = r.kind === 'movie' ? 'movie' : r.kind === 'album' ? 'music' : r.kind === 'book' ? 'book' : 'tv'
        const st = r.state && r.state !== hide ? RECENT_KEY[r.state] : undefined
        return (
          <PosterCard
            key={`${r.kind}-${r.id}`}
            to={recentLink(r)}
            kind={kind}
            poster={r.posterUrl}
            title={r.title}
            meta={`${when && r.at ? timeAgo(r.at) : r.year || ''}${r.kind === 'series' ? ` · ${t('TV')}` : r.kind === 'album' ? ` · ${t('Music')}` : r.kind === 'book' ? ` · ${t('Book')}` : ''}`}
            state={st ? { ...describeState(st, undefined, r.state === 'partial' && kind === 'music' ? t('Some of the albums you want are downloaded.') : undefined), label: r.state === 'partial' && kind === 'music' ? t('Some albums') : RECENT_STATE[r.state!] } : undefined}
          />
        )
      })}
    </div>
  )
}

// Where the media shapes float behind the greeting.
const DRIFT: { k: GlyphKind; c: string; x: string; y: string; s: number; far?: boolean }[] = [
  { k: 'movie', c: 'movie', x: '40%', y: '-8%', s: 120 },
  { k: 'tv', c: 'tv', x: '58%', y: '48%', s: 110 },
  { k: 'music', c: 'music', x: '66%', y: '-22%', s: 140 },
  { k: 'book', c: 'book', x: '88%', y: '56%', s: 96 },
  { k: 'audiobook', c: 'audiobook', x: '26%', y: '58%', s: 84, far: true },
  { k: 'movie', c: 'movie', x: '70%', y: '22%', s: 64, far: true },
  { k: 'music', c: 'music', x: '48%', y: '74%', s: 58, far: true },
  { k: 'tv', c: 'tv', x: '93%', y: '36%', s: 54, far: true },
]

function greeting(): string {
  const h = new Date().getHours()
  return h < 5 ? t('Still up') : h < 12 ? t('Good morning') : h < 18 ? t('Good afternoon') : t('Good evening')
}

function Tile({ tone, icon, title, link, to, tip, children }: { tone?: string; icon: IconName; title: string; link?: string; to?: string; tip?: string; children: ReactNode }) {
  return (
    <section className={`tile${tone ? ` tone-${tone}` : ''}`}>
      <div className="tile-head">
        <span className="tile-ico">
          <Icon name={icon} size={18} />
        </span>
        <h2>{title}</h2>
        {tip && <InfoTip text={tip} />}
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
  const { user } = useAuth()
  const server = useServerStats(10000, isAdmin(user))
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
  // The dashboard moves quickly only while a download does.
  useLive(loadDashboard, pollEvery(anyMoving(active), 5000, 15000))

  useEffect(() => {
    api.calendar().then(setCal).catch(() => undefined)
  }, [])

  // Downloads move quickly, so they refresh on their own faster clock.
  const loadActive = useCallback(() => {
    api
      .listActiveQueue()
      .then(setActive)
      .catch(() => undefined)
  }, [])
  useEffect(loadActive, [loadActive])
  useLive(loadActive, pollEvery(anyMoving(active), 3000, 10000))

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
  const noBooks = { books: 0, downloaded: 0, missing: 0 }
  const ebooks = lib.ebooks ?? noBooks
  const audiobooks = lib.audiobooks ?? noBooks
  // The same number the Wanted page lists (monitored, not downloaded, already aired).
  const startedGaps = setupGaps(factsFromDashboard(data, { movies: on('movies'), tv: on('tv') }))
  const wanted = (lib.wanted?.movies ?? 0) + (lib.wanted?.episodes ?? 0) + (on('ebooks') ? ebooks.missing : 0) + (on('audiobooks') ? audiobooks.missing : 0)
  // Downloads waiting in line are not downloading yet.
  const inLine = active.filter((i) => i.status === 'queued').length
  const running = active.length - inLine
  const problems = data.health.filter((h) => h.level !== 'info').length
  const qualityTotal = lib.qualities.reduce((n, q) => n + q.count, 0)

  // Downloading, wanted, all good: with the server panel when there is one.
  const heroPills = (
        <div className="hero-pills">
          <Link to="/queue" className="hero-pill" style={{ ['--pc' as string]: 'var(--c-activity)' }}>
            <Icon name="download" size={15} /> {t('{n} downloading', { n: running })}
          </Link>
          <Link to="/wanted" className="hero-pill" style={{ ['--pc' as string]: 'var(--c-wanted)' }}>
            <Icon name="bookmark" size={15} /> {t('{n} wanted', { n: wanted })}
          </Link>
          {!admin ? null : problems > 0 ? (
            <a href="#attention" className="hero-pill" style={{ ['--pc' as string]: 'var(--danger)' }}>
              <Icon name="warning" size={15} /> {t('{n} to fix', { n: problems })}
            </a>
          ) : (
            <span className="hero-pill" style={{ ['--pc' as string]: 'var(--success)' }} title={t('Everything Mediarium needs is set up and working.')}>
              <Icon name="check" size={15} /> {t('All good')}
            </span>
          )}
          {data.health.some((h) => h.level === 'info') && (
            <button className="hero-pill" style={{ ['--pc' as string]: 'var(--c-settings)' }} onClick={() => setShowOptional((v) => !v)} aria-expanded={showOptional}>
              <Icon name="sliders" size={15} /> {t('{n} optional', { n: data.health.filter((h) => h.level === 'info').length })}
            </button>
          )}
        </div>
  )

  return (
    <div className="dashboard">
      <section className={`hero-welcome${server.denied ? '' : ' with-server'}`}>
        <div className="hero-drift" aria-hidden="true">
          {DRIFT.map((d, i) => (
            <span key={i} className={`drift-item${d.far ? ' far' : ''}`} style={{ left: d.x, top: d.y, color: `var(--c-${d.c})`, animationDelay: `${-i * 3.7}s` }}>
              <KindGlyph kind={d.k} size={d.s} variant="solid" />
            </span>
          ))}
        </div>
        <div className="hero-left">
        <div>
          <h1>
            {tx('{greeting}, {name}', { greeting: greeting(), name: <span>{name}</span> })}
          </h1>
          <p>
            {empty
              ? admin
                ? t('Welcome to Mediarium. Search for a movie or show in the bar above, or import what you already have.')
                : t('Welcome to Mediarium. Search for a movie or show in the bar above to add it.')
              : running > 0
                ? inLine > 0
                  ? plural(running, '{n} download is in progress right now, {waiting} waiting in line.', '{n} downloads are in progress right now, {waiting} waiting in line.', { waiting: inLine })
                  : plural(running, '{n} download is in progress right now.', '{n} downloads are in progress right now.')
                : inLine > 0
                  ? plural(inLine, '{n} download is waiting in line.', '{n} downloads are waiting in line.')
                : wanted > 0
                  ? plural(wanted, 'Nothing is downloading right now. {n} item is still missing.', 'Nothing is downloading right now. {n} items are still missing.')
                  : admin && problems > 0
                    ? t('Nothing is downloading and nothing is missing.')
                    : t('Everything is up to date.')}
          </p>
        </div>
          {server.denied && heroPills}
        </div>
        {!server.denied && (
          <div className="hero-server">
            {heroPills}
            <ServerInfoCard stats={server.stats} denied={server.denied} to={admin ? '/settings/system' : undefined} />
          </div>
        )}
      </section>

      {admin && <DashboardUpdate />}
      <TwoStepTip />

      {admin && !startedClosed && startedGaps.length > 0 && (
        <section className="card started-card" aria-labelledby="started-title">
          <div className="started-head">
            <h2 id="started-title">{t('Getting started')}</h2>
            <button
              type="button"
              className="icon-btn"
              aria-label={t('Hide Getting started')}
              title={t('Hide this')}
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
          <Health items={data.health} showOptional={showOptional} errors={unreadErrors} onChanged={loadDashboard} />
        </div>
      )}

      <div className="dash-cards">
        <ModuleCard icon="film" glyph="movie" label={t('Movies')} value={lib.movies.total} sub={t('{downloaded} downloaded · {missing} missing', { downloaded: lib.movies.downloaded, missing: lib.movies.missing })} to="/library?kind=movie" color="var(--c-movie)" off={!on('movies')} />
        <ModuleCard icon="tv" glyph="tv" label={t('TV shows')} value={lib.series.total} sub={t('{have} of {total} episodes', { have: lib.series.episodesDownloaded, total: lib.series.episodes })} to="/library?kind=tv" color="var(--c-tv)" off={!on('tv')} />
        <ModuleCard
          icon="music"
          glyph="music"
          label={t('Music')}
          value={music.artists}
          unit={plural(music.artists, 'artist', 'artists')}
          sub={plural(music.albums, '{n} album · {missing} missing', '{n} albums · {missing} missing', { missing: music.missing })}
          to="/library?kind=music"
          color="var(--c-music)"
          off={!on('music')}
        />
        <ModuleCard
          icon="book"
          glyph="book"
          label={t('Ebooks')}
          value={ebooks.books}
          unit={plural(ebooks.books, 'book', 'books')}
          sub={t('{downloaded} downloaded · {missing} missing', { downloaded: ebooks.downloaded, missing: ebooks.missing })}
          to="/library?kind=ebook"
          color="var(--c-book)"
          off={!on('ebooks')}
        />
        <ModuleCard
          icon="headphones"
          glyph="audiobook"
          label={t('Audiobooks')}
          value={audiobooks.books}
          unit={plural(audiobooks.books, 'book', 'books')}
          sub={t('{downloaded} downloaded · {missing} missing', { downloaded: audiobooks.downloaded, missing: audiobooks.missing })}
          to="/library?kind=audiobook"
          color="var(--c-audiobook)"
          off={!on('audiobooks')}
        />
      </div>

      {!empty && data.recentlyAdded.length > 0 && (
        <div className="recent-split">
          <Tile tone="library" icon="check" title={t('Just added to your library')} tip={t('The latest titles whose files have arrived in your library.')} link={t('Library')} to="/library">
            {(data.recentInLibrary ?? []).length > 0 ? (
              <RecentRow items={data.recentInLibrary ?? []} hide="downloaded" when />
            ) : (
              <div className="empty-inline">
                <Icon name="film" size={22} /> {t('Nothing has arrived yet.')}
              </div>
            )}
          </Tile>
          <Tile tone="wanted" icon="clock" title={t('Added, still to come')} tip={t('Titles you added recently that are still waiting for a release or for their release date. They download by themselves once a release is found.')} link={t('Wanted')} to="/wanted">
            {(data.recentToCome ?? []).length > 0 ? (
              <RecentRow items={data.recentToCome ?? []} hide="missing" />
            ) : (
              <div className="empty-inline">
                <Icon name="check" size={22} /> {t('Everything you added is in your library.')}
              </div>
            )}
          </Tile>
        </div>
      )}

      <div className="bento">
        <Tile tone="activity" icon="download" title={t('Downloading now')} tip={t('Downloads that are running or waiting in line. Open Activity to pause, stop or retry one.')} link={t('Activity')} to="/queue">
          <ActiveDownloads items={active} />
        </Tile>

        <Tile tone="calendar" icon="calendar" title={t('Coming up')} tip={t('Releases and episodes due soon for the titles in your library.')} link={t('Calendar')} to="/calendar">
          {data.upcoming.length === 0 ? (
            <div className="empty-inline">
              <Icon name="calendar" size={22} /> {t('Nothing is scheduled.')}
            </div>
          ) : (
            <ul className="mini-list compact">
              {data.upcoming.map((e) => (
                <li key={`${e.kind}-${e.id}`}>
                  <Link className="mini-link" to={e.kind === 'movie' ? `/title/${e.tmdbId}` : e.kind === 'episode' ? `/series/${e.seriesId}` : `/music/artist/${e.artistId}`}>
                  <span className="date-chip">
                    <b>{chipDay(e).getDate()}</b>
                    {chipDay(e).toLocaleString(locale(), { month: 'short' })}
                  </span>
                  <KindBadge kind={e.kind} subtitle={e.subtitle} />
                  <div className="mini-main">
                    <strong>{e.title}</strong>
                    {e.subtitle && (
                      <small className="qwhen">
                        {e.subtitle.split(' · ')[0]}
                        {entryTime(e, resolveZone(readZonePref())) ? ` · ${entryTime(e, resolveZone(readZonePref()))}` : ''}
                      </small>
                    )}
                  </div>
                  <span className={`badge ${e.status}`}>{e.status === 'missing' ? t('Waiting') : e.status === 'downloaded' ? t('Have it') : e.status === 'downloading' ? t('Downloading') : e.status}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </Tile>
      </div>

      {on('lifecycle') && <LeavingSoon />}

      <section className="tile tone-settings wide-tile folders-tile">
        <div className="tile-head">
          <span className="tile-ico">
            <Icon name="folder" size={18} />
          </span>
          <h2>{t('Connected folders')}</h2>
          <HelpLink topic="install-docker#why-one-data-folder-hardlinks" text={t('The folders Mediarium uses, whether it can write to them and how much room is left. "Hardlinks on" means an import takes no extra space, because Downloads and the library are on the same drive.')} />
          {admin && (
            <Link className="tile-link" to="/settings/media">
              {t('Change')} <Icon name="open" size={13} />
            </Link>
          )}
        </div>
        <div className="folder-grid">
          {data.folders.filter((f) => !f.off).map((f) => (
            <FolderCard key={f.key} f={f} />
          ))}
        </div>
        {data.folders.some((f) => f.off) && (
          <p className="folders-off">
            {t('Switched off: {names}', { names: data.folders.filter((f) => f.off).map((f) => f.label).join(', ') })}
            {admin && (
              <>
                {' · '}
                <Link to="/settings/media">{t('Media types')}</Link>
              </>
            )}
          </p>
        )}
      </section>

      <section className="tile tone-calendar wide-tile">
        <div className="tile-head">
          <span className="tile-ico">
            <Icon name="calendar" size={18} />
          </span>
          <h2>{t('Calendar')}</h2>
          <Link className="tile-link" to="/calendar">
            {t('Full page')} <Icon name="open" size={13} />
          </Link>
        </div>
        <CalendarGrid entries={cal} compact />
      </section>

      <div className="dash-pair">
      {qualityTotal > 0 && (
        <section className="tile tone-library">
          <div className="tile-head">
            <span className="tile-ico">
              <Icon name="star" size={18} />
            </span>
            <h2>{t('Quality breakdown')}</h2>
            <InfoTip text={t('How many of the files in your library are in each quality.')} />
            {admin && (
              <Link className="tile-link" to="/settings/quality">
                {t('Profiles')} <Icon name="open" size={13} />
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
      <StatsTile />
      </div>
    </div>
  )
}

// A look at the statistics page: the space the library takes, how much
// came from Usenet, and downloads over the last six months.
function StatsTile() {
  const [s, setS] = useState<LibraryStats | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    api
      .libraryStats()
      .then(setS)
      .catch(() => setFailed(true))
  }, [])
  const months = (s?.months ?? []).slice(-6)
  const top = Math.max(1, ...months.map((m) => m.completed))
  return (
    <section className="tile tone-activity stats-tile">
      <div className="tile-head">
        <span className="tile-ico">
          <Icon name="activity" size={18} />
        </span>
        <h2>{t('Statistics')}</h2>
        <InfoTip text={t('The space your library takes, the share that came from Usenet, and downloads over the last six months.')} />
        <Link className="tile-link" to="/settings/statistics">
          {t('Full page')} <Icon name="open" size={13} />
        </Link>
      </div>
      {failed ? (
        <p className="hint">{t('The numbers could not be loaded. Try the full page.')}</p>
      ) : !s ? (
        <div className="skeleton" style={{ height: 120 }} />
      ) : (
        <div className="stats-glance">
          <div className="stats-figures">
            <div>
              <span className="mono">{t('Space used')}</span>
              <b>{formatBytes(s.movieBytes + s.episodeBytes)}</b>
            </div>
            <div>
              <span className="mono">{t('From Usenet')}</span>
              <b>{s.usenetShare}%</b>
            </div>
          </div>
          {months.length > 0 && (
            <div className="stats-months" role="img" aria-label={t('Downloads in the last six months')}>
              {months.map((m) => (
                <div key={m.month} className="stats-month" title={plural(m.completed, '{n} download', '{n} downloads')}>
                  <span className="stats-bar" style={{ height: `${Math.max(4, (m.completed / top) * 100)}%` }} />
                  <small className="mono">{new Date(`${m.month}-01T00:00:00`).toLocaleString(locale(), { month: 'short' })}</small>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
