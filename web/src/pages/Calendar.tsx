import { useEffect, useState } from 'react'
import { api, type CalendarEntry } from '../api'
import CalendarGrid from '../components/CalendarGrid'
import { useLive } from '../useLive'

// Monitored movie releases and episode airs on one month view.
export default function Calendar() {
  const [entries, setEntries] = useState<CalendarEntry[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .calendar()
      .then(setEntries)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useLive(() => api.calendar().then(setEntries).catch(() => undefined), 30000)

  return (
    <div>
      <div className="page-header">
        <h1>Calendar</h1>
      </div>
      {error && <p className="error-text">{error}</p>}
      {entries === null && !error && <div className="skeleton" style={{ height: 420 }} />}
      {entries !== null && (
        <>
          {entries.length === 0 && <p style={{ color: 'var(--text-dim)' }}>Nothing is scheduled yet. Monitored movies and shows appear here once TMDB has release or air dates for them.</p>}
          <CalendarGrid entries={entries} />
        </>
      )}
    </div>
  )
}
