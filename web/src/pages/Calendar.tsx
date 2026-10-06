import { useEffect, useState } from 'react'
import { api, type CalendarEntry } from '../api'
import CalendarGrid from '../components/CalendarGrid'
import Icon from '../components/Icon'
import Loading from '../components/Loading'
import { useToast } from '../components/Toast'
import { copyText } from '../components/CopyBox'
import { useKinds, useModules } from '../ModulesContext'
import { useLive } from '../useLive'

type Show = 'all' | 'movie' | 'episode' | 'album' | 'book'
const SHOW_LABEL: Record<Show, string> = { all: 'All', movie: 'Movies', episode: 'TV', album: 'Music', book: 'Books' }

// Monitored movie releases, episode airs and album releases on one month
// view, with simple filters and a link to add it to a calendar app.
export default function Calendar() {
  const [entries, setEntries] = useState<CalendarEntry[] | null>(null)
  const [error, setError] = useState('')
  const [show, setShow] = useState<Show>('all')
  const [missingOnly, setMissingOnly] = useState(false)
  const [feedOpen, setFeedOpen] = useState(false)
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
  const shown = (entries ?? []).filter((e) => (show === 'all' || e.kind === show) && (!missingOnly || e.status !== 'downloaded'))

  return (
    <div>
      <div className="page-header">
        <h1>Calendar</h1>
        <button className="btn-with-icon" onClick={() => setFeedOpen((v) => !v)} aria-expanded={feedOpen}>
          <Icon name="calendar" size={15} /> Add to your calendar app
        </button>
      </div>
      {feedOpen && <FeedPanel />}
      {error && <p className="error-text">{error}</p>}
      {entries === null && !error && <Loading height={420} />}
      {entries !== null && (
        <>
          <div className="chip-row" style={{ marginBottom: 14, alignItems: 'center' }}>
            {options.length > 2 &&
              options.map((k) => (
                <button key={k} className={`chip${show === k ? ' active' : ''}`} onClick={() => setShow(k)}>
                  {SHOW_LABEL[k]}
                </button>
              ))}
            <label className="inline-field" style={{ marginLeft: options.length > 2 ? 8 : 0 }}>
              <input type="checkbox" checked={missingOnly} onChange={(e) => setMissingOnly(e.target.checked)} />
              Not downloaded yet only
            </label>
          </div>
          {entries.length === 0 && <p style={{ color: 'var(--text-dim)' }}>Nothing is scheduled yet. Monitored movies and shows appear here once their release or air dates are known.</p>}
          <CalendarGrid entries={shown} />
        </>
      )}
    </div>
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
            Add this address to your calendar app as a subscription (in Google Calendar: <em>Other calendars → From URL</em>; on an iPhone: <em>Settings → Calendar → Accounts → Add Subscribed Calendar</em>). It updates by itself.
          </p>
          <div className="feed-url">
            <code>{url}</code>
            <button className="btn-sm" onClick={() => void copyText(url).then((ok) => (ok ? toast.success('Address copied.') : toast.error('Copy did not work. Select the address and copy it by hand.')))}>
              Copy
            </button>
          </div>
          <p className="field-hint">
            Anyone with this address can see your calendar, so keep it to yourself. A new address stops the old one working.
          </p>
          <div className="row-actions">
            <button className="btn-sm" disabled={busy} onClick={() => void run(() => api.newCalendarFeed(), 'New address made. Update it in your calendar app.')}>
              Make a new address
            </button>
            <button className="btn-sm" disabled={busy} onClick={() => void run(() => api.removeCalendarFeed(), 'The calendar address is turned off.')}>
              Turn it off
            </button>
          </div>
          {!window.location.protocol.startsWith('https') && <p className="field-hint">Calendar apps on the internet can only reach this address if Mediarium is reachable from outside, for example through your reverse proxy.</p>}
        </>
      ) : (
        <>
          <p style={{ marginTop: 0 }}>Get a private address that calendar apps (Google, Apple, Outlook) can subscribe to, so releases and episodes show up in your own calendar.</p>
          <button className="primary" disabled={busy} onClick={() => void run(() => api.newCalendarFeed(), 'Your calendar address is ready.')}>
            Make my calendar address
          </button>
        </>
      )}
    </div>
  )
}
