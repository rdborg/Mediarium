import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, isAdmin, type Movie, type QualityProfile, type QueueItem, type Series } from '../api'
import { useAuth } from '../AuthContext'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import PosterCard, { PosterFallback, type CardAction } from '../components/PosterCard'
import { movieState, progressFor, seriesState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { useConfirm } from '../components/ConfirmProvider'

type Kind = 'movie' | 'tv'
type View = 'grid' | 'list'
type Sort = 'added' | 'title' | 'year' | 'status'

// One shape for a movie or a show, so filtering, sorting, selecting and the
// action buttons work the same for both.
interface Item {
  key: string
  kind: Kind
  id: number
  tmdbId: number
  title: string
  year: number
  posterUrl?: string
  monitored: boolean
  profileId: number
  status: 'missing' | 'downloading' | 'downloaded' | 'partial'
  quality?: string
  have: number // episodes downloaded (shows)
  total: number // episodes in total (shows)
  filePath?: string
  state: ItemState
  live?: QueueItem
  genres: string[]
}

const STORAGE = { kind: 'mediarium-library-tab', view: 'mediarium-library-view' }
const remember = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v)
  } catch {
    // best effort only
  }
}
const recall = (k: string) => {
  try {
    return localStorage.getItem(k)
  } catch {
    return null
  }
}

function fromMovie(m: Movie, queue: QueueItem[]): Item {
  return {
    key: `movie-${m.id}`, kind: 'movie', id: m.id, tmdbId: m.tmdbId, title: m.title, year: m.year, posterUrl: m.posterUrl,
    monitored: m.monitored !== false, profileId: m.profileId ?? 0, status: m.status, quality: m.quality, have: m.status === 'downloaded' ? 1 : 0, total: 1, filePath: m.filePath,
    state: movieState(m, queue), live: progressFor(queue, (q) => q.movieId === m.id && !q.seriesId), genres: m.genres ?? [],
  }
}

function fromSeries(s: Series, queue: QueueItem[]): Item {
  const status: Item['status'] = s.episodeCount > 0 && s.downloadedCount === s.episodeCount ? 'downloaded' : s.downloadedCount > 0 ? 'partial' : 'missing'
  return {
    key: `tv-${s.id}`, kind: 'tv', id: s.id, tmdbId: s.tmdbId, title: s.title, year: s.year, posterUrl: s.posterUrl,
    monitored: s.monitored, profileId: s.profileId ?? 0, status, have: s.downloadedCount, total: s.episodeCount,
    state: seriesState(s, queue), live: progressFor(queue, (q) => q.seriesId === s.id), genres: s.genres ?? [],
  }
}

const STATE_FILTERS: { id: string; label: string; test: (i: Item) => boolean }[] = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'downloaded', label: 'Downloaded', test: (i) => i.state.key === 'downloaded' },
  { id: 'downloading', label: 'Downloading', test: (i) => i.state.key === 'downloading' },
  { id: 'pending', label: 'Pending', test: (i) => i.state.key === 'pending' },
  { id: 'searching', label: 'Searching', test: (i) => i.state.key === 'searching' },
  { id: 'partial', label: 'Partial', test: (i) => i.state.key === 'partial' },
  { id: 'added', label: 'Added', test: (i) => i.state.key === 'added' },
  { id: 'failed', label: 'Failed', test: (i) => i.state.key === 'failed' },
]
const FILTERS: Record<Kind, { id: string; label: string; test: (i: Item) => boolean }[]> = { movie: STATE_FILTERS, tv: STATE_FILTERS }

// Your library: movies and shows as posters or a list, with the things you do
// most a click away on every item (search for a release, monitor, open,
// remove) and a select mode for doing them to many at once.
export default function Library() {
  const confirm = useConfirm()
  const navigate = useNavigate()
  const toast = useToast()
  // Removing titles, changing their quality profile and importing a library
  // are for administrators.
  const admin = isAdmin(useAuth().user)
  const [kind, setKind] = useState<Kind>(() => (recall(STORAGE.kind) === 'tv' ? 'tv' : 'movie'))
  const [view, setView] = useState<View>(() => (recall(STORAGE.view) === 'list' ? 'list' : 'grid'))
  const [movies, setMovies] = useState<Item[] | null>(null)
  const [shows, setShows] = useState<Item[] | null>(null)
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfile, setDefaultProfile] = useState(0)
  const [error, setError] = useState('')
  const [text, setText] = useState('')
  const [filter, setFilter] = useState('all')
  const [sort, setSort] = useState<Sort>('added')
  const [genre, setGenre] = useState('')
  const [decade, setDecade] = useState('')
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .listQueue()
      .catch(() => [] as QueueItem[])
      .then((queue) => {
        api.listMovies().then((m) => setMovies(m.map((x) => fromMovie(x, queue)))).catch((e) => setError(e instanceof Error ? e.message : String(e)))
        api.listSeries().then((s) => setShows(s.map((x) => fromSeries(x, queue)))).catch((e) => setError(e instanceof Error ? e.message : String(e)))
      })
  }, [])
  useEffect(() => {
    load()
    const t = setInterval(load, 4000)
    return () => clearInterval(t)
  }, [load])
  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(r.profiles)
      setDefaultProfile(r.defaultId)
    }).catch(() => undefined)
  }, [])

  const items = kind === 'movie' ? movies : shows
  const shown = useMemo(() => {
    if (!items) return null
    const test = FILTERS[kind].find((f) => f.id === filter)?.test ?? (() => true)
    const needle = text.trim().toLowerCase()
    const list = items.filter(
      (i) =>
        test(i) &&
        (!needle || i.title.toLowerCase().includes(needle)) &&
        (!genre || i.genres.includes(genre)) &&
        (!decade || (i.year >= Number(decade) && i.year < Number(decade) + 10)),
    )
    if (sort === 'title') list.sort((a, b) => a.title.localeCompare(b.title))
    else if (sort === 'year') list.sort((a, b) => b.year - a.year)
    else if (sort === 'status') list.sort((a, b) => a.state.key.localeCompare(b.state.key) || a.title.localeCompare(b.title))
    return list
  }, [items, kind, filter, text, sort, genre, decade])
  const genreOptions = useMemo(() => [...new Set((items ?? []).flatMap((i) => i.genres))].sort(), [items])
  const decadeOptions = useMemo(() => [...new Set((items ?? []).filter((i) => i.year).map((i) => Math.floor(i.year / 10) * 10))].sort((a, b) => b - a), [items])

  function chooseKind(k: Kind) {
    setKind(k)
    setFilter('all')
    setSelected(new Set())
    remember(STORAGE.kind, k)
  }
  function chooseView(v: View) {
    setView(v)
    remember(STORAGE.view, v)
  }

  const openPath = (i: Item) => (i.kind === 'movie' ? `/title/${i.tmdbId}` : `/series/${i.id}`)

  async function run(fn: () => Promise<unknown>, done: string) {
    setBusy(true)
    try {
      await fn()
      toast.success(done)
      load()
      return true
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function searchNow(i: Item) {
    setBusy(true)
    try {
      const r = await (i.kind === 'movie' ? api.searchNowMovie(i.id) : api.searchNowSeries(i.id))
      toast.info(`${i.title}: ${r.message}`)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const setMonitored = (i: Item, v: boolean) =>
    run(() => (i.kind === 'movie' ? api.setMovieMonitored(i.id, v) : api.setSeriesMonitored(i.id, v)), `${i.title} ${v ? 'is now monitored' : 'is no longer monitored'}.`)

  async function remove(list: Item[]) {
    if (list.length === 0) return
    const what = list.length === 1 ? list[0].title : `${list.length} items`
    const answer = await confirm({
      title: `Remove ${what} from your library?`,
      body: <p>Mediarium stops tracking {list.length === 1 ? 'it' : 'them'}. You can add {list.length === 1 ? 'it' : 'them'} again at any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: { label: 'Also delete everything on disk', hint: 'The files in your library and anything left over from downloads, so nothing is left behind.', defaultChecked: true },
    })
    if (!answer) return
    const files = answer.checked
    const ok = await run(async () => {
      for (const i of list) await (i.kind === 'movie' ? api.deleteMovie(i.id, files) : api.deleteSeries(i.id, files))
    }, `Removed ${what}${files ? ' and its files' : ''}.`)
    if (ok) {
      setSelected(new Set())
      setSelecting(false)
    }
  }

  const actionsFor = (i: Item): CardAction[] => [
    { icon: 'search', label: 'Search for a release now', onClick: () => void searchNow(i), disabled: busy },
    { icon: i.monitored ? 'eye' : 'eye-off', label: i.monitored ? 'Monitored (click to stop)' : 'Not monitored (click to monitor)', onClick: () => void setMonitored(i, !i.monitored), active: i.monitored, disabled: busy },
    { icon: 'open', label: 'Open', onClick: () => navigate(openPath(i)) },
    ...(admin ? [{ icon: 'trash', label: 'Remove from library', onClick: () => void remove([i]), danger: true, disabled: busy } satisfies CardAction] : []),
  ]

  const toggle = (key: string) =>
    setSelected((cur) => {
      const next = new Set(cur)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  const chosen = (items ?? []).filter((i) => selected.has(i.key))

  async function bulk(fn: (i: Item) => Promise<unknown>, done: string) {
    const ok = await run(async () => {
      for (const i of chosen) await fn(i)
    }, done)
    if (ok) setSelected(new Set())
  }

  const total = items?.length ?? 0

  return (
    <div>
      <div className="toolbar">
        <div className="seg">
          <button className={kind === 'movie' ? 'active' : ''} onClick={() => chooseKind('movie')}>
            Movies {movies ? <small>({movies.length})</small> : null}
          </button>
          <button className={kind === 'tv' ? 'active' : ''} onClick={() => chooseKind('tv')}>
            TV {shows ? <small>({shows.length})</small> : null}
          </button>
        </div>
        <input type="search" placeholder={`Find in your ${kind === 'movie' ? 'movies' : 'shows'}…`} value={text} onChange={(e) => setText(e.target.value)} aria-label="Find in your library" />
        {genreOptions.length > 0 && (
          <Dropdown label="Genre" value={genre} onChange={setGenre} options={[{ value: '', label: 'All genres' }, ...genreOptions.map((g) => ({ value: g, label: g }))]} />
        )}
        <Dropdown label="Year" value={decade} onChange={setDecade} options={[{ value: '', label: 'Any year' }, ...decadeOptions.map((d) => ({ value: String(d), label: `${d}s` }))]} />
        <Dropdown
          label="Sort"
          value={sort}
          onChange={(v) => setSort(v as Sort)}
          options={[
            { value: 'added', label: 'Recently added' },
            { value: 'title', label: 'Title A–Z' },
            { value: 'year', label: 'Newest year' },
            { value: 'status', label: 'Status' },
          ]}
        />
        <span className="spacer" />
        <button className={`btn-with-icon${selecting ? ' primary' : ''}`} aria-pressed={selecting} onClick={() => { setSelecting((s) => !s); setSelected(new Set()) }}>
          <Icon name="check" size={16} /> Select
        </button>
        <div className="seg">
          <button className={view === 'grid' ? 'active' : ''} onClick={() => chooseView('grid')} aria-label="Grid view">
            <Icon name="grid" size={16} />
          </button>
          <button className={view === 'list' ? 'active' : ''} onClick={() => chooseView('list')} aria-label="List view">
            <Icon name="list" size={16} />
          </button>
        </div>
        {admin && (
          <button className="btn-with-icon" onClick={() => navigate('/import')}>
            <Icon name="folder" size={16} /> Import existing
          </button>
        )}
        <button className="primary btn-with-icon" onClick={() => navigate('/search')}>
          <Icon name="plus" size={16} /> Add new
        </button>
      </div>

      <div className="chip-row" style={{ marginBottom: 18 }}>
        {FILTERS[kind].map((f) => (
          <button key={f.id} className={`chip${filter === f.id ? ' active' : ''}`} onClick={() => setFilter(f.id)}>
            {f.label} {items ? <small>{items.filter(f.test).length}</small> : null}
          </button>
        ))}
      </div>

      {selecting && (
        <div className="bulkbar">
          <strong>{selected.size} selected</strong>
          <button className="btn-sm" onClick={() => setSelected(new Set((shown ?? []).map((i) => i.key)))}>All shown</button>
          <button className="btn-sm" disabled={chosen.length === 0 || busy} onClick={() => void bulk((i) => (i.kind === 'movie' ? api.setMovieMonitored(i.id, true) : api.setSeriesMonitored(i.id, true)), 'Monitoring turned on.')}>
            Monitor
          </button>
          <button className="btn-sm" disabled={chosen.length === 0 || busy} onClick={() => void bulk((i) => (i.kind === 'movie' ? api.setMovieMonitored(i.id, false) : api.setSeriesMonitored(i.id, false)), 'Monitoring turned off.')}>
            Unmonitor
          </button>
          <button className="btn-sm" disabled={chosen.length === 0 || busy} onClick={() => void bulk((i) => (i.kind === 'movie' ? api.searchNowMovie(i.id) : api.searchNowSeries(i.id)), 'Searched for releases.')}>
            Search now
          </button>
          {admin && (
            <select
              className="btn-sm"
              value=""
              disabled={chosen.length === 0 || busy}
              onChange={(e) => {
                const id = Number(e.target.value)
                void bulk((i) => (i.kind === 'movie' ? api.setMovieProfile(i.id, id) : api.setSeriesProfile(i.id, id)), 'Quality profile changed.')
              }}
              aria-label="Set quality profile"
            >
              <option value="">Set quality profile…</option>
              <option value={0}>Default</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          )}
          {admin && (
            <button className="btn-sm btn-danger" disabled={chosen.length === 0 || busy} onClick={() => void remove(chosen)}>
              Remove
            </button>
          )}
        </div>
      )}

      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid">
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="empty-state">
          <Icon name={kind === 'movie' ? 'film' : 'tv'} size={44} />
          {total === 0 ? (
            <>
              <p>Your {kind === 'movie' ? 'movie' : 'TV'} library is empty.</p>
              <div className="row-actions">
                <button className="primary btn-with-icon" onClick={() => navigate('/search')}>
                  <Icon name="search" size={16} /> Search to add {kind === 'movie' ? 'a movie' : 'a show'}
                </button>
                {admin && (
                  <button className="btn-with-icon" onClick={() => navigate('/import')}>
                    <Icon name="folder" size={16} /> Import what you already have
                  </button>
                )}
              </div>
            </>
          ) : (
            <p>Nothing matches this filter.</p>
          )}
        </div>
      ) : view === 'grid' ? (
        <div className="poster-grid">
          {shown.map((i) => (
            <PosterCard
              key={i.key}
              to={openPath(i)}
              poster={i.posterUrl}
              title={i.title}
              meta={[i.year || null, i.kind === 'movie' ? i.quality : `${i.have}/${i.total} episodes`, i.monitored ? null : 'unmonitored'].filter(Boolean).join(' · ')}
              state={i.state}
              kind={i.kind}
              progress={i.live ? { pct: i.live.progressPct, label: `${Math.round(i.live.progressPct)}%${i.live.sizeBytes ? ` of ${formatBytes(i.live.sizeBytes)}` : ''} · ${i.live.protocol}` } : undefined}
              dim={!i.monitored}
              actions={actionsFor(i)}
              selection={selecting ? { selected: selected.has(i.key), onToggle: () => toggle(i.key) } : undefined}
              footer={
                i.kind === 'tv' && i.total > 0 ? (
                  <div className="bar" style={{ width: '100%', marginTop: 6 }} title={`${i.have} of ${i.total} episodes`}>
                    <span style={{ width: `${(i.have / i.total) * 100}%` }} />
                  </div>
                ) : undefined
              }
            />
          ))}
        </div>
      ) : (
        <table className="data-table lib-list">
          <thead>
            <tr>
              {selecting && <th style={{ width: 34 }} />}
              <th>Title</th>
              <th>Status</th>
              <th>{kind === 'movie' ? 'Quality' : 'Episodes'}</th>
              <th>Profile</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {shown.map((i) => (
              <tr key={i.key} style={i.monitored ? undefined : { opacity: 0.65 }}>
                {selecting && (
                  <td>
                    <input type="checkbox" checked={selected.has(i.key)} onChange={() => toggle(i.key)} aria-label={`Select ${i.title}`} />
                  </td>
                )}
                <td>
                  <Link to={openPath(i)} className="lib-title">
                    <span className="lib-thumb">{i.posterUrl ? <img src={i.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</span>
                    <span>
                      <strong>{i.title}</strong>
                      <small>
                        {i.year || '—'}
                        {i.monitored ? '' : ' · unmonitored'}
                      </small>
                    </span>
                  </Link>
                </td>
                <td>
                  <span className={`state-tag st-${i.state.key}`} title={i.state.hint}>
                    <Icon name={i.state.icon} size={12} /> {i.state.label}
                  </span>
                  {i.live && (
                    <div className="bar active" style={{ marginTop: 6, minWidth: 110, ['--tc' as string]: 'var(--c-activity)' }}>
                      <span style={{ width: `${Math.max(3, i.live.progressPct)}%` }} />
                    </div>
                  )}
                </td>
                <td>
                  {i.kind === 'movie' ? (
                    i.quality || '—'
                  ) : (
                    <div style={{ minWidth: 110 }}>
                      {i.have} / {i.total}
                      <div className="bar" style={{ marginTop: 4 }}>
                        <span style={{ width: `${i.total ? (i.have / i.total) * 100 : 0}%` }} />
                      </div>
                    </div>
                  )}
                </td>
                <td style={{ color: 'var(--text-dim)' }}>{profiles.find((p) => p.id === (i.profileId || defaultProfile))?.name ?? '—'}</td>
                <td>
                  <div className="row-actions">
                    {actionsFor(i).map((a) => (
                      <button key={a.label} className={`icon-btn${a.active ? ' on' : ''}${a.danger ? ' danger' : ''}`} title={a.label} aria-label={a.label} disabled={a.disabled} onClick={a.onClick}>
                        <Icon name={a.icon} size={17} />
                      </button>
                    ))}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
