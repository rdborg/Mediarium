import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { CalendarEntry } from '../api'
import { useModules } from '../ModulesContext'
import Icon from './Icon'
import KindGlyph, { type GlyphKind } from './KindGlyph'
import { locale, plural, t } from '../i18n'
import { dayInZone, entryTime, groupByDay, readZonePref, resolveZone } from '../airTime'
import { useUrlState } from '../useUrlState'
import { VIEW_KEY, layoutFor, parseView, pickDay, type View } from '../calendarView'
import '../calendar.css'

// The calendar reads like a cinema's programme: the month in big type, a
// grid of days where each release is a small poster in its kind's frame, or
// a list of days with a ticket stub for the date. Status follows the rest of
// the app: solid when you have it, a bright line while it downloads, a
// dashed outline in the kind's colour while it's still wanted.

// The width (in pixels) up to which calendar.css shows only the list of days.
const NARROW_PANEL = 640


const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

function entryLink(e: CalendarEntry): string | undefined {
  if (e.kind === 'episode' && e.seriesId) return `/series/${e.seriesId}`
  if (e.kind === 'movie' && e.tmdbId) return `/title/${e.tmdbId}`
  if (e.kind === 'album' && e.albumId) return `/music/album/${e.albumId}`
  if (e.kind === 'book' && e.bookId) return `/book/${e.bookId}`
  return undefined
}

// A book entry is an audiobook when that's the only format wanted; the
// server ends the subtitle with the formats ("Audiobook", "Ebook and audiobook").
function glyphOf(kind: CalendarEntry['kind'], subtitle?: string): GlyphKind {
  if (kind === 'episode') return 'tv'
  if (kind === 'album') return 'music'
  if (kind === 'book') return subtitle && /(^|· )Audiobook$/.test(subtitle) ? 'audiobook' : 'book'
  return 'movie'
}

const KIND_LABEL: Record<GlyphKind, string> = { movie: t('Movie'), tv: t('TV episode'), music: t('Album'), book: t('Book'), audiobook: t('Audiobook') }
const LEGEND_LABEL: Record<GlyphKind, string> = { movie: t('Movies'), tv: t('TV'), music: t('Music'), book: t('Books'), audiobook: t('Audiobooks') }
const STATUS_LABEL: Record<CalendarEntry['status'], string> = { downloaded: t('Have it'), downloading: t('Downloading'), missing: t('Waiting') }

// A movie that is on the calendar twice says which of its dates this is.
const releaseLabel = (e: CalendarEntry) => (e.release === 'cinema' ? t('Cinema') : e.release === 'digital' ? t('Digital') : e.release === 'both' ? t('Cinema and digital') : undefined)

// "S03E09 · Second Service" splits into the code and the episode's name.
function splitSubtitle(e: CalendarEntry): { code?: string; rest?: string } {
  const label = releaseLabel(e)
  if (label) return { rest: e.subtitle ? `${label} · ${e.subtitle}` : label }
  const m = e.subtitle?.match(/^(S\d+E\d+)(?: · (.*))?$/)
  if (m) return { code: m[1], rest: m[2] }
  if (e.season && e.episode) return { code: `S${String(e.season).padStart(2, '0')}E${String(e.episode).padStart(2, '0')}`, rest: e.subtitle }
  return { rest: e.subtitle }
}

// The kind's glyph on a small tile of its colour (the dashboard's list uses
// it too).
export function KindBadge({ kind, subtitle }: { kind: CalendarEntry['kind']; subtitle?: string }) {
  const g = glyphOf(kind, subtitle)
  return (
    <span className={`prog-badge k-${g}`} role="img" aria-label={KIND_LABEL[g]} title={KIND_LABEL[g]}>
      <KindGlyph kind={g} size={16} />
    </span>
  )
}

// A small poster in the frame of its kind: a ticket with notches, a CRT
// screen, a sleeve with its record, a book with a spine, a cover wearing
// headphones. Without a picture the frame holds the kind's glyph.
function Thumb({ e, g }: { e: CalendarEntry; g: GlyphKind }) {
  const [brokenSrc, setBrokenSrc] = useState<string | undefined>()
  return (
    <span className={`prog-thumb f-${g}`} aria-hidden="true">
      {e.posterUrl && brokenSrc !== e.posterUrl ? (
        <img src={e.posterUrl} alt="" loading="lazy" decoding="async" onError={() => setBrokenSrc(e.posterUrl)} />
      ) : (
        <span className="prog-thumb-blank">
          <KindGlyph kind={g} size={18} />
        </span>
      )}
    </span>
  )
}

function Entry({ e, past, card, zone }: { e: CalendarEntry; past: boolean; card?: boolean; zone: string }) {
  const g = glyphOf(e.kind, e.subtitle)
  const { code, rest } = splitSubtitle(e)
  const time = entryTime(e, zone)
  const late = past && e.status === 'missing'
  const cls = `${card ? 'prog-card' : 'prog-slot'} k-${g} s-${e.status}${late ? ' late' : ''}`
  const tip = `${e.title}${e.subtitle ? ` · ${e.subtitle}` : ''}${releaseLabel(e) ? ` · ${releaseLabel(e)}` : ''}${time ? ` · ${time}` : ''} (${STATUS_LABEL[e.status] ?? e.status})`
  const body = card ? (
    <>
      <Thumb e={e} g={g} />
      <span className="prog-card-text">
        <span className="prog-card-title">{e.title}</span>
        {(code || rest) && (
          <span className="prog-card-sub">
            {code && <span className="prog-code">{code}</span>}
            {time && <span className="prog-time">{time}</span>}
            {rest && <span className="prog-rest">{rest}</span>}
          </span>
        )}
        <span className="prog-card-foot">
          <span className="prog-kind">
            <KindGlyph kind={g} size={14} />
            {KIND_LABEL[g]}
          </span>
          <span className="prog-state">{STATUS_LABEL[e.status] ?? e.status}</span>
        </span>
      </span>
    </>
  ) : (
    <>
      <Thumb e={e} g={g} />
      <span className="prog-slot-text">
        <span className="prog-slot-title">{e.title}</span>
        <span className="prog-slot-meta">
          <KindGlyph kind={g} size={12} />
          {code ?? releaseLabel(e) ?? KIND_LABEL[g]}
          {time && <span className="prog-time">{time}</span>}
        </span>
      </span>
      <span className="sr-only">
        {KIND_LABEL[g]}, {STATUS_LABEL[e.status] ?? e.status}
      </span>
    </>
  )
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

// One release as a big poster with its name in a frosted bubble: the Posters view of the calendar.
// In the Backdrops view (wide) it is a wide still with the title's logo on it
// instead, once both have been fetched; before that it falls back to the poster.
function PosterTile({ e, past, wide, zone }: { e: CalendarEntry; past: boolean; wide?: boolean; zone: string }) {
  const g = glyphOf(e.kind, e.subtitle)
  const { code, rest } = splitSubtitle(e)
  const time = entryTime(e, zone)
  const [brokenSrc, setBrokenSrc] = useState<string | undefined>()
  const [brokenLogo, setBrokenLogo] = useState(false)
  const late = past && e.status === 'missing'
  const src = wide && e.backdropUrl ? e.backdropUrl : e.posterUrl
  const logo = wide && e.backdropUrl && e.logoUrl && !brokenLogo ? e.logoUrl : undefined
  const cls = `pt k-${g} s-${e.status}${late ? ' late' : ''}${wide ? ' wide' : ''}`
  const tip = `${e.title}${e.subtitle ? ` · ${e.subtitle}` : ''} (${STATUS_LABEL[e.status] ?? e.status})`
  const body = (
    <>
      {src && brokenSrc !== src ? (
        <img src={src} alt="" loading="lazy" decoding="async" onError={() => setBrokenSrc(src)} />
      ) : (
        <span className="pt-blank" aria-hidden="true">
          <KindGlyph kind={g} size={34} />
        </span>
      )}
      {logo && <img className="pt-logo" src={logo} alt="" loading="lazy" decoding="async" onError={() => setBrokenLogo(true)} />}
      <span className={`pt-bubble${logo ? ' has-logo' : ''}`}>
        <span className={logo ? 'sr-only' : 'pt-title'}>{e.title}</span>
        <span className="pt-meta">
          <KindGlyph kind={g} size={12} />
          {code ?? rest ?? KIND_LABEL[g]}
          {time && <span className="prog-time">{time}</span>}
          <span className="pt-state">{STATUS_LABEL[e.status] ?? e.status}</span>
        </span>
      </span>
    </>
  )
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

function storedView(): View {
  try {
    return parseView(localStorage.getItem(VIEW_KEY))
  } catch {
    return 'month'
  }
}

// zone is the time zone the days and times are read in (an IANA name); without one, the choice saved in this browser, else the device's.
export default function CalendarGrid({ entries, compact, zone: zoneProp }: { entries: CalendarEntry[]; compact?: boolean; zone?: string }) {
  const { on } = useModules()
  const zone = zoneProp ?? resolveZone(readZonePref())
  // The list of days is only built while it can be seen: when it is the chosen view, or when the panel is too narrow
  // for the month grid (calendar.css then always shows the list). A month of releases is hundreds of cards.
  const root = useRef<HTMLDivElement>(null)
  const [narrow, setNarrow] = useState(false)
  useLayoutEffect(() => {
    const el = root.current
    if (!el) return
    const measure = () => setNarrow(el.clientWidth <= NARROW_PANEL + 48) // a little extra for the panel's padding
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const watch = new ResizeObserver(measure)
    watch.observe(el)
    return () => watch.disconnect()
  }, [])
  const [openDay, setOpenDay] = useState<string | null>(null)
  // The day picked on the compact month (a phone's Month view).
  const [pickedDay, setPickedDay] = useState<string | null>(null)
  const [view, setView] = useState<View>(storedView)
  const [slide, setSlide] = useState<'next' | 'prev' | ''>('')
  // On the Calendar page the month is in the address (?month=2026-10), so Back returns to it; the small
  // calendar on the dashboard keeps it to itself.
  const [ui, patchUi] = useUrlState({ month: '' })
  const [own, setOwn] = useState(() => {
    const d = new Date()
    return new Date(d.getFullYear(), d.getMonth(), 1)
  })
  const picked = !compact && /^\d{4}-\d{2}$/.test(ui.month) ? new Date(Number(ui.month.slice(0, 4)), Number(ui.month.slice(5)) - 1, 1) : null
  const cursor = picked ?? own
  const setCursor = (d: Date) => {
    setOwn(d)
    if (!compact) patchUi({ month: `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}` })
  }

  // An episode with a known air time sits on the day it airs in the viewer's zone; the rest stay on their date.
  const byDay = useMemo(() => groupByDay(entries, zone), [entries, zone])

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

  // Weekday names in the reader's language, Monday first (1 Jan 2024 was a Monday).
  const weekdays = useMemo(() => Array.from({ length: 7 }, (_, i) => new Date(2024, 0, 1 + i).toLocaleString(locale(), { weekday: 'short' })), [])

  const now = new Date()
  const today = dayInZone(now, zone)
  const monthKey = `${cursor.getFullYear()}-${cursor.getMonth()}`
  const monthName = cursor.toLocaleString(locale(), { month: 'long' })
  const monthDays = cells.filter((d) => d.getMonth() === cursor.getMonth() && (byDay.get(iso(d))?.length ?? 0) > 0)
  const monthCount = monthDays.reduce((n, d) => n + (byDay.get(iso(d))?.length ?? 0), 0)
  // The list keeps today in it, even on a quiet day, so you can see where you are.
  const agendaDays = cells.filter((d) => d.getMonth() === cursor.getMonth() && ((byDay.get(iso(d))?.length ?? 0) > 0 || iso(d) === today))
  const cap = compact ? 2 : 3

  const shift = (n: number) => {
    setSlide(n > 0 ? 'next' : 'prev')
    setOpenDay(null)
    setCursor(new Date(cursor.getFullYear(), cursor.getMonth() + n, 1))
  }
  const currentMonth = new Date(Number(today.slice(0, 4)), Number(today.slice(5, 7)) - 1, 1)
  const goToday = () => {
    const target = currentMonth
    if (target.getTime() === cursor.getTime()) return
    setSlide(target > cursor ? 'next' : 'prev')
    setOpenDay(null)
    setCursor(target)
  }
  const pickView = (v: View) => {
    setView(v)
    try {
      localStorage.setItem(VIEW_KEY, v)
    } catch {
      // A private window may refuse; the choice just isn't remembered.
    }
  }

  // The legend shows the kinds that are switched on, plus any kind that has
  // an entry this month.
  const present = new Set(entries.map((e) => glyphOf(e.kind, e.subtitle)))
  const legendKinds = (['movie', 'tv', 'music', 'book', 'audiobook'] as const).filter(
    (k) => present.has(k) || (k === 'movie' && on('movies')) || (k === 'tv' && on('tv')) || (k === 'music' && on('music')) || (k === 'book' && on('ebooks')) || (k === 'audiobook' && on('audiobooks')),
  )

  const Heading = compact ? 'h3' : 'h2'
  const tiles = view === 'posters' || view === 'backdrops' // a big picture for each day
  const fullDate = (d: Date) => d.toLocaleDateString(locale(), { weekday: 'long', day: 'numeric', month: 'long' })
  const layout = layoutFor(view, narrow)
  const monthIso = `${cursor.getFullYear()}-${String(cursor.getMonth() + 1).padStart(2, '0')}`
  const chosenDay = pickDay(pickedDay, monthIso, today, [...byDay.keys()].filter((k) => (byDay.get(k)?.length ?? 0) > 0))

  return (
    <div ref={root} className={`prog${compact ? ' compact' : ''}`} data-view={view}>
      <div className="prog-head">
        <div className="prog-title">
          <Heading className="prog-month">
            {monthName} <span className="prog-year">{cursor.getFullYear()}</span>
          </Heading>
          <p className="prog-count">{monthCount === 0 ? t('Nothing scheduled this month.') : plural(monthCount, '{n} release this month', '{n} releases this month')}</p>
        </div>
        <div className="prog-controls">
          <div className="prog-nav">
            <div className="prog-console">
              <button type="button" className="prog-round" onClick={() => shift(-1)} aria-label={t('Previous month')}>
                <span className="prog-back">
                  <Icon name="open" size={16} />
                </span>
              </button>
              <span className="prog-label" aria-live="polite">
                {cursor.toLocaleString(locale(), { month: 'short', year: 'numeric' })}
              </span>
              <button type="button" className="prog-round" onClick={() => shift(1)} aria-label={t('Next month')}>
                <Icon name="open" size={16} />
              </button>
            </div>
            <button type="button" className="prog-today" onClick={goToday} disabled={cursor.getTime() === currentMonth.getTime()}>
              {t('Today')}
            </button>
          </div>
          <div className="seg prog-views" role="group" aria-label={t('Calendar view')}>
            <button type="button" className={view === 'month' ? 'active' : ''} aria-pressed={view === 'month'} onClick={() => pickView('month')}>
              {t('Month')}
            </button>
            <button type="button" className={view === 'posters' ? 'active' : ''} aria-pressed={view === 'posters'} onClick={() => pickView('posters')}>
              {t('Posters')}
            </button>
            <button type="button" className={view === 'backdrops' ? 'active' : ''} aria-pressed={view === 'backdrops'} onClick={() => pickView('backdrops')}>
              {t('Backdrops')}
            </button>
            <button type="button" className={view === 'agenda' ? 'active' : ''} aria-pressed={view === 'agenda'} onClick={() => pickView('agenda')}>
              {t('Agenda')}
            </button>
          </div>
        </div>
      </div>

      {layout === 'dots' && (
        <>
          <div className="prog-board prog-board-dots">
            <div className="prog-dows" aria-hidden="true">
              {weekdays.map((w, i) => (
                <span key={w} className={i >= 5 ? 'wkend' : ''}>
                  {w.slice(0, 2)}
                </span>
              ))}
            </div>
            <div key={monthKey} className={`prog-grid prog-dotgrid${slide ? ` slide-${slide}` : ''}`}>
              {cells.map((d, i) => {
                const key = iso(d)
                const list = byDay.get(key) ?? []
                const out = d.getMonth() !== cursor.getMonth()
                const cls = ['prog-dotcell', out && 'out', key < today && 'past', key === today && 'today', i % 7 >= 5 && 'wkend', key === chosenDay && 'chosen'].filter(Boolean).join(' ')
                return (
                  <button
                    key={key}
                    type="button"
                    className={cls}
                    disabled={out}
                    aria-pressed={key === chosenDay}
                    aria-label={`${fullDate(d)}${key === today ? `, ${t('Today')}` : ''}`}
                    onClick={() => setPickedDay(key)}
                  >
                    <span className="prog-num" aria-hidden="true">
                      {d.getDate()}
                    </span>
                    <span className="prog-dots" aria-hidden="true">
                      {list.slice(0, 4).map((e) => (
                        <i key={`${e.kind}-${e.id}-${e.release ?? ''}`} className={`prog-dot k-${glyphOf(e.kind, e.subtitle)} s-${e.status}`} />
                      ))}
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
          <section className="prog-picked" aria-label={fullDate(new Date(Number(chosenDay.slice(0, 4)), Number(chosenDay.slice(5, 7)) - 1, Number(chosenDay.slice(8, 10))))}>
            <h3 className="prog-picked-title">
              {fullDate(new Date(Number(chosenDay.slice(0, 4)), Number(chosenDay.slice(5, 7)) - 1, Number(chosenDay.slice(8, 10))))}
              {chosenDay === today ? ` · ${t('Today')}` : ''}
            </h3>
            {(byDay.get(chosenDay) ?? []).length === 0 ? (
              <p className="prog-quiet prog-quiet-flat">{chosenDay === today ? t('Nothing comes out today.') : t('Nothing comes out this day.')}</p>
            ) : (
              <ul className="prog-cards prog-cards-flat">
                {(byDay.get(chosenDay) ?? []).map((e) => (
                  <li key={`${e.kind}-${e.id}-${e.release ?? ''}`}>
                    <Entry e={e} past={chosenDay < today} card zone={zone} />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}

      {layout === 'grid' && (
      <div className="prog-board">
        <div className="prog-dows" aria-hidden="true">
          {weekdays.map((w, i) => (
            <span key={w} className={i >= 5 ? 'wkend' : ''}>
              {w}
            </span>
          ))}
        </div>
        <div key={monthKey} className={`prog-grid${tiles ? ' posters' : ''}${view === 'backdrops' ? ' backdrops' : ''}${slide ? ` slide-${slide}` : ''}`}>
          {cells.map((d, i) => {
            const key = iso(d)
            const list = byDay.get(key) ?? []
            const out = d.getMonth() !== cursor.getMonth()
            const past = key < today
            const open = openDay === key
            const cls = ['prog-cell', out && 'out', past && 'past', key === today && 'today', i % 7 >= 5 && 'wkend', list.length > 0 && 'has'].filter(Boolean).join(' ')
            return (
              <div key={key} className={cls}>
                <div className="prog-cell-head">
                  <span className="prog-num" aria-hidden="true">
                    {d.getDate()}
                  </span>
                  {key === today && <span className="prog-lamp" aria-hidden="true" />}
                  <span className="sr-only">
                    {fullDate(d)}
                    {key === today ? `, ${t('Today')}` : ''}
                  </span>
                </div>
                {tiles && list.length > 0 && (
                  <div className="pt-day">
                    <PosterTile e={list[0]} past={past} wide={view === 'backdrops'} zone={zone} />
                    {list.length > 1 && (
                      <button type="button" className="pt-more" onClick={() => setOpenDay(open ? null : key)} aria-expanded={open}>
                        {t('+{n} more', { n: list.length - 1 })}
                      </button>
                    )}
                    {open && list.length > 1 && (
                      <div className={`pt-pop${i % 7 <= 1 ? ' at-start' : i % 7 >= 5 ? ' at-end' : ''}${Math.floor(i / 7) >= 3 ? ' up' : ''}`} role="dialog" aria-label={fullDate(d)}>
                        <div className="pt-pop-head">
                          <strong>{fullDate(d)}</strong>
                          <button type="button" className="icon-btn" aria-label={t('Close')} onClick={() => setOpenDay(null)}>
                            ×
                          </button>
                        </div>
                        <div className="pt-pop-grid">
                          {list.map((e) => (
                            <PosterTile key={`${e.kind}-${e.id}-${e.release ?? ''}`} e={e} past={past} wide={view === 'backdrops'} zone={zone} />
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}
                {!tiles && list.length > 0 && (
                  <div className="prog-slots">
                    {(open ? list : list.slice(0, cap)).map((e) => (
                      <Entry key={`${e.kind}-${e.id}-${e.release ?? ''}`} e={e} past={past} zone={zone} />
                    ))}
                  </div>
                )}
                {!tiles && list.length > cap && (
                  <button type="button" className="prog-more" onClick={() => setOpenDay(open ? null : key)} aria-expanded={open}>
                    {open ? t('Show less') : t('+{n} more', { n: list.length - cap })}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      </div>
      )}

      {(layout === 'agenda' || layout === 'day-tiles' || layout === 'day-wide') && (
      <ol key={`a-${monthKey}-${layout}`} className={`prog-agenda prog-${layout}${slide ? ` slide-${slide}` : ''}`}>
        {agendaDays.map((d) => {
          const key = iso(d)
          const past = key < today
          const list = byDay.get(key) ?? []
          return (
            <li key={key} className={`prog-day${key === today ? ' today' : ''}${past ? ' past' : ''}`}>
              <div className="prog-stub">
                <span className="prog-stub-num">{d.getDate()}</span>
                <span className="prog-stub-wd">{key === today ? t('Today') : d.toLocaleString(locale(), { weekday: 'short' })}</span>
                <span className="prog-stub-mon">{d.toLocaleString(locale(), { month: 'short' })}</span>
                <span className="sr-only">{fullDate(d)}</span>
              </div>
              {list.length === 0 ? (
                <p className="prog-quiet">{t('Nothing comes out today.')}</p>
              ) : layout === 'agenda' ? (
                <ul className="prog-cards">
                  {list.map((e) => (
                    <li key={`${e.kind}-${e.id}-${e.release ?? ''}`}>
                      <Entry e={e} past={past} card zone={zone} />
                    </li>
                  ))}
                </ul>
              ) : (
                <ul className={`prog-tiles${layout === 'day-wide' ? ' wide' : ''}`}>
                  {list.map((e) => (
                    <li key={`${e.kind}-${e.id}-${e.release ?? ''}`}>
                      <PosterTile e={e} past={past} wide={layout === 'day-wide'} zone={zone} />
                    </li>
                  ))}
                </ul>
              )}
            </li>
          )
        })}
      </ol>
      )}

      <div className="prog-legend" role="group" aria-label={t('Legend')}>
        <ul className="prog-legend-kinds">
          {legendKinds.map((k) => (
            <li key={k} className={`k-${k}`}>
              <KindGlyph kind={k} size={18} />
              {LEGEND_LABEL[k]}
            </li>
          ))}
        </ul>
        <ul className="prog-legend-states">
          {(['downloaded', 'downloading', 'missing'] as const).map((st) => (
            <li key={st}>
              <i className={`prog-swatch s-${st}`} aria-hidden="true" />
              {STATUS_LABEL[st]}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
