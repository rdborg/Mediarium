import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import type { CalendarEntry } from '../api'
import { useModules } from '../ModulesContext'
import Icon from './Icon'

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

function entryLink(e: CalendarEntry): string | undefined {
  if (e.kind === 'episode' && e.seriesId) return `/series/${e.seriesId}`
  if (e.kind === 'movie' && e.tmdbId) return `/title/${e.tmdbId}`
  if (e.kind === 'album' && e.albumId) return `/music/album/${e.albumId}`
  if (e.kind === 'book' && e.bookId) return `/book/${e.bookId}`
  return undefined
}

// One badge per kind of media, told apart by shape of the symbol and by
// colour (movies teal, shows violet, music orange, books green, the same
// everywhere in the app).
const KIND = {
  movie: { icon: 'film', label: 'Movie', cls: 'movie' },
  episode: { icon: 'tv', label: 'TV episode', cls: 'tv' },
  album: { icon: 'music', label: 'Album', cls: 'music' },
  book: { icon: 'book', label: 'Book', cls: 'book' },
} as const

const STATUS_LABEL: Record<CalendarEntry['status'], string> = { downloaded: 'Have it', downloading: 'Downloading', missing: 'Waiting' }

export function KindBadge({ kind }: { kind: CalendarEntry['kind'] }) {
  const k = KIND[kind] ?? KIND.movie
  return (
    <span className={`cal-kind kind-${k.cls}`} role="img" aria-label={k.label} title={k.label}>
      <Icon name={k.icon} size={13} />
    </span>
  )
}

function Chip({ e }: { e: CalendarEntry }) {
  const k = KIND[e.kind] ?? KIND.movie
  const body = (
    <>
      <KindBadge kind={e.kind} />
      <span className="cal-chip-title">{e.title}</span>
    </>
  )
  const cls = `cal-chip st-${e.status} kind-${k.cls}`
  const tip = `${e.title}${e.subtitle ? ` - ${e.subtitle}` : ''} (${STATUS_LABEL[e.status] ?? e.status})`
  const to = entryLink(e)
  return to ? (
    <Link to={to} className={cls} title={tip}>
      {body}
    </Link>
  ) : (
    <span className={cls} title={tip}>
      {body}
    </span>
  )
}

// Month grid on wide screens, agenda list on phones. Each entry has a stripe
// for its status (waiting, downloading, have it) and a badge for its kind.
export default function CalendarGrid({ entries, compact }: { entries: CalendarEntry[]; compact?: boolean }) {
  const [openDay, setOpenDay] = useState<string | null>(null)
  const musicOn = useModules().on('music')
  const [cursor, setCursor] = useState(() => {
    const d = new Date()
    return new Date(d.getFullYear(), d.getMonth(), 1)
  })

  const byDay = useMemo(() => {
    const m = new Map<string, CalendarEntry[]>()
    for (const e of entries) {
      const list = m.get(e.releaseDate) ?? []
      list.push(e)
      m.set(e.releaseDate, list)
    }
    return m
  }, [entries])

  const cells = useMemo(() => {
    const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1)
    const lead = (first.getDay() + 6) % 7
    const start = new Date(first)
    start.setDate(1 - lead)
    const days = Math.ceil((lead + new Date(cursor.getFullYear(), cursor.getMonth() + 1, 0).getDate()) / 7) * 7
    return Array.from({ length: days }, (_, i) => {
      const d = new Date(start)
      d.setDate(start.getDate() + i)
      return d
    })
  }, [cursor])

  const today = iso(new Date())
  const monthLabel = cursor.toLocaleString(undefined, { month: 'long', year: 'numeric' })
  const monthDays = cells.filter((d) => d.getMonth() === cursor.getMonth() && (byDay.get(iso(d))?.length ?? 0) > 0)
  const shift = (n: number) => setCursor(new Date(cursor.getFullYear(), cursor.getMonth() + n, 1))
  const cap = compact ? 2 : 3

  return (
    <div className="cal">
      <div className="cal-head">
        <button className="icon-btn" onClick={() => shift(-1)} aria-label="Previous month">
          <span style={{ display: 'inline-flex', transform: 'rotate(180deg)' }}>
            <Icon name="open" size={16} />
          </span>
        </button>
        <h3>{monthLabel}</h3>
        <button className="icon-btn" onClick={() => shift(1)} aria-label="Next month">
          <Icon name="open" size={16} />
        </button>
        <button className="btn-sm" onClick={() => setCursor(new Date(new Date().getFullYear(), new Date().getMonth(), 1))}>
          Today
        </button>
        <div className="cal-legend" aria-label="Legend">
          <div className="cal-legend-group">
            {(['downloaded', 'downloading', 'missing'] as const).map((st) => (
              <span key={st} className="cal-legend-item">
                <i className={`cal-dot st-${st}`} /> {STATUS_LABEL[st]}
              </span>
            ))}
          </div>
          <div className="cal-legend-group">
            <span className="cal-legend-item">
              <KindBadge kind="movie" /> Movie
            </span>
            <span className="cal-legend-item">
              <KindBadge kind="episode" /> TV episode
            </span>
            {musicOn && (
              <span className="cal-legend-item">
                <KindBadge kind="album" /> Album
              </span>
            )}
          </div>
        </div>
      </div>

      <div className="cal-grid" role="grid" aria-label={monthLabel}>
        {WEEKDAYS.map((w) => (
          <div key={w} className="cal-dow">
            {w}
          </div>
        ))}
        {cells.map((d) => {
          const key = iso(d)
          const list = byDay.get(key) ?? []
          const out = d.getMonth() !== cursor.getMonth()
          return (
            <div key={key} className={`cal-cell${out ? ' out' : ''}${key === today ? ' today' : ''}${list.length ? ' has' : ''}`}>
              <span className="cal-num">{d.getDate()}</span>
              {(openDay === key ? list : list.slice(0, cap)).map((e) => (
                <Chip key={`${e.kind}-${e.id}`} e={e} />
              ))}
              {list.length > cap && (
                <button className="cal-more" onClick={() => setOpenDay(openDay === key ? null : key)} aria-expanded={openDay === key}>
                  {openDay === key ? 'Show less' : `+${list.length - cap} more`}
                </button>
              )}
            </div>
          )
        })}
      </div>

      <ul className="cal-agenda">
        {monthDays.length === 0 && <li className="empty-inline">Nothing scheduled this month.</li>}
        {monthDays.map((d) => {
          const key = iso(d)
          return (
            <li key={key} className={key === today ? 'today' : ''}>
              <span className="date-chip">
                <b>{d.getDate()}</b>
                {d.toLocaleString(undefined, { weekday: 'short' })}
              </span>
              <div className="cal-agenda-items">
                {(byDay.get(key) ?? []).map((e) => (
                  <div key={`${e.kind}-${e.id}`} className="cal-agenda-row">
                    <Chip e={e} />
                    {e.subtitle && <small>{e.subtitle}</small>}
                  </div>
                ))}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
