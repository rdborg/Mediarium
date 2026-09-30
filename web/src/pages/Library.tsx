import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, isAdmin, type BulkResult, type BulkTitle, type Movie, type MusicArtist, type QualityProfile, type QueueItem, type Series, type SourcePref } from '../api'
import { useAuth } from '../AuthContext'
import { useKinds, useModules, type MediaKind } from '../ModulesContext'
import MusicLibrary from './MusicLibrary'
import ActionMenu from '../components/ActionMenu'
import BulkBar from '../components/BulkBar'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import PosterCard, { PosterFallback, type CardAction } from '../components/PosterCard'
import { sortProfiles } from '../components/qualityBlurb'
import { detailsState, movieState, progressFor, seriesState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { useConfirm } from '../components/ConfirmProvider'
import { useLive } from '../useLive'
import { chosenOf, count, failureLines, nameList, selectAllKeys, selectionText, selectRange, toggleKey } from '../librarySelection'
import { removeOption } from '../diskUsage'

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
  gettingDetails: boolean // an import is still filling in this title's details
}

// The most titles one "search now" for a selection looks up (the server enforces it).
const BULK_SEARCH_LIMIT = 25

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
    state: detailsState(m) ?? movieState(m, queue), live: progressFor(queue, (q) => q.movieId === m.id && !q.seriesId), genres: m.genres ?? [],
    gettingDetails: !!m.detailsState,
  }
}

function fromSeries(s: Series, queue: QueueItem[]): Item {
  const status: Item['status'] = s.episodeCount > 0 && s.downloadedCount === s.episodeCount ? 'downloaded' : s.downloadedCount > 0 ? 'partial' : 'missing'
  return {
    key: `tv-${s.id}`, kind: 'tv', id: s.id, tmdbId: s.tmdbId, title: s.title, year: s.year, posterUrl: s.posterUrl,
    monitored: s.monitored, profileId: s.profileId ?? 0, status, have: s.downloadedCount, total: s.episodeCount,
    state: detailsState(s) ?? seriesState(s, queue), live: progressFor(queue, (q) => q.seriesId === s.id), genres: s.genres ?? [],
    gettingDetails: !!s.detailsState,
  }
}

const STATE_FILTERS: { id: string; label: string; test: (i: Item) => boolean }[] = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'downloaded', label: 'Downloaded', test: (i) => i.state.key === 'downloaded' },
  { id: 'downloading', label: 'Downloading', test: (i) => i.state.key === 'downloading' },
  { id: 'pending', label: 'Pending', test: (i) => i.state.key === 'pending' },
  { id: 'searching', label: 'Waiting for a release', test: (i) => i.state.key === 'searching' },
  { id: 'partial', label: 'Partial', test: (i) => i.state.key === 'partial' },
  { id: 'added', label: 'Not monitored', test: (i) => i.state.key === 'added' },
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
  const [params] = useSearchParams()
  const kinds = useKinds()
  const musicOn = useModules().on('music')
  const [tab, setTab] = useState<MediaKind>(() => {
    // The Library always opens on the first tab (Movies) unless a link asks for another.
    const asked = params.get('kind')
    return asked === 'tv' || asked === 'music' ? asked : 'movie'
  })
  useEffect(() => {
    const asked = params.get('kind')
    if (asked === 'tv' || asked === 'movie' || asked === 'music') setTab(asked)
  }, [params])
  // A kind that is switched off cannot be shown: fall back to the first that is on.
  const active: MediaKind = kinds.includes(tab) ? tab : kinds[0]
  const kind: Kind = active === 'music' ? 'movie' : active
  const [artists, setArtists] = useState<MusicArtist[] | null>(null)
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
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const anchor = useRef<string | null>(null) // the last one ticked, for shift-click
  const [note, setNote] = useState<{ message: string; lines: string[] } | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .listQueue()
      .catch(() => [] as QueueItem[])
      .then((queue) => {
        // A list that arrives clears an earlier message, so one failed refresh does not stay on the page.
        const failed = (e: unknown) => setError(e instanceof Error ? e.message : String(e))
        api.listMovies().then((m) => { setMovies(m.map((x) => fromMovie(x, queue))); setError('') }).catch(failed)
        api.listSeries().then((s) => { setShows(s.map((x) => fromSeries(x, queue))); setError('') }).catch(failed)
        if (musicOn) api.listArtists().then((a) => { setArtists(a); setError('') }).catch(failed)
      })
  }, [musicOn])
  useEffect(() => {
    load()
  }, [load])
  useLive(load, 4000)
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
  // Everything that narrows the list (sorting does not), and the way back.
  const filtersActive = text.trim() !== '' || filter !== 'all' || genre !== '' || decade !== ''
  function clearFilters() {
    setText('')
    setFilter('all')
    setGenre('')
    setDecade('')
  }
  const genreOptions = useMemo(() => [...new Set((items ?? []).flatMap((i) => i.genres))].sort(), [items])
  const decadeOptions = useMemo(() => [...new Set((items ?? []).filter((i) => i.year).map((i) => Math.floor(i.year / 10) * 10))].sort((a, b) => b - a), [items])

  function chooseKind(k: MediaKind) {
    setTab(k)
    setFilter('all')
    setSelected(new Set())
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

  // One title's Remove button on its card or row: the question and the
  // "also delete the files" box are the same as ever.
  async function removeOne(i: Item) {
    const usage = await (i.kind === 'movie' ? api.movieDiskUsage(i.id) : api.seriesDiskUsage(i.id)).catch(() => null)
    const answer = await confirm({
      title: `Remove ${i.title} from your library?`,
      body: <p>Mediarium stops tracking it. You can add it back any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: removeOption(i.kind === 'movie' ? 'movie' : 'show', usage),
    })
    if (!answer) return
    const files = answer.checked
    await run(() => (i.kind === 'movie' ? api.deleteMovie(i.id, files) : api.deleteSeries(i.id, files)), `Removed ${i.title}${files ? ' and its files' : ''}.`)
  }

  const actionsFor = (i: Item): CardAction[] => [
    { icon: 'search', label: 'Search for a release now', onClick: () => void searchNow(i), disabled: busy },
    { icon: i.monitored ? 'eye' : 'eye-off', label: i.monitored ? 'Monitored (click to stop)' : 'Not monitored (click to monitor)', onClick: () => void setMonitored(i, !i.monitored), active: i.monitored, disabled: busy },
    { icon: 'open', label: 'Open', onClick: () => navigate(openPath(i)) },
    ...(admin ? [{ icon: 'trash', label: 'Remove from library', onClick: () => void removeOne(i), danger: true, disabled: busy } satisfies CardAction] : []),
  ]

  // Ticking one, or a whole stretch with shift held.
  function pick(key: string, shift: boolean) {
    setSelected((cur) => (shift ? selectRange(cur, shown ?? [], anchor.current, key) : toggleKey(cur, key)))
    anchor.current = key
  }
  function stopSelecting() {
    setSelecting(false)
    setSelected(new Set())
    setNote(null)
  }
  // The bar counts, and every action reaches, only what is in the view: after a
  // filter or a search, nothing you can no longer see is touched.
  const chosen = useMemo(() => chosenOf(shown ?? [], selected), [shown, selected])
  const bulkTitles = (): BulkTitle[] => chosen.map((i) => ({ kind: i.kind, id: i.id }))
  const noun = kind === 'movie' ? 'movie' : 'show'
  const selText = selectionText({ selected: chosen.length, shown: shown?.length ?? 0, total: items?.length ?? 0, noun })

  // Says how a bulk change went: a short toast when all is well, and a note
  // that stays on the page, with the reasons, when something could not be done.
  function report(r: { message: string; failed: BulkResult['failed'] }) {
    if (r.failed.length === 0) {
      setNote(null)
      toast.success(r.message)
      return
    }
    const titleOf = (k: string, id: number) => (items ?? []).find((i) => i.kind === k && i.id === id)?.title
    setNote({ message: r.message, lines: failureLines(r.failed, titleOf) })
    toast.error(r.message)
  }
  async function runBulk(action: () => Promise<{ message: string; failed: BulkResult['failed'] }>) {
    setBusy(true)
    setNote(null)
    try {
      report(await action())
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function searchChosen() {
    // The same test the server uses: monitored, and something missing.
    const wanted = chosen.filter((i) => i.monitored && (i.kind === 'movie' ? i.status === 'missing' : i.total > 0 && i.have < i.total))
    if (wanted.length === 0) {
      toast.info('Nothing to search for. Only monitored titles that are missing something are searched.')
      return
    }
    const over = Math.max(0, wanted.length - BULK_SEARCH_LIMIT)
    const answer = await confirm({
      title: `Search for ${count(Math.min(wanted.length, BULK_SEARCH_LIMIT), noun)} now?`,
      body: (
        <>
          <p>
            {wanted.length} of the {chosen.length} selected {wanted.length === 1 ? 'is' : 'are'} monitored and missing something, so {wanted.length === 1 ? 'that one is' : 'those are'} searched. The rest are left alone.
          </p>
          {over > 0 && (
            <p>
              It searches at most {BULK_SEARCH_LIMIT} titles at a time. The other {over} are left to the automatic search.
            </p>
          )}
          <p>The search runs in the background. Anything it finds shows up in Activity.</p>
        </>
      ),
      confirmLabel: `Search ${Math.min(wanted.length, BULK_SEARCH_LIMIT)}`,
    })
    if (!answer) return
    setBusy(true)
    setNote(null)
    try {
      const r = await api.bulkSearchNow(wanted.map((i) => ({ kind: i.kind, id: i.id })))
      if (r.failed.length > 0) report(r)
      else toast.info(r.message)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function removeChosen() {
    const n = chosen.length
    if (n === 0) return
    const what = count(n, noun)
    const answer = await confirm({
      title: `Remove ${what} from your library?`,
      body: (
        <>
          <p>
            <strong>{nameList(chosen.map((i) => i.title))}</strong>
          </p>
          <p>
            Mediarium stops tracking {n === 1 ? 'it' : `all ${n}`} and cancels anything still downloading for {n === 1 ? 'it' : 'them'}. You can add {n === 1 ? 'it' : 'them'} back any time.
          </p>
          <p>
            <strong>Your files stay where they are</strong> unless you tick the box below.
          </p>
        </>
      ),
      confirmLabel: `Remove ${what}`,
      danger: true,
      option: {
        label: n === 1 ? 'Also delete its files from the disk' : `Also delete the files of all ${n} from the disk`,
        hint: 'This deletes their folders in your library too. It cannot be undone.',
        defaultChecked: false,
        warning: n === 1 ? `The files of this ${noun} will be deleted for good.` : `The files of all ${what} will be deleted for good.`,
      },
    })
    if (!answer) return
    await runBulk(() => api.bulkRemove(bulkTitles(), answer.checked))
  }

  const profileChoices = useMemo(
    () => [
      { id: '0', label: `Default (${profiles.find((p) => p.id === defaultProfile)?.name ?? 'the default profile'})` },
      ...sortProfiles(profiles).map((p) => ({ id: String(p.id), label: p.name })),
    ],
    [profiles, defaultProfile],
  )

  const total = items?.length ?? 0

  // Only the kinds of media that are switched on get a tab.
  const counts: Record<MediaKind, number | undefined> = { movie: movies?.length, tv: shows?.length, music: artists?.length }
  const kindSwitch = (
    <div className="seg">
      {kinds.map((k) => (
        <button key={k} className={active === k ? 'active' : ''} onClick={() => chooseKind(k)}>
          {k === 'movie' ? 'Movies' : k === 'tv' ? 'TV' : 'Music'} {counts[k] !== undefined ? <small>({counts[k]})</small> : null}
        </button>
      ))}
    </div>
  )

  if (active === 'music') return <MusicLibrary artists={artists} error={error} switcher={kindSwitch} admin={admin} reload={load} />

  return (
    <div>
      <div className="toolbar">
        {kindSwitch}
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
        {admin && (
          <button className={`btn-with-icon${selecting ? ' primary' : ''}`} aria-pressed={selecting} onClick={() => (selecting ? stopSelecting() : setSelecting(true))}>
            <Icon name="check" size={16} /> Select
          </button>
        )}
        <div className="seg">
          <button className={view === 'grid' ? 'active' : ''} onClick={() => chooseView('grid')} aria-label="Grid view">
            <Icon name="grid" size={16} />
          </button>
          <button className={view === 'list' ? 'active' : ''} onClick={() => chooseView('list')} aria-label="List view">
            <Icon name="list" size={16} />
          </button>
        </div>
        {admin && (
          <button className="btn-with-icon" onClick={() => navigate(`/import?kind=${kind}`)}>
            <Icon name="folder" size={16} /> Import existing
          </button>
        )}
        <button className="primary btn-with-icon" onClick={() => navigate('/search')}>
          <Icon name="plus" size={16} /> Add new
        </button>
      </div>

      <div className="chip-row" style={{ marginBottom: 18, alignItems: 'center' }}>
        {FILTERS[kind].map((f) => (
          <button key={f.id} className={`chip${filter === f.id ? ' active' : ''}`} onClick={() => setFilter(f.id)}>
            {f.label} {items ? <small>{items.filter(f.test).length}</small> : null}
          </button>
        ))}
        {filtersActive && (
          <button className="btn-sm" onClick={clearFilters}>
            Clear filters
          </button>
        )}
      </div>

      {selecting && admin && (
        <BulkBar text={selText} onSelectAll={() => shown && setSelected(selectAllKeys(shown))} onSelectNone={() => setSelected(new Set())} onDone={stopSelecting} busy={busy}>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkMonitored(bulkTitles(), true))}>
            <Icon name="eye" size={15} /> Monitor
          </button>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkMonitored(bulkTitles(), false))}>
            <Icon name="eye-off" size={15} /> Stop monitoring
          </button>
          <ActionMenu
            label="Better versions"
            disabled={chosen.length === 0 || busy}
            onPick={(id) => void runBulk(() => api.bulkNoUpgrade(bulkTitles(), id === 'leave'))}
            choices={[
              { id: 'look', label: 'Look for better versions', hint: 'Swap a download for a better one when its quality profile allows it.' },
              { id: 'leave', label: 'Leave what I have alone', hint: 'Keep the files as they are, even if a better one is out there.' },
            ]}
          />
          <ActionMenu label="Quality profile" disabled={chosen.length === 0 || busy || profiles.length === 0} choices={profileChoices} onPick={(id) => void runBulk(() => api.bulkProfile(bulkTitles(), Number(id)))} />
          <ActionMenu
            label="Download from"
            disabled={chosen.length === 0 || busy}
            onPick={(id) => void runBulk(() => api.bulkSources(bulkTitles(), (id === 'default' ? '' : id) as SourcePref))}
            choices={[
              { id: 'default', label: 'Use the default' },
              { id: 'usenet', label: 'Usenet only' },
              { id: 'torrent', label: 'Torrents only' },
              { id: 'both', label: 'Usenet and torrents' },
            ]}
          />
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void searchChosen()}>
            <Icon name="search" size={15} /> Search now
          </button>
          <button className="btn-sm btn-danger btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void removeChosen()}>
            <Icon name="trash" size={15} /> Remove from library
          </button>
        </BulkBar>
      )}

      {note && (
        <div className="bulk-note" role="status">
          <div>
            <strong>{note.message}</strong>
            <ul>
              {note.lines.map((l, n) => (
                <li key={n}>{l}</li>
              ))}
            </ul>
          </div>
          <button className="btn-sm" onClick={() => setNote(null)}>
            Dismiss
          </button>
        </div>
      )}

      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid" key={`grid-${active}`}>
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="empty-state" key={`empty-${active}`}>
          <Icon name={kind === 'movie' ? 'film' : 'tv'} size={44} />
          {total === 0 ? (
            <>
              <p>Your {kind === 'movie' ? 'movie' : 'TV'} library is empty.</p>
              <div className="row-actions">
                <button className="primary btn-with-icon" onClick={() => navigate('/search')}>
                  <Icon name="search" size={16} /> Search to add {kind === 'movie' ? 'a movie' : 'a show'}
                </button>
                {admin && (
                  <button className="btn-with-icon" onClick={() => navigate(`/import?kind=${kind}`)}>
                    <Icon name="folder" size={16} /> Import what you already have
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
              <p>Nothing matches these filters.</p>
              <button className="btn-with-icon" onClick={clearFilters}>
                Clear filters
              </button>
            </>
          )}
        </div>
      ) : view === 'grid' ? (
        <div className="poster-grid" key={`grid-${active}`}>
          {shown.map((i) => (
            <PosterCard
              key={i.key}
              to={openPath(i)}
              poster={i.posterUrl}
              title={i.title}
              meta={[i.year || null, i.kind === 'movie' ? i.quality : i.gettingDetails ? null : `${i.have}/${i.total} episodes`, i.monitored ? null : 'unmonitored'].filter(Boolean).join(' · ')}
              state={i.state}
              kind={i.kind}
              progress={i.live ? { pct: i.live.progressPct, label: `${Math.round(i.live.progressPct)}%${i.live.sizeBytes ? ` of ${formatBytes(i.live.sizeBytes)}` : ''} · ${i.live.protocol}` } : undefined}
              dim={!i.monitored}
              actions={actionsFor(i)}
              selection={selecting ? { selected: selected.has(i.key), onToggle: (e) => pick(i.key, e.shiftKey) } : undefined}
              footer={
                i.kind === 'tv' && i.total > 0 && !i.gettingDetails ? (
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
              {selecting && (
                <th style={{ width: 34 }}>
                  <input
                    type="checkbox"
                    checked={selText.allSelected}
                    onChange={() => setSelected(selText.allSelected ? new Set() : selectAllKeys(shown))}
                    aria-label="Select all in this view"
                  />
                </th>
              )}
              <th>Title</th>
              <th>Status</th>
              <th>{kind === 'movie' ? 'Quality' : 'Episodes'}</th>
              <th>Profile</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {shown.map((i) => (
              <tr key={i.key} className={selecting && selected.has(i.key) ? 'selected' : undefined} style={i.monitored ? undefined : { opacity: 0.65 }}>
                {selecting && (
                  <td>
                    <input type="checkbox" checked={selected.has(i.key)} onChange={() => undefined} onClick={(e) => pick(i.key, e.shiftKey)} aria-label={`Select ${i.title}`} />
                  </td>
                )}
                <td>
                  <Link to={openPath(i)} className="lib-title" onClick={selecting ? (e) => { e.preventDefault(); pick(i.key, e.shiftKey) } : undefined}>
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
