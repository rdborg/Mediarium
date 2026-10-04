import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import Icon from '../components/Icon'
import ProfilePicker from '../components/ProfilePicker'
import Switch from '../components/Switch'
import TitleHero from '../components/TitleHero'
import WatchLinks from '../components/WatchLinks'
import TitleEvents from '../components/TitleEvents'
import { useToast } from '../components/Toast'
import ReleaseTable from '../components/ReleaseTable'
import FilesPanel from '../components/FilesPanel'
import SubtitlesOffNote from '../components/SubtitlesOffNote'
import SubtitlesPanel from '../components/SubtitlesPanel'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { api, isAdmin, type Episode, type SearchResult, type SeriesDetail as SeriesDetailData, type TVDetail } from '../api'
import { qualityText } from '../format'
import { useLive } from '../useLive'
import { useConfirm } from '../components/ConfirmProvider'
import { removeOption } from '../diskUsage'
import ReleaseNotice from '../components/ReleaseNotice'
import type { Unanswered } from '../releaseHint'
import { useDocumentTitle } from '../documentTitle'

// A target for interactive search: one episode, or a whole season (no
// episode set) which only offers season packs.
interface SearchTarget {
  season: number
  episode?: number
  label: string
}

// Series detail: seasons with per-episode status, and Sonarr-style
// interactive search — pick an episode (or a whole season) and choose which
// release to grab.
export default function SeriesDetail() {
  // Another show starts clean, so nothing of the last one (its releases, its messages) stays on the page.
  return <SeriesPage key={useParams().id} />
}

function SeriesPage() {
  const confirm = useConfirm()
  const admin = isAdmin(useAuth().user)
  const { subtitlesOn } = useModules()
  const { id } = useParams()
  const seriesId = Number(id)
  const navigate = useNavigate()
  const [series, setSeries] = useState<SeriesDetailData | null>(null)
  useDocumentTitle(series?.title)
  const [tv, setTv] = useState<TVDetail | null>(null)
  const [error, setError] = useState('')
  // A refresh that fails while the show is already on screen is not worth an error message.
  const [loadError, setLoadError] = useState('')
  const [message, setMessage] = useState('')
  const [target, setTarget] = useState<SearchTarget | null>(null)
  const [results, setResults] = useState<SearchResult[] | null>(null)
  const [answered, setAnswered] = useState<{ sources: number; unanswered: Unanswered[] }>({ sources: 0, unanswered: [] })
  const [searching, setSearching] = useState(false)
  const [grabbing, setGrabbing] = useState<string | null>(null)
  const [searchingNow, setSearchingNow] = useState(false)
  const [subsOpen, setSubsOpen] = useState<number | null>(null)
  // Which seasons are open (unset: only the one that needs attention), and the
  // "Missing episodes only" switch.
  const [openSeasons, setOpenSeasons] = useState<Record<number, boolean>>({})
  const [missingOnly, setMissingOnly] = useState(false)
  const toast = useToast()

  const load = useCallback(() => {
    api
      .getSeries(seriesId)
      .then((s) => {
        setSeries(s)
        setLoadError('')
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [seriesId])
  useEffect(load, [load])
  const tmdbId = series?.tmdbId
  useEffect(() => {
    if (tmdbId) api.tmdbTVDetail(tmdbId).then(setTv).catch(() => undefined)
  }, [tmdbId])

  // Refreshes itself, faster while anything is downloading so statuses update without a reload.
  const anyDownloading = series?.episodes.some((e) => e.status === 'downloading') ?? false
  useLive(load, anyDownloading ? 3000 : 5000)

  const seasons = useMemo(() => {
    const map = new Map<number, Episode[]>()
    for (const e of series?.episodes ?? []) {
      map.set(e.season, [...(map.get(e.season) ?? []), e])
    }
    return [...map.entries()].sort((a, b) => a[0] - b[0])
  }, [series])

  async function runSearch(t: SearchTarget) {
    setTarget(t)
    setResults(null)
    setSearching(true)
    setError('')
    setMessage('')
    try {
      const a = await api.seriesSearch(seriesId, t.season, t.episode)
      setAnswered({ sources: a.sources, unanswered: a.unanswered })
      setResults(a.results)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSearching(false)
    }
  }

  async function grab(r: SearchResult) {
    setGrabbing(r.downloadUrl)
    setError('')
    try {
      await api.grabSeries(seriesId, {
        releaseTitle: r.title,
        downloadUrl: r.downloadUrl,
        sizeBytes: r.sizeBytes,
        protocol: r.protocol,
        season: target?.season,
        episode: target?.episode,
      })
      setMessage(`Started downloading "${r.title}". Follow it in Activity.`)
      setTimeout(load, 500)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setGrabbing(null)
    }
  }

  async function toggleSeries(monitored: boolean) {
    try {
      await api.setSeriesMonitored(seriesId, monitored)
      toast.success(monitored ? 'Monitored: looking for missing episodes.' : 'Unmonitored: this show is left alone.')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggleSeason(season: number, monitored: boolean) {
    try {
      await api.setSeasonMonitored(seriesId, season, monitored)
      toast.success(`Season ${season} ${monitored ? 'monitored' : 'unmonitored'}.`)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggleEpisode(id: number, monitored: boolean) {
    try {
      await api.setEpisodeMonitored(id, monitored)
      toast.success(monitored ? 'Episode monitored.' : 'Episode unmonitored.')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function searchNow(scope: { season?: number; episode?: number } = {}) {
    setSearchingNow(true)
    setError('')
    setMessage('')
    try {
      const res = await api.searchNowSeries(seriesId, scope)
      setMessage(res.message)
      if (res.grabbed > 0) setTimeout(load, 500)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSearchingNow(false)
    }
  }

  async function refresh() {
    setError('')
    try {
      await api.refreshSeries(seriesId)
      setMessage('Episode list refreshed.')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove() {
    if (!series) return
    const usage = await api.seriesDiskUsage(seriesId).catch(() => null)
    const answer = await confirm({
      title: `Remove ${series.title} from your library?`,
      body: <p>Mediarium stops tracking this show and cancels anything still downloading for it. You can add it back any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: removeOption('show', usage),
    })
    if (!answer) return
    const deleteFiles = answer.checked
    try {
      await api.deleteSeries(seriesId, deleteFiles)
      navigate('/library?kind=tv')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  if (!series) return loadError || error ? <p className="error-text">{loadError || error}</p> : <p>Loading…</p>

  const today = new Date().toISOString().slice(0, 10)
  const isMissing = (e: { status: string; airDate?: string }) => e.status === 'missing' && !!e.airDate && e.airDate <= today
  // The next episode still to air, for the facts at the top.
  const next = series.episodes.filter((e) => e.airDate && e.airDate >= today).sort((a, b) => (a.airDate! < b.airDate! ? -1 : 1))[0]
  const nextText = next ? `S${String(next.season).padStart(2, '0')}E${String(next.episode).padStart(2, '0')} on ${new Date(next.airDate + 'T12:00:00').toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}` : ''
  // Seasons start folded, except the one that needs attention: the latest with
  // missing episodes, or else the latest season.
  const attention = [...seasons].reverse().find(([, eps]) => eps.some(isMissing))?.[0] ?? seasons[seasons.length - 1]?.[0]
  const isOpen = (season: number) => openSeasons[season] ?? season === attention
  const shownSeasons = missingOnly ? seasons.filter(([, eps]) => eps.some(isMissing)) : seasons

  return (
    <div>
      <TitleHero
        info={{
          ...(tv ?? {}),
          kind: 'tv',
          title: series.title,
          year: series.year,
          posterUrl: series.posterUrl,
          overview: series.overview,
          certification: tv?.contentRating,
          facts: [
            { label: 'Episodes', value: `${series.downloadedCount} of ${series.episodeCount} downloaded` },
            ...(nextText ? [{ label: 'Next episode', value: nextText }] : []),
            ...(tv?.networks?.length ? [{ label: 'Network', value: tv.networks.join(', ') }] : []),
          ],
        }}
        actions={
          <>
            <button className="primary btn-with-icon" onClick={() => searchNow()} disabled={searchingNow}>
              <Icon name="search" size={16} /> {searchingNow ? 'Searching…' : 'Find missing episodes'}
            </button>
            <button className="btn-with-icon" onClick={refresh}>
              <Icon name="refresh" size={16} /> Refresh episodes
            </button>
            {series.downloadedCount > 0 && <WatchLinks tmdbId={series.tmdbId} kind="tv" />}
            {admin && (
              <button className="btn-with-icon danger-ghost" onClick={remove}>
                <Icon name="trash" size={16} /> Remove
              </button>
            )}
          </>
        }
      >
        <div className="title-controls">
          <Switch checked={series.monitored} onChange={toggleSeries} label="Monitored" description="Keeps looking for missing episodes." />
          <ProfilePicker kind="series" itemId={seriesId} />
        </div>
      </TitleHero>

      {error && <p className="error-text">{error}</p>}
      {message && <p className="badge downloaded">{message}</p>}

      {target && (
        <section className="card" style={{ marginBottom: 24 }}>
          <h2>Releases for {target.label}</h2>
          {searching && <p>Looking for releases…</p>}
          {results !== null && <ReleaseNotice sources={answered.sources} unanswered={answered.unanswered} results={results.length} />}
          {results !== null && results.length > 0 && (
            <ReleaseTable results={results} grabbing={grabbing} onGrab={grab} showPackBadge />
          )}
        </section>
      )}

      {seasons.length > 0 && (
        <div className="season-bar">
          <div className="chip-row" role="group" aria-label="Seasons">
            {seasons.map(([season, eps]) => (
              <button
                key={season}
                className={`chip${isOpen(season) ? ' active' : ''}`}
                onClick={() => {
                  setOpenSeasons((o) => ({ ...o, [season]: true }))
                  requestAnimationFrame(() => document.getElementById(`season-${season}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
                }}
              >
                {season === 0 ? 'Specials' : `Season ${season}`}
                {eps.some(isMissing) && <span className="dot-missing" aria-label="has missing episodes" />}
              </button>
            ))}
          </div>
          <div className="season-bar-actions">
            <label className="inline-field">
              <input type="checkbox" checked={missingOnly} onChange={(e) => setMissingOnly(e.target.checked)} />
              Missing episodes only
            </label>
            <button className="btn-sm" onClick={() => setOpenSeasons(Object.fromEntries(seasons.map(([s]) => [s, true])))}>
              Open all
            </button>
            <button className="btn-sm" onClick={() => setOpenSeasons(Object.fromEntries(seasons.map(([s]) => [s, false])))}>
              Close all
            </button>
          </div>
        </div>
      )}
      {missingOnly && shownSeasons.length === 0 && <p style={{ color: 'var(--text-dim)' }}>No episodes that have aired are missing.</p>}

      {shownSeasons.map(([season, allEps]) => {
        const eps = missingOnly ? allEps.filter(isMissing) : allEps
        const done = allEps.filter((e) => e.status === 'downloaded').length
        const open = missingOnly || isOpen(season)
        return (
          <section key={season} id={`season-${season}`} style={{ marginBottom: open ? 28 : 12, scrollMarginTop: 80 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
              <button className="season-toggle" aria-expanded={open} onClick={() => setOpenSeasons((o) => ({ ...o, [season]: !open }))}>
                <Icon name={open ? 'chevron-down' : 'chevron-right'} size={18} />
                <h2 style={{ margin: 0 }}>{season === 0 ? 'Specials' : `Season ${season}`}</h2>
              </button>
              <span className={`badge ${done === allEps.length ? 'downloaded' : 'missing'}`}>
                {done} / {allEps.length}
              </span>
              <button onClick={() => runSearch({ season, label: `Season ${season} (packs)` })}>Search season</button>
              <label style={{ display: 'flex', gap: 4, alignItems: 'center', fontSize: '0.85rem', color: 'var(--text-dim)' }}>
                <input
                  type="checkbox"
                  checked={allEps.every((e) => e.monitored)}
                  onChange={(e) => toggleSeason(season, e.target.checked)}
                />
                Monitored
              </label>
            </div>
            {open && <table style={{ marginTop: 8 }}>
              <thead>
                <tr>
                  <th style={{ width: 32 }}></th>
                  <th style={{ width: 48 }}>#</th>
                  <th>Title</th>
                  <th>Air date</th>
                  <th>Status</th>
                  <th>Quality</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {eps.map((e) => {
                  const unaired = !e.airDate || e.airDate > today
                  return (
                    <Fragment key={e.id}>
                    <tr>
                      <td>
                        <input
                          type="checkbox"
                          checked={e.monitored}
                          onChange={(ev) => toggleEpisode(e.id, ev.target.checked)}
                          aria-label={`Monitor episode ${e.episode}`}
                        />
                      </td>
                      <td>{e.episode}</td>
                      <td>{e.title || <span style={{ color: 'var(--text-dim)' }}>TBA</span>}</td>
                      <td style={{ color: 'var(--text-dim)' }}>{e.airDate || '—'}</td>
                      <td>
                        {e.status === 'missing' && unaired ? (
                          <span className="badge">unaired</span>
                        ) : (
                          <span className={`badge ${e.status}`}>{e.status}</span>
                        )}
                      </td>
                      <td>{qualityText(e.quality)}</td>
                      <td>
                        <button
                          disabled={unaired && e.status === 'missing'}
                          onClick={() => runSearch({ season, episode: e.episode, label: `S${String(season).padStart(2, '0')}E${String(e.episode).padStart(2, '0')}` })}
                        >
                          Search
                        </button>{' '}
                        {subtitlesOn && e.status === 'downloaded' && (
                          <button onClick={() => setSubsOpen(subsOpen === e.id ? null : e.id)}>Subtitles</button>
                        )}
                      </td>
                    </tr>
                    {subtitlesOn && subsOpen === e.id && (
                      <tr>
                        <td colSpan={7}>
                          <SubtitlesPanel kind="episode" id={e.id} />
                        </td>
                      </tr>
                    )}
                    </Fragment>
                  )
                })}

              </tbody>
            </table>}
          </section>
        )
      })}

      <details className="card files-card" style={{ marginBottom: 24 }}>
        <summary>
          <h2 style={{ margin: 0, display: 'inline' }}>What happened</h2>
        </summary>
        <div style={{ marginTop: 14 }}>
          <TitleEvents kind="series" id={series.id} />
        </div>
      </details>

      {series.downloadedCount > 0 && (
        <details className="card files-card" style={{ marginBottom: 24 }}>
          <summary>
            <h2 style={{ margin: 0, display: 'inline' }}>Files on disk</h2>
          </summary>
          <div style={{ marginTop: 14 }}>
            <FilesPanel kind="series" id={series.id} />
          </div>
        </details>
      )}

      {series.downloadedCount > 0 && !subtitlesOn && <SubtitlesOffNote />}
    </div>
  )
}
