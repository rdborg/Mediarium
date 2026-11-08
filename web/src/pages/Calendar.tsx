import { useEffect, useState } from 'react'
import { api, isViewer, type CalendarEntry } from '../api'
import { useAuth } from '../AuthContext'
import CalendarGrid from '../components/CalendarGrid'
import Icon from '../components/Icon'
import KindGlyph, { type GlyphKind } from '../components/KindGlyph'
import Loading from '../components/Loading'
import { useToast } from '../components/Toast'
import { copyText } from '../components/CopyBox'
import { useKinds, useModules } from '../ModulesContext'
import { t, tx } from '../i18n'
import { useUrlState } from '../useUrlState'
import { COMMON_ZONES, deviceZone, readZonePref, resolveZone, writeZonePref } from '../airTime'
import { useLive } from '../useLive'

type Show = 'all' | 'movie' | 'episode' | 'album' | 'book'
const SHOW_LABEL: Record<Show, string> = { all: t('All'), movie: t('Movies'), episode: t('TV'), album: t('Music'), book: t('Books') }

// Monitored movie releases, episode airs and album releases on one month
// view, with simple filters and a link to add it to a calendar app.
// Each filter wears its kind's glyph and colour, as on Discover and Library.
const SHOW_GLYPH: Record<Exclude<Show, 'all'>, GlyphKind> = { movie: 'movie', episode: 'tv', album: 'music', book: 'book' }
const SHOW_COLOR: Record<Exclude<Show, 'all'>, string> = { movie: 'var(--c-movie)', episode: 'var(--c-tv)', album: 'var(--c-music)', book: 'var(--c-book)' }

export default function Calendar() {
  const [entries, setEntries] = useState<CalendarEntry[] | null>(null)
  const [error, setError] = useState('')
  // The kind and the "not downloaded" tick live in the address, so Back returns to them.
  const [ui, patchUi] = useUrlState({ show: 'all', missing: '', film: '' })
  const show: Show = (['movie', 'episode', 'album', 'book'] as const).find((k) => k === ui.show) ?? 'all'
  const setShow = (k: Show) => patchUi({ show: k })
  const missingOnly = ui.missing === '1'
  // Films by the release they show for (discussion #170): every one, or only cinema or only digital releases.
  const film: '' | 'cinema' | 'digital' = ui.film === 'cinema' || ui.film === 'digital' ? ui.film : ''
  const setMissingOnly = (v: boolean) => patchUi({ missing: v ? '1' : '' })
  const [feedOpen, setFeedOpen] = useState(false)
  // Which time zone the days and times are read in: this device's, or one picked here (kept in this browser only).
  const [zonePref, setZonePref] = useState(readZonePref)
  const zone = resolveZone(zonePref)
  const pickZone = (z: string) => {
    setZonePref(z)
    writeZonePref(z)
  }
  // The private calendar address belongs to an account that can change things.
  const viewer = isViewer(useAuth().user)
  const kinds = useKinds()
  const { on } = useModules()

  useEffect(() => {
    api
      .calendar()
      .then(setEntries)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useLive(() => api.calendar().then(setEntries).catch(() => undefined), 30000)

  const options: Show[] = ['all', ...(kinds.includes('movie') ? (['movie'] as const) : []), ...(kinds.includes('tv') ? (['episode'] as const) : []), ...(kinds.includes('music') ? (['album'] as const) : []), ...(on('ebooks') || on('audiobooks') ? (['book'] as const) : [])]
  const shown = (entries ?? []).filter((e) => (show === 'all' || e.kind === show) && (!missingOnly || e.status !== 'downloaded') && (film === '' || e.kind !== 'movie' || e.release === 'both' || (e.release ?? 'cinema') === film))
  const filmsShown = (show === 'all' || show === 'movie') && kinds.includes('movie')

  return (
    <div>
      <div className="page-header">
        <h1>{t('Calendar')}</h1>
        {!viewer && (
          <button className="btn-with-icon" onClick={() => setFeedOpen((v) => !v)} aria-expanded={feedOpen}>
            <Icon name="calendar" size={15} /> {t('Add to your calendar app')}
          </button>
        )}
      </div>
      {feedOpen && !viewer && <FeedPanel />}
      {error && <p className="error-text">{error}</p>}
      {entries === null && !error && <Loading height={420} />}
      {entries !== null && (
        <>
          <div className="toolbar filter-bar cal-filters" style={{ marginBottom: 18 }}>
            {options.length > 2 && (
              <div className="seg kind-tabs">
                {options.map((k) => (
                  <button key={k} className={`k-${k}${show === k ? ' active' : ''}`} style={k === 'all' ? undefined : { ['--kc' as string]: SHOW_COLOR[k] }} onClick={() => setShow(k)}>
                    {k === 'all' ? <Icon name="grid" size={15} /> : <KindGlyph kind={SHOW_GLYPH[k]} size={17} />}
                    {SHOW_LABEL[k]}
                  </button>
                ))}
              </div>
            )}
            <label className="inline-field" style={{ marginLeft: options.length > 2 ? 8 : 0 }}>
              <input type="checkbox" checked={missingOnly} onChange={(e) => setMissingOnly(e.target.checked)} />
              {t('Not downloaded yet only')}
            </label>
            {filmsShown && (
              <label className="inline-field">
                {t('Films')}
                <select value={film} onChange={(e) => patchUi({ film: e.target.value })}>
                  <option value="">{t('Every release')}</option>
                  <option value="cinema">{t('In cinemas')}</option>
                  <option value="digital">{t('Digital release')}</option>
                </select>
              </label>
            )}
          </div>
          {entries.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing is scheduled yet. Monitored movies and shows appear here once their release or air dates are known.')}</p>}
          <CalendarGrid entries={shown} zone={zone} />
          <div className="cal-foot">
            {entries.some((e) => e.kind === 'episode' && e.airAt) && <ZonePicker pref={zonePref} onPick={pickZone} />}
            <p className="field-hint cal-credit">
              {tx('Air times from {link}', {
                link: (
                  <a href="https://www.tvmaze.com" target="_blank" rel="noopener noreferrer">
                    TVmaze
                  </a>
                ),
              })}
            </p>
          </div>
        </>
      )}
    </div>
  )
}

// Which time zone episode days and times are shown in. Episodes TVmaze has a time for move to the day they
// air in this zone; the choice stays in this browser.
function ZonePicker({ pref, onPick }: { pref: string; onPick: (zone: string) => void }) {
  const zones = pref && !COMMON_ZONES.includes(pref) ? [pref, ...COMMON_ZONES] : COMMON_ZONES
  return (
    <label className="inline-field cal-zone">
      {t('Show times in')}
      <select value={pref} onChange={(e) => onPick(e.target.value)}>
        <option value="">{t('This device ({zone})', { zone: deviceZone() })}</option>
        {zones.map((z) => (
          <option key={z} value={z}>
            {z === 'UTC' ? t('UTC') : z.replace(/_/g, ' ')}
          </option>
        ))}
      </select>
    </label>
  )
}

// The calendar feed: a private address that Google, Apple or Outlook
// calendars subscribe to.
function FeedPanel() {
  const toast = useToast()
  const [path, setPath] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api
      .calendarFeed()
      .then((f) => setPath(f.on ? (f.path ?? '') : ''))
      .catch(() => setPath(''))
  }, [])
  const url = path ? `${window.location.origin}${path}` : ''

  async function run(fn: () => Promise<{ on: boolean; path?: string }>, done: string) {
    setBusy(true)
    try {
      const f = await fn()
      setPath(f.on ? (f.path ?? '') : '')
      toast.success(done)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (path === null) return null
  return (
    <div className="card feed-panel">
      {url ? (
        <>
          <p style={{ marginTop: 0 }}>
            {tx('Add this address to your calendar app as a subscription (in Google Calendar: {google}; on an iPhone: {iphone}). It updates by itself.', {
              google: <em>{t('Other calendars → From URL')}</em>,
              iphone: <em>{t('Settings → Calendar → Accounts → Add Subscribed Calendar')}</em>,
            })}
          </p>
          <div className="feed-url">
            <code>{url}</code>
            <button className="btn-sm" onClick={() => void copyText(url).then((ok) => (ok ? toast.success(t('Address copied.')) : toast.error(t('Copy did not work. Select the address and copy it by hand.'))))}>
              {t('Copy')}
            </button>
          </div>
          <p className="field-hint">
            {t('Anyone with this address can see your calendar, so keep it to yourself. A new address stops the old one working.')}
          </p>
          <div className="row-actions">
            <button className="btn-sm" disabled={busy} onClick={() => void run(() => api.newCalendarFeed(), t('New address made. Update it in your calendar app.'))}>
              {t('Make a new address')}
            </button>
            <button className="btn-sm" disabled={busy} onClick={() => void run(() => api.removeCalendarFeed(), t('The calendar address is turned off.'))}>
              {t('Turn it off')}
            </button>
          </div>
          {!window.location.protocol.startsWith('https') && <p className="field-hint">{t('Calendar apps on the internet can only reach this address if Mediarium is reachable from outside, for example through your reverse proxy.')}</p>}
        </>
      ) : (
        <>
          <p style={{ marginTop: 0 }}>{t('Get a private address that calendar apps (Google, Apple, Outlook) can subscribe to, so releases and episodes show up in your own calendar.')}</p>
          <button className="primary" disabled={busy} onClick={() => void run(() => api.newCalendarFeed(), t('Your calendar address is ready.'))}>
            {t('Make my calendar address')}
          </button>
        </>
      )}
    </div>
  )
}
