import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Movie, type Series, type SubtitleWanted, type WantedItem } from '../api'
import Icon from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { describeState } from '../components/state'
import { useToast } from '../components/Toast'
import { languageName } from '../languages'

type Kind = 'missing' | 'cutoff' | 'subtitles'

// Wanted: what Mediarium is still hunting for. "Missing" is monitored movies
// with nothing downloaded and aired episodes without a file; "Upgrades" is
// downloaded items still below their profile's cutoff; "Subtitles" is
// downloaded titles missing a language you asked for.
export default function Wanted() {
  const toast = useToast()
  const [kind, setKind] = useState<Kind>('missing')
  const [items, setItems] = useState<WantedItem[] | null>(null)
  const [subs, setSubs] = useState<SubtitleWanted[] | null>(null)
  const [movies, setMovies] = useState<Movie[]>([])
  const [series, setSeries] = useState<Series[]>([])
  const [sweeping, setSweeping] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [counts, setCounts] = useState<{ missing?: number; cutoff?: number; subtitles?: number }>({})

  useEffect(() => {
    api.listMovies().then(setMovies).catch(() => undefined)
    api.listSeries().then(setSeries).catch(() => undefined)
    api.getWanted('missing').then((r) => setCounts((c) => ({ ...c, missing: r.length }))).catch(() => undefined)
    api.getWanted('cutoff').then((r) => setCounts((c) => ({ ...c, cutoff: r.length }))).catch(() => undefined)
    api.subtitlesWanted().then((r) => setCounts((c) => ({ ...c, subtitles: r.length }))).catch(() => undefined)
  }, [])

  const posterOf = useMemo(() => {
    const byMovie = new Map(movies.map((m) => [m.tmdbId, m.posterUrl]))
    const bySeries = new Map(series.map((s) => [s.id, s.posterUrl]))
    return (i: { kind: string; tmdbId?: number; seriesId?: number }) => (i.kind === 'movie' ? byMovie.get(i.tmdbId ?? -1) : bySeries.get(i.seriesId ?? -1))
  }, [movies, series])

  const load = useCallback(() => {
    setError('')
    if (kind === 'subtitles') {
      setSubs(null)
      api.subtitlesWanted().then(setSubs).catch((e) => setError(e instanceof Error ? e.message : String(e)))
      return
    }
    setItems(null)
    api.getWanted(kind).then(setItems).catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [kind])
  useEffect(load, [load])

  async function sweep() {
    setSweeping(true)
    try {
      toast.success((await api.subtitleSweep()).message)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSweeping(false)
    }
  }

  const keyOf = (i: WantedItem) => `${i.kind}-${i.id}`

  async function searchNow(item: WantedItem) {
    setBusy(keyOf(item))
    try {
      const res =
        item.kind === 'movie'
          ? await api.searchNowMovie(item.movieId ?? item.id)
          : await api.searchNowSeries(item.seriesId!, { season: item.season, episode: item.episode })
      toast.info(`${item.title}${item.subtitle ? ' ' + item.subtitle.split(' · ')[0] : ''}: ${res.message}`)
      if (res.grabbed > 0) load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const link = (i: { kind: string; tmdbId?: number; seriesId?: number }) => (i.kind === 'movie' ? `/title/${i.tmdbId}` : `/series/${i.seriesId}`)
  const missingState = describeState('searching')

  const tab = (k: Kind, label: string) => (
    <button className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>
      {label} {counts[k] !== undefined && <small>({counts[k]})</small>}
    </button>
  )

  return (
    <div>
      <div className="page-header">
        <h1>Wanted</h1>
        <div className="seg">
          {tab('missing', 'Missing')}
          {tab('cutoff', 'Upgrades')}
          {tab('subtitles', 'Subtitles')}
        </div>
      </div>

      {error && <p className="error-text">{error}</p>}

      {kind === 'subtitles' ? (
        subs === null ? (
          <div className="skeleton" style={{ height: 120 }} />
        ) : (
          <>
            <div className="toolbar">
              <p style={{ color: 'var(--text-dim)', margin: 0 }}>Downloaded titles missing a subtitle in one of your chosen languages.</p>
              <span className="spacer" />
              <button className="primary btn-with-icon" onClick={sweep} disabled={sweeping}>
                <Icon name="search" size={16} /> {sweeping ? 'Searching…' : 'Search for all now'}
              </button>
            </div>
            {subs.length === 0 ? (
              <div className="empty-state">
                <Icon name="check" size={44} />
                <p>Every downloaded title has all its subtitles.</p>
              </div>
            ) : (
              <div className="wlist">
                {subs.map((s) => (
                  <div key={`${s.kind}-${s.id}`} className={`wrow kind-${s.kind === 'movie' ? 'movie' : 'tv'}`}>
                    <div className="qthumb">{posterOf(s) ? <img src={posterOf(s)} alt="" loading="lazy" /> : <PosterFallback />}</div>
                    <div className="wmain">
                      <Link to={link(s)}>
                        <strong>{s.title}</strong>
                      </Link>
                      {s.subtitle && <small>{s.subtitle}</small>}
                    </div>
                    <div className="wmeta">
                      {s.missing.map((m) => (
                        <span key={m} className="badge missing">
                          {languageName(m)}
                        </span>
                      ))}
                    </div>
                    <Link className="icon-btn" to={link(s)} title="Open" aria-label="Open">
                      <Icon name="open" size={17} />
                    </Link>
                  </div>
                ))}
              </div>
            )}
          </>
        )
      ) : items === null ? (
        <div className="skeleton" style={{ height: 120 }} />
      ) : items.length === 0 ? (
        <div className="empty-state">
          <Icon name="check" size={44} />
          <p>{kind === 'missing' ? 'Nothing missing. Everything monitored is downloaded, or has not aired yet.' : 'Everything downloaded meets its profile cutoff.'}</p>
        </div>
      ) : (
        <div className="wlist">
          {items.map((i) => (
            <div key={keyOf(i)} className={`wrow kind-${i.kind === 'movie' ? 'movie' : 'tv'}`}>
              <div className="qthumb">{posterOf(i) ? <img src={posterOf(i)} alt="" loading="lazy" /> : <PosterFallback />}</div>
              <div className="wmain">
                <Link to={link(i)}>
                  <strong>{i.title}</strong>
                </Link>
                {i.subtitle && <small>{i.subtitle}</small>}
              </div>
              <div className="wmeta">
                {kind === 'missing' ? (
                  <span className={`state-tag st-${missingState.key}`} title={missingState.hint}>
                    <Icon name={missingState.icon} size={12} /> {missingState.label}
                  </span>
                ) : (
                  <span className="state-tag st-partial" title="Downloaded, but below the quality you asked for">
                    <Icon name="star" size={12} /> {i.quality} → {i.cutoff}
                  </span>
                )}
                {kind === 'missing' && i.date && <small>{i.date}</small>}
                {i.profileName && <span className="badge">{i.profileName}</span>}
              </div>
              <div className="row-actions">
                <button className="primary btn-sm btn-with-icon" disabled={busy === keyOf(i)} onClick={() => void searchNow(i)}>
                  <Icon name="search" size={14} /> {busy === keyOf(i) ? 'Searching…' : 'Search now'}
                </button>
                <Link className="icon-btn" to={link(i)} title="Open" aria-label="Open">
                  <Icon name="open" size={17} />
                </Link>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
