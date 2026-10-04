import { useEffect, useState } from 'react'
import { api, type LibraryStats } from '../api'
import { formatBytes } from '../format'
import Loading from '../components/Loading'

// Statistics: what is in the library, at which quality, how much space it
// takes, and how downloads went month by month.
export default function Statistics() {
  const [s, setS] = useState<LibraryStats | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    api
      .libraryStats()
      .then(setS)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  if (error) return <p className="error-text">{error}</p>
  if (!s) return <Loading height={300} />
  const qualityMax = Math.max(1, ...s.quality.map((q) => q.count))
  const monthMax = Math.max(1, ...s.months.map((m) => m.completed + m.failed))
  const pct = (a: number, b: number) => (b > 0 ? Math.round((a * 100) / b) : 0)
  return (
    <div>
      <div className="page-header">
        <h1>Statistics</h1>
      </div>
      <div className="stat-cards">
        <div className="card stat-card">
          <small>Movies</small>
          <strong>{s.movies}</strong>
          <span>
            {s.moviesHave} downloaded ({pct(s.moviesHave, s.movies)}%) · {formatBytes(s.movieBytes)}
          </span>
        </div>
        <div className="card stat-card">
          <small>TV shows</small>
          <strong>{s.shows}</strong>
          <span>
            {s.episodesHave} of {s.episodes} episodes ({pct(s.episodesHave, s.episodes)}%) · {formatBytes(s.episodeBytes)}
          </span>
        </div>
        {s.albums > 0 && (
          <div className="card stat-card">
            <small>Albums</small>
            <strong>{s.albums}</strong>
            <span>
              {s.albumsHave} downloaded ({pct(s.albumsHave, s.albums)}%)
            </span>
          </div>
        )}
        <div className="card stat-card">
          <small>Library size</small>
          <strong>{formatBytes(s.movieBytes + s.episodeBytes)}</strong>
          <span>{s.usenetShare > 0 || s.months.length > 0 ? `${s.usenetShare}% of downloads came from Usenet` : 'No downloads yet'}</span>
        </div>
      </div>

      <div className="half-cols">
        <section className="card">
          <h2>By quality</h2>
          {s.quality.length === 0 && <p style={{ color: 'var(--text-dim)' }}>Nothing downloaded yet.</p>}
          <div className="bar-list">
            {s.quality.map((q) => (
              <div key={q.label} className="bar-row">
                <span className="bar-label">{q.label}</span>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(q.count * 100) / qualityMax}%` }} />
                </span>
                <span className="bar-value">
                  {q.count} · {formatBytes(q.bytes ?? 0)}
                </span>
              </div>
            ))}
          </div>
        </section>
        <section className="card">
          <h2>Downloads by month</h2>
          {s.months.length === 0 && <p style={{ color: 'var(--text-dim)' }}>No downloads in the history yet.</p>}
          <div className="bar-list">
            {s.months.map((m) => (
              <div key={m.month} className="bar-row">
                <span className="bar-label">{new Date(`${m.month}-15`).toLocaleDateString(undefined, { month: 'short', year: 'numeric' })}</span>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(m.completed * 100) / monthMax}%` }} />
                  <span className="bar-fill bad" style={{ width: `${(m.failed * 100) / monthMax}%` }} />
                </span>
                <span className="bar-value">
                  {m.completed} done{m.failed > 0 ? `, ${m.failed} failed` : ''} · {formatBytes(m.bytes)}
                </span>
              </div>
            ))}
          </div>
          {s.historyKeptDays > 0 && <p className="field-hint">The history is kept for {s.historyKeptDays} days, so older months are not counted.</p>}
        </section>
      </div>
    </div>
  )
}
