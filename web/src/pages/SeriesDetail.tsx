import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import Icon from '../components/Icon'
import ProfilePicker from '../components/ProfilePicker'
import Switch from '../components/Switch'
import TitleHero from '../components/TitleHero'
import WatchLinks from '../components/WatchLinks'
import TitleEvents from '../components/TitleEvents'
import { useToast } from '../components/Toast'
import ReleaseTable from '../components/ReleaseTable'
import FilesPanel from '../components/FilesPanel'
import SubtitlesPanel from '../components/SubtitlesPanel'
import { useAuth } from '../AuthContext'
import { api, isAdmin, type Episode, type SearchResult, type SeriesDetail as SeriesDetailData, type TVDetail } from '../api'
import { useLive } from '../useLive'
import { useConfirm } from '../components/ConfirmProvider'

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
  const confirm = useConfirm()
  const admin = isAdmin(useAuth().user)
  const { id } = useParams()
  const seriesId = Number(id)
  const navigate = useNavigate()
  const [series, setSeries] = useState<SeriesDetailData | null>(null)
  const [tv, setTv] = useState<TVDetail | null>(null)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [target, setTarget] = useState<SearchTarget | null>(null)
  const [results, setResults] = useState<SearchResult[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [grabbing, setGrabbing] = useState<string | null>(null)
  const [searchingNow, setSearchingNow] = useState(false)
  const [subsOpen, setSubsOpen] = useState<number | null>(null)
  const toast = useToast()

  const load = useCallback(() => {
    api
      .getSeries(seriesId)
      .then(setSeries)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [seriesId])
  useEffect(load, [load])
  useLive(load, 5000)
  const tmdbId = series?.tmdbId
  useEffect(() => {
    if (tmdbId) api.tmdbTVDetail(tmdbId).then(setTv).catch(() => undefined)
  }, [tmdbId])

  // Poll while anything is downloading so statuses update without a reload.
  const anyDownloading = series?.episodes.some((e) => e.status === 'downloading') ?? false
  useEffect(() => {
    if (!anyDownloading) return
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [anyDownloading, load])

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
      setResults(await api.seriesSearch(seriesId, t.season, t.episode))
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
      setMessage(`Grabbed "${r.title}" — track it in Activity / Queue.`)
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
      toast.success(monitored ? 'Monitored: automation will search for missing episodes.' : 'Unmonitored: automation will leave this show alone.')
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
      setMessage('Episode list refreshed from TMDB.')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove() {
    if (!series) return
    const answer = await confirm({
      title: `Remove ${series.title} from your library?`,
      body: <p>Mediarium stops tracking this show and cancels anything still downloading for it. You can add it again at any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: { label: 'Also delete everything on disk', hint: 'The files in your library and anything left over from downloads, so nothing is left behind.', defaultChecked: true },
    })
    if (!answer) return
    const deleteFiles = answer.checked
    try {
      await api.deleteSeries(seriesId, deleteFiles)
      navigate('/library')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  if (!series) return error ? <p className="error-text">{error}</p> : <p>Loading…</p>

  const today = new Date().toISOString().slice(0, 10)

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
          facts: [{ label: 'Episodes', value: `${series.downloadedCount} of ${series.episodeCount} downloaded` }, ...(tv?.networks?.length ? [{ label: 'Network', value: tv.networks.join(', ') }] : [])],
        }}
        actions={
          <>
            <button className="primary btn-with-icon" onClick={() => searchNow()} disabled={searchingNow}>
              <Icon name="search" size={16} /> {searchingNow ? 'Searching…' : 'Find missing episodes'}
            </button>
            <button className="btn-with-icon" onClick={refresh}>
              <Icon name="refresh" size={16} /> Refresh from TMDB
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
          <Switch checked={series.monitored} onChange={toggleSeries} label="Monitored" description="Mediarium keeps looking for missing episodes." />
          <ProfilePicker kind="series" itemId={seriesId} />
        </div>
      </TitleHero>

      {error && <p className="error-text">{error}</p>}
      {message && <p className="badge downloaded">{message}</p>}

      {target && (
        <section className="card" style={{ marginBottom: 24 }}>
          <h2>Releases for {target.label}</h2>
          {searching && <p>Searching indexers…</p>}
          {results !== null && results.length === 0 && (
            <p style={{ color: 'var(--text-dim)' }}>No matching releases. Check that at least one indexer is set up under <Link to="/settings/indexers">Settings &gt; Indexers</Link>.</p>
          )}
          {results !== null && results.length > 0 && (
            <ReleaseTable results={results} grabbing={grabbing} onGrab={grab} showPackBadge />
          )}
        </section>
      )}

      {seasons.map(([season, eps]) => {
        const done = eps.filter((e) => e.status === 'downloaded').length
        return (
          <section key={season} style={{ marginBottom: 28 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <h2 style={{ margin: 0 }}>Season {season}</h2>
              <span className={`badge ${done === eps.length ? 'downloaded' : 'missing'}`}>
                {done} / {eps.length}
              </span>
              <button onClick={() => runSearch({ season, label: `Season ${season} (packs)` })}>Search season</button>
              <label style={{ display: 'flex', gap: 4, alignItems: 'center', fontSize: '0.85rem', color: 'var(--text-dim)' }}>
                <input
                  type="checkbox"
                  checked={eps.every((e) => e.monitored)}
                  onChange={(e) => toggleSeason(season, e.target.checked)}
                />
                Monitored
              </label>
            </div>
            <table style={{ marginTop: 8 }}>
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
                      <td>{e.quality || '—'}</td>
                      <td>
                        <button
                          disabled={unaired && e.status === 'missing'}
                          onClick={() => runSearch({ season, episode: e.episode, label: `S${String(season).padStart(2, '0')}E${String(e.episode).padStart(2, '0')}` })}
                        >
                          Search
                        </button>{' '}
                        {e.status === 'downloaded' && (
                          <button onClick={() => setSubsOpen(subsOpen === e.id ? null : e.id)}>Subtitles</button>
                        )}
                      </td>
                    </tr>
                    {subsOpen === e.id && (
                      <tr>
                        <td colSpan={7}>
                          <SubtitlesPanel kind="episode" id={e.id} />
                        </td>
                      </tr>
                    )}
                    </Fragment>
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
              </tbody>
            </table>
          </section>
        )
      })}
    </div>
  )
}
