import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type LibraryStats, type WatchStats, type WatchedStatTitle } from '../api'
import { formatBytes, timeAgo } from '../format'
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
      {s.watched && <WatchedStats w={s.watched} />}
    </div>
  )
}

function titleLink(t: WatchedStatTitle): string {
  return t.kind === 'series' ? `/series/${t.id}` : `/title/${t.tmdbId ?? 0}`
}

// What gets watched, read from Plex, Jellyfin and Emby (Settings > Media
// servers > "Read what's been watched").
function WatchedStats({ w }: { w: WatchStats }) {
  const pct = (a: number, b: number) => (a + b > 0 ? Math.round((a * 100) / (a + b)) : 0)
  const topMax = Math.max(1, ...w.top.map((t) => t.plays))
  return (
    <>
      <h2 style={{ marginTop: 28 }}>What gets watched</h2>
      <div className="stat-cards">
        <div className="card stat-card">
          <small>Movies watched</small>
          <strong>{w.moviesWatched}</strong>
          <span>
            {pct(w.moviesWatched, w.moviesUnwatched)}% of the {w.moviesWatched + w.moviesUnwatched} you have
          </span>
        </div>
        <div className="card stat-card">
          <small>Episodes watched</small>
          <strong>{w.episodesWatched}</strong>
          <span>
            {pct(w.episodesWatched, w.episodesUnwatched)}% of the {w.episodesWatched + w.episodesUnwatched} you have
          </span>
        </div>
        <div className="card stat-card">
          <small>Never watched</small>
          <strong>{formatBytes(w.unwatchedBytes)}</strong>
          <span>
            {w.moviesUnwatched} {w.moviesUnwatched === 1 ? 'movie' : 'movies'} and {w.episodesUnwatched} {w.episodesUnwatched === 1 ? 'episode' : 'episodes'} nobody has played
          </span>
        </div>
        <div className="card stat-card">
          <small>Plays</small>
          <strong>{w.plays}</strong>
          <span>{w.lastSync ? `Checked ${timeAgo(w.lastSync)}` : 'Not checked yet'}</span>
        </div>
      </div>
      <div className="half-cols">
        <section className="card">
          <h2>Most watched</h2>
          {w.top.length === 0 && <p style={{ color: 'var(--text-dim)' }}>Nothing watched yet.</p>}
          <div className="bar-list">
            {w.top.map((t) => (
              <div key={`${t.kind}-${t.id}`} className="bar-row">
                <Link className="bar-label" to={titleLink(t)} title={t.title}>
                  {t.title}
                </Link>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(t.plays * 100) / topMax}%` }} />
                </span>
                <span className="bar-value">
                  {t.plays} {t.plays === 1 ? 'play' : 'plays'}
                  {t.kind === 'series' ? ` · ${t.episodes} ${t.episodes === 1 ? 'episode' : 'episodes'}` : ''}
                </span>
              </div>
            ))}
          </div>
        </section>
        <section className="card">
          <h2>Watched lately</h2>
          {w.recent.length === 0 && (
            <p style={{ color: 'var(--text-dim)' }}>{w.top.length === 0 ? 'Nothing watched yet.' : "Your media servers haven't said when things were played."}</p>
          )}
          <div className="bar-list">
            {w.recent.map((t) => (
              <div key={`${t.kind}-${t.id}`} className="bar-row recent-row">
                <Link className="bar-label" to={titleLink(t)} title={t.title}>
                  {t.title}
                </Link>
                <span className="bar-value">{timeAgo(t.lastPlayed)}</span>
              </div>
            ))}
          </div>
        </section>
      </div>
    </>
  )
}
