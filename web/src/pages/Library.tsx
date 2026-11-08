import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { paneDefaultTab } from '../twoPane'
import { useOpenFromList } from '../useTwoPane'
import { api, can, isAdmin, type Book, type BookFormat, type BulkResult, type BulkTitle, type CollectionListItem, type MediaSummary, type Movie, type MusicArtist, type QualityProfile, type QueueItem, type Series, type SourcePref } from '../api'
import { useAuth } from '../AuthContext'
import { defaultProfileFor } from '../profileDefaults'
import { useModules, type MediaKind } from '../ModulesContext'
import { activeLibraryTab, libraryTabs } from '../libraryTabs'
import MusicLibrary from './MusicLibrary'
import BookLibrary from './BookLibrary'
import ActionMenu from '../components/ActionMenu'
import BulkBar from '../components/BulkBar'
import { BulkTagMenu, TagChips } from '../components/Tags'
import Dropdown from '../components/Dropdown'
import Icon, { type IconName } from '../components/Icon'
import { isFinishedShow, isRunningShow, showRibbon, showStatusLabel } from '../showStatus'
import KindGlyph from '../components/KindGlyph'
import MoveToFolderDialog from '../components/MoveToFolderDialog'
import PosterCard, { PosterFallback, type CardAction } from '../components/PosterCard'
import StarterPicks from '../components/StarterPicks'
import { WindowedBody, WindowedGrid } from '../components/Windowed'
import { sortProfiles } from '../components/qualityBlurb'
import { detailsState, movieState, progressFor, seriesState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { formatBytes, withNameTag } from '../format'
import { useConfirm } from '../components/ConfirmProvider'
import { useLive } from '../useLive'
import { anyMoving, fileDetailsPending, libraryTick, pollEvery } from '../pollPlan'
import { chosenOf, count, failureLines, nameList, selectAllKeys, selectionText, selectRange, toggleKey } from '../librarySelection'
import { removeOption } from '../diskUsage'
import FilterToggle from '../components/FilterToggle'
import InfoTip from '../components/InfoTip'
import HelpLink from '../components/HelpLink'
import { useUrlState } from '../useUrlState'
import { plural, t, tx, st } from '../i18n'
import { compareTitles } from '../titleSort'
import { looseIncludes } from '../looseText'
import { largestFirst, libraryTotalText, sumBytes } from '../diskTotals'
import { compareByProfile, matchesProfile, profileFilterOptions, profileName } from '../libraryProfile'
import { redoAnswerText, redoRequest } from '../shortFile'
import { fileFilterOptions, fileFiltersActive, langName, matchesFileFilters, NO_FILE_FILTERS, type FileFilters } from '../fileInfo'

type Kind = 'movie' | 'tv'
type View = 'grid' | 'list'
type Sort = 'added' | 'title' | 'released' | 'size' | 'status' | 'profile'
const SORTS: Sort[] = ['added', 'title', 'released', 'size', 'status', 'profile']

// One shape for a movie or a show, so filtering, sorting, selecting and the
// action buttons work the same for both.
interface Item {
  key: string
  kind: Kind
  anime?: boolean // a show of the anime type (its own tab with the anime library on)
  id: number
  tmdbId: number
  title: string
  tag?: string // tells a show from another of the same name ("UK")
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
  tags: string[]
  gettingDetails: boolean // an import is still filling in this title's details
  copies: number // second copies kept in another quality
  released: string // "2024-03-01", or the year alone when that is all there is
  sizeBytes: number // on disk
  showStatus: string // where a show stands (ended, canceled, continuing, upcoming), '' for movies and shows not read yet
  media?: MediaSummary // what the files hold (codecs, HDR, audio and subtitle languages), once they have been read
  shortFiles: number // files far shorter than the movie or episode should be (fakes, cut-off downloads)
}

// The most titles one "search now" for a selection looks up (the server enforces it).
const BULK_SEARCH_LIMIT = 25

const STORAGE = { kind: 'mediarium-library-tab', view: 'mediarium-library-view', sort: 'mediarium-library-sort', reverse: 'mediarium-library-reverse', size: 'mediarium-library-poster-size' }

// Poster widths the size slider moves between, in pixels.
const POSTER_MIN = 110
const POSTER_MAX = 280
const POSTER_DEFAULT = 168
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
    state: detailsState(m) ?? movieState(m, queue), live: progressFor(queue, (q) => q.movieId === m.id && !q.seriesId), genres: m.genres ?? [], tags: m.tags ?? [],
    gettingDetails: !!m.detailsState,
    copies: m.copies ?? 0,
    released: m.releaseDate || (m.year ? String(m.year) : ''),
    sizeBytes: m.sizeBytes ?? 0,
    showStatus: '',
    media: m.media,
    shortFiles: m.shortFile ? 1 : 0,
  }
}

function fromSeries(s: Series, queue: QueueItem[]): Item {
  const status: Item['status'] = s.episodeCount > 0 && s.downloadedCount === s.episodeCount ? 'downloaded' : s.downloadedCount > 0 ? 'partial' : 'missing'
  return {
    key: `tv-${s.id}`, kind: 'tv', anime: s.seriesType === 'anime', id: s.id, tmdbId: s.tmdbId, title: s.title, tag: s.nameTag, year: s.year, posterUrl: s.posterUrl,
    monitored: s.monitored, profileId: s.profileId ?? 0, status, have: s.downloadedCount, total: s.episodeCount,
    state: detailsState(s) ?? seriesState(s, queue), live: progressFor(queue, (q) => q.seriesId === s.id), genres: s.genres ?? [], tags: s.tags ?? [],
    gettingDetails: !!s.detailsState,
    copies: s.copies ?? 0,
    released: s.firstAirDate || (s.year ? String(s.year) : ''),
    sizeBytes: s.sizeBytes ?? 0,
    showStatus: s.showStatus ?? '',
    media: s.media,
    shortFiles: s.shortFiles ?? 0,
  }
}

const STATE_FILTERS: { id: string; label: string; test: (i: Item) => boolean }[] = [
  { id: 'all', label: t('All'), test: () => true },
  { id: 'downloaded', label: t('Downloaded'), test: (i) => i.state.key === 'downloaded' },
  { id: 'downloading', label: t('Downloading'), test: (i) => i.state.key === 'downloading' },
  { id: 'pending', label: t('Pending'), test: (i) => i.state.key === 'pending' },
  { id: 'searching', label: t('Waiting for a release'), test: (i) => i.state.key === 'searching' },
  { id: 'partial', label: t('Partial'), test: (i) => i.state.key === 'partial' },
  { id: 'added', label: t('Not monitored'), test: (i) => i.state.key === 'added' },
  { id: 'failed', label: t('Failed'), test: (i) => i.state.key === 'failed' },
  { id: 'incomplete', label: t('Looks incomplete'), test: (i) => i.shortFiles > 0 },
]
// The symbol each state filter wears (the same ones the posters' pills use).
const FILTER_ICON: Record<string, IconName> = { ended: 'stop', continuing: 'play', all: 'grid', downloaded: 'check', downloading: 'download', pending: 'clock', searching: 'search', partial: 'download', added: 'bookmark', failed: 'warning', incomplete: 'warning' }
// Shows can also be told apart by whether more episodes are coming.
const SHOW_FILTERS = [
  { id: 'ended', label: t('Ended'), test: (i: Item) => isFinishedShow(i.showStatus) },
  { id: 'continuing', label: t('Continuing'), test: (i: Item) => isRunningShow(i.showStatus) },
]
const FILTERS: Record<Kind, { id: string; label: string; test: (i: Item) => boolean }[]> = { movie: STATE_FILTERS, tv: [...STATE_FILTERS, ...SHOW_FILTERS] }

// Your library: movies and shows as posters or a list, with the things you do
// most a click away on every item (search for a release, monitor, open,
// remove) and a select mode for doing them to many at once.
// A Library tab: a kind of media, or one of the two book formats.
type Tab = MediaKind | 'anime' | BookFormat
// Each tab wears its kind's colour, as everywhere else.
const TAB_COLOR: Record<Tab, string> = { movie: 'var(--c-movie)', tv: 'var(--c-tv)', anime: 'var(--c-anime)', music: 'var(--c-music)', ebook: 'var(--c-book)', audiobook: 'var(--c-audiobook)' }

const TAB_LABEL: Record<Tab, string> = { movie: t('Movies'), tv: t('TV'), anime: t('Anime'), music: t('Music'), ebook: t('Ebooks'), audiobook: t('Audiobooks') }

// `pane` is true while the Library is the left side of the tablet layout (see TwoPane): the posters
// come as a narrow column and the page keeps its tab from the title that is open beside it.
export default function Library({ pane = false }: { pane?: boolean }) {
  const [filtersOpen, setFiltersOpen] = useState(false)
  const confirm = useConfirm()
  const navigate = useNavigate()
  const openFromList = useOpenFromList()
  const loc = useLocation()
  const toast = useToast()
  // Removing titles, changing their quality profile and importing a library
  // are for administrators.
  const me = useAuth().user
  const admin = isAdmin(me)
  // Searching and monitoring need the "manage" permission; a viewer only looks.
  const canManage = can(me, 'manage')
  // The tab and the filters live in the address, so Back from a title brings the list back as it was.
  const [ui, patchUi] = useUrlState({ kind: pane ? paneDefaultTab(loc.pathname) : 'movie', st: 'all', q: '', genre: '', tag: '', decade: '', collection: '', profile: '', ...NO_FILE_FILTERS })
  const { on } = useModules()
  const musicOn = on('music')
  const booksOn = on('ebooks') || on('audiobooks')
  // The tabs: movies, TV and music (whichever are on), then ebooks and audiobooks.
  // The left menu lists the same ones (see libraryTabs.ts).
  const kinds: Tab[] = libraryTabs(on)
  // The Library opens on the first tab (Movies) unless the address asks for another;
  // a kind that is switched off cannot be shown, so the first that is on opens instead.
  const active: Tab = activeLibraryTab(kinds, ui.kind)
  // The anime tab is the TV view, for the anime shows (the TV tab then shows the others).
  const animeOn = on('anime') && on('tv')
  const onAnime = active === 'anime'
  const kind: Kind = active === 'tv' || onAnime ? 'tv' : 'movie'
  const onMovies = active === 'movie'
  const onTV = active === 'tv' || onAnime
  const [bookList, setBookList] = useState<Book[] | null>(null)
  const [queueNow, setQueueNow] = useState<QueueItem[]>([])
  const [artists, setArtists] = useState<MusicArtist[] | null>(null)
  const [view, setView] = useState<View>(() => (recall(STORAGE.view) === 'list' ? 'list' : 'grid'))
  const [movies, setMovies] = useState<Item[] | null>(null)
  const [shows, setShows] = useState<Item[] | null>(null)
  // How many movies and shows there are, for the tab whose list is not loaded.
  const [titleCounts, setTitleCounts] = useState<{ movies: number; shows: number; anime?: number } | null>(null)
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfile, setDefaultProfile] = useState(0)
  const [defaultTvProfile, setDefaultTvProfile] = useState(0)
  const [error, setError] = useState('')
  const text = ui.q
  const setText = (v: string) => patchUi({ q: v })
  const filter = ui.st
  const setFilter = (v: string) => patchUi({ st: v })
  // The sort and its direction are remembered in this browser.
  const [sort, setSortState] = useState<Sort>(() => {
    const saved = recall(STORAGE.sort)
    if (saved === 'year') return 'released' // the older name of this sort
    return SORTS.includes(saved as Sort) ? (saved as Sort) : 'added'
  })
  // The poster size in the grid, remembered in this browser.
  const [posterSize, setPosterSizeState] = useState(() => {
    const n = Number(recall(STORAGE.size))
    return n >= POSTER_MIN && n <= POSTER_MAX ? n : POSTER_DEFAULT
  })
  const setPosterSize = (n: number) => {
    setPosterSizeState(n)
    remember(STORAGE.size, String(n))
  }
  const [reverse, setReverseState] = useState(() => recall(STORAGE.reverse) === '1')
  const setSort = (s: Sort) => {
    setSortState(s)
    remember(STORAGE.sort, s)
  }
  const setReverse = (r: boolean) => {
    setReverseState(r)
    remember(STORAGE.reverse, r ? '1' : '0')
  }
  const genre = ui.genre
  const setGenre = (v: string) => patchUi({ genre: v })
  const tag = ui.tag
  const setTag = (v: string) => patchUi({ tag: v })
  const decade = ui.decade
  const setDecade = (v: string) => patchUi({ decade: v })
  const profileFilter = ui.profile
  const setProfileFilter = (v: string) => patchUi({ profile: v })
  const collection = ui.collection
  // What the files hold: video codec, HDR, resolution, audio and subtitle languages (read from the files' headers).
  const fileFilters: FileFilters = useMemo(() => ({ vc: ui.vc, hdr: ui.hdr, res: ui.res, al: ui.al, ac: ui.ac, sl: ui.sl, nal: ui.nal, nsl: ui.nsl }), [ui.vc, ui.hdr, ui.res, ui.al, ui.ac, ui.sl, ui.nal, ui.nsl])
  const setCollection = (v: string) => patchUi({ collection: v })
  // The series of films (trilogies, franchises) with a film in the library, for the Collection filter.
  const [collections, setCollections] = useState<CollectionListItem[]>([])
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const anchor = useRef<string | null>(null) // the last one ticked, for shift-click
  const [note, setNote] = useState<{ message: string; lines: string[] } | null>(null)
  const [busy, setBusy] = useState(false)
  // The folders a title can be kept in (the main one first), and the folder being moved to.
  const [libraryFolders, setLibraryFolders] = useState<{ movies: string[]; tv: string[] }>({ movies: [], tv: [] })
  const [moveTo, setMoveTo] = useState<{ folder: string; titles: BulkTitle[] } | null>(null)

  // Every few seconds the lists are read again; an answer that is the same as what is on screen changes nothing, so
  // a big library is not redrawn for no reason.
  const seen = useRef<{ movies?: [QueueItem[], Movie[]]; shows?: [QueueItem[], Series[]] }>({})
  const load = useCallback(() => {
    // All of it is asked at once; the queue only adds each title's progress, so nothing waits for it.
    const queue = api.listQueue().catch(() => [] as QueueItem[])
    const same = <T,>(next: T) => (prev: T | null): T => (prev !== null && JSON.stringify(prev) === JSON.stringify(next) ? prev : next)
    // A list that arrives clears an earlier message, so one failed refresh does not stay on the page.
    const failed = (e: unknown) => setError(e instanceof Error ? e.message : String(e))
    // The server answers "nothing changed" with the very same objects as last time. Then neither the titles nor the
    // queue are new, and there is nothing to build, compare or draw.
    // Only the open tab's list is read (on a big TV library the show list takes a while); the other tab's number
    // comes from the counts.
    if (onMovies)
      Promise.all([queue, api.listMovies()]).then(([q, m]) => {
        setError('')
        if (seen.current.movies?.[0] === q && seen.current.movies[1] === m) return
        seen.current.movies = [q, m]
        setMovies(same(m.map((x) => fromMovie(x, q))))
      }).catch(failed)
    if (onTV)
      Promise.all([queue, api.listSeries()]).then(([q, s]) => {
        setError('')
        if (seen.current.shows?.[0] === q && seen.current.shows[1] === s) return
        seen.current.shows = [q, s]
        setShows(same(s.map((x) => fromSeries(x, q))))
      }).catch(failed)
    api.libraryCounts().then((c) => setTitleCounts((prev) => (prev && prev.movies === c.movies && prev.shows === c.shows && prev.anime === c.anime ? prev : c))).catch(() => undefined)
    // Only the shown tab's list is read each time. The artists and books are also read once (below) for the numbers
    // on their tabs, so a Movies tab does not ask for them every few seconds.
    const tick = libraryTick(active)
    if (musicOn && tick.artists) api.listArtists().then((a) => { setArtists(same(a)); setError('') }).catch(failed)
    if (booksOn && tick.books) api.listBooks().then((b) => { setBookList(same(b)); setError('') }).catch(failed)
    queue.then((q) => setQueueNow(same(q)))
  }, [musicOn, booksOn, onMovies, onTV, active])
  useEffect(() => {
    load()
  }, [load])
  // The numbers on the Music and Book tabs: read once when the page opens (and when a module is switched on).
  useEffect(() => {
    const same = <T,>(next: T) => (prev: T | null): T => (prev !== null && JSON.stringify(prev) === JSON.stringify(next) ? prev : next)
    if (musicOn) api.listArtists().then((a) => setArtists(same(a))).catch(() => undefined)
    if (booksOn) api.listBooks().then((b) => setBookList(same(b))).catch(() => undefined)
  }, [musicOn, booksOn])
  // Every 4 seconds while a download moves, every 10 when nothing does.
  const downloading = anyMoving(queueNow)
  useLive(load, pollEvery(downloading, 4000, 10000))
  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(r.profiles)
      setDefaultProfile(r.defaultId)
      setDefaultTvProfile(defaultProfileFor('tv', r))
    }).catch(() => undefined)
  }, [])

  const movieCount = movies?.length
  useEffect(() => {
    api.collections().then(setCollections).catch(() => undefined)
  }, [movieCount])

  useEffect(() => {
    if (admin) api.libraryFolders().then(setLibraryFolders).catch(() => undefined)
  }, [admin])
  const folderChoices = kind === 'movie' ? libraryFolders.movies : libraryFolders.tv

  // With the anime library on, anime shows have their own tab and the TV tab the others.
  const tabShows = useMemo(() => (shows && animeOn ? shows.filter((s) => !!s.anime === onAnime) : shows), [shows, animeOn, onAnime])
  const items = kind === 'movie' ? movies : tabShows
  const shown = useMemo(() => {
    if (!items) return null
    const test = FILTERS[kind].find((f) => f.id === filter)?.test ?? (() => true)
    const needle = text.trim().toLowerCase()
    const inCollection = kind === 'movie' && collection ? new Set(collections.find((c) => String(c.id) === collection)?.movieIds ?? []) : null
    const list = items.filter(
      (i) =>
        test(i) &&
        (!needle || looseIncludes(i.title, needle)) &&
        (!genre || i.genres.includes(genre)) &&
        (!tag || i.tags.some((t) => t.toLowerCase() === tag.toLowerCase())) &&
        (!decade || (i.year >= Number(decade) && i.year < Number(decade) + 10)) &&
        matchesProfile(i, profileFilter, defaultProfile, defaultTvProfile) &&
        (!inCollection || inCollection.has(i.id)) &&
        matchesFileFilters(i.media, fileFilters),
    )
    if (sort === 'title') list.sort((a, b) => compareTitles(a.title, b.title))
    else if (sort === 'released') list.sort((a, b) => b.released.localeCompare(a.released) || compareTitles(a.title, b.title))
    else if (sort === 'size') list.sort((a, b) => largestFirst(a, b, (x, y) => compareTitles(x.title, y.title)))
    else if (sort === 'status') list.sort((a, b) => a.state.key.localeCompare(b.state.key) || compareTitles(a.title, b.title))
    else if (sort === 'profile') list.sort((a, b) => compareByProfile(a, b, profiles, defaultProfile, defaultTvProfile, (x, y) => compareTitles(x.title, y.title)))
    if (reverse) list.reverse()
    return list
  }, [items, kind, filter, text, sort, reverse, genre, tag, decade, profileFilter, profiles, defaultProfile, defaultTvProfile, collection, collections, fileFilters])
  // Everything that narrows the list (sorting does not), and the way back.
  const filtersActive =text.trim() !== '' || filter !== 'all' || genre !== '' || tag !== '' || decade !== '' || profileFilter !== '' || (kind === 'movie' && collection !== '') || fileFiltersActive(fileFilters)
  // What the files of this tab take on disk, for the line at the end of the chips.
  const totalText = useMemo(() => libraryTotalText(sumBytes(items ?? []), sumBytes(shown ?? []), filtersActive, kind), [items, shown, filtersActive, kind])
  function clearFilters() {
    patchUi({ q: '', st: 'all', genre: '', tag: '', decade: '', collection: '', profile: '', ...NO_FILE_FILTERS })
  }
  const fileOptions = useMemo(() => fileFilterOptions(items ?? []), [items])
  const langOption = (code: string) => ({ value: code, label: `${langName(code)} (${code.toUpperCase()})` })
  // How many of the files have had their video and audio tracks read; the rest are read a few at a time in the background.
  const [fileProgress, setFileProgress] = useState<{ done: number; total: number } | null>(null)
  useEffect(() => {
    if (!onMovies && !onTV) return
    let stop = false
    api.fileDetailsStatus().then((p) => !stop && setFileProgress((prev) => (prev && prev.done === p.done && prev.total === p.total ? prev : p))).catch(() => undefined)
    return () => {
      stop = true
    }
    // Looked at again when the tab changes, titles come or go, or downloads start or end: that is when new files can have appeared.
  }, [onMovies, onTV, movieCount, shows?.length, downloading])
  // While files are still being read it is asked for every 20 seconds (only while the tab is shown); once they are all read it stops.
  useLive(
    () => api.fileDetailsStatus().then((p) => setFileProgress((prev) => (prev && prev.done === p.done && prev.total === p.total ? prev : p))).catch(() => undefined),
    20000,
    (onMovies || onTV) && fileDetailsPending(fileProgress),
  )
  const genreOptions = useMemo(() => [...new Set((items ?? []).flatMap((i) => i.genres))].sort(), [items])
  const tagOptions = useMemo(() => [...new Set((items ?? []).flatMap((i) => i.tags))].sort((a, b) => a.localeCompare(b)), [items])
  const profileOptions = useMemo(() => profileFilterOptions(items ?? [], sortProfiles(profiles), defaultProfile, defaultTvProfile), [items, profiles, defaultProfile, defaultTvProfile])
  const decadeOptions = useMemo(() => [...new Set((items ?? []).filter((i) => i.year).map((i) => Math.floor(i.year / 10) * 10))].sort((a, b) => b - a), [items])

  function chooseKind(k: Tab) {
    patchUi({ kind: k, st: 'all' })
    setSelected(new Set())
  }
  function chooseView(v: View) {
    setView(v)
    remember(STORAGE.view, v)
  }

  const openPath = (i: Item) => openFromList(i.kind === 'movie' ? `/title/${i.tmdbId}` : `/series/${i.id}`)

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
      toast.info(`${i.title}: ${st(r.message)}`)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const setMonitored = (i: Item, v: boolean) =>
    run(() => (i.kind === 'movie' ? api.setMovieMonitored(i.id, v) : api.setSeriesMonitored(i.id, v)), v ? t('{title} is now monitored.', { title: i.title }) : t('{title} is no longer monitored.', { title: i.title }))

  // One title's Remove button on its card or row: the question and the
  // "also delete the files" box are the same as ever.
  async function removeOne(i: Item) {
    const usage = await (i.kind === 'movie' ? api.movieDiskUsage(i.id) : api.seriesDiskUsage(i.id)).catch(() => null)
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: i.title }),
      body: <p>{t('Mediarium stops tracking it. You can add it back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: removeOption(i.kind === 'movie' ? 'movie' : 'show', usage),
    })
    if (!answer) return
    const files = answer.checked
    await run(() => (i.kind === 'movie' ? api.deleteMovie(i.id, files) : api.deleteSeries(i.id, files)), files ? t('Removed {title} and its files.', { title: i.title }) : t('Removed {title}.', { title: i.title }))
  }

  const actionsFor = (i: Item): CardAction[] => [
    ...(canManage
      ? [
          { icon: 'search', label: t('Search for a release now'), onClick: () => void searchNow(i), disabled: busy } satisfies CardAction,
          { icon: i.monitored ? 'eye' : 'eye-off', label: i.monitored ? t('Monitored (click to stop)') : t('Not monitored (click to monitor)'), onClick: () => void setMonitored(i, !i.monitored), active: i.monitored, disabled: busy } satisfies CardAction,
        ]
      : []),
    { icon: 'open', label: t('Open'), onClick: () => navigate(openPath(i)) },
    ...(admin ? [{ icon: 'trash', label: t('Remove from library'), onClick: () => void removeOne(i), danger: true, disabled: busy } satisfies CardAction] : []),
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
      toast.success(st(r.message))
      return
    }
    const titleOf = (k: string, id: number) => (items ?? []).find((i) => i.kind === k && i.id === id)?.title
    setNote({ message: r.message, lines: failureLines(r.failed, titleOf) })
    toast.error(st(r.message))
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
      toast.info(t('Nothing to search for. Only monitored titles that are missing something are searched.'))
      return
    }
    const over = Math.max(0, wanted.length - BULK_SEARCH_LIMIT)
    const answer = await confirm({
      title: t('Search for {what} now?', { what: count(Math.min(wanted.length, BULK_SEARCH_LIMIT), noun) }),
      body: (
        <>
          <p>
            {plural(wanted.length, '{n} of the {total} selected is monitored and missing something, so that one is searched. The rest are left alone.', '{n} of the {total} selected are monitored and missing something, so those are searched. The rest are left alone.', { total: chosen.length })}
          </p>
          {over > 0 && (
            <p>
              {t('It searches at most {limit} titles at a time. The other {over} are left to the automatic search.', { limit: BULK_SEARCH_LIMIT, over })}
            </p>
          )}
          <p>{t('The search runs in the background. Anything it finds shows up in Activity.')}</p>
        </>
      ),
      confirmLabel: t('Search {n}', { n: Math.min(wanted.length, BULK_SEARCH_LIMIT) }),
    })
    if (!answer) return
    setBusy(true)
    setNote(null)
    try {
      const r = await api.bulkSearchNow(wanted.map((i) => ({ kind: i.kind, id: i.id })))
      if (r.failed.length > 0) report(r)
      else toast.info(st(r.message))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function replaceChosen() {
    const withFile = chosen.filter((i) => i.kind === 'movie' && i.status === 'downloaded')
    if (withFile.length === 0) {
      toast.info(t('Nothing to replace. Only movies that have a file can be replaced with a smaller version.'))
      return
    }
    const todo = withFile.slice(0, BULK_SEARCH_LIMIT)
    const total = todo.reduce((sum, i) => sum + i.sizeBytes, 0)
    const answer = await confirm({
      title: t('Replace {what} with a smaller version?', { what: count(todo.length, noun) }),
      body: (
        <>
          <p>
            <strong>{nameList(todo.map((i) => i.title))}</strong>
          </p>
          <p>{t('For each one, Mediarium looks for a release that its quality profile accepts and that is smaller than the file it has now. The files take up {size} together.', { size: formatBytes(total) })}</p>
          <p>{t('A file is only removed once its new version is downloaded and in your library. If nothing suitable is found, or a download fails, that file stays.')}</p>
          {withFile.length > todo.length && <p>{t('It does at most {limit} titles at a time. Run it again for the other {over}.', { limit: BULK_SEARCH_LIMIT, over: withFile.length - todo.length })}</p>}
        </>
      ),
      confirmLabel: t('Look for smaller versions'),
    })
    if (!answer) return
    setBusy(true)
    setNote(null)
    try {
      const r = await api.bulkReplaceSmaller(todo.map((i) => ({ kind: i.kind, id: i.id })))
      if (r.failed.length > 0) report(r)
      else toast.info(st(r.message))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  // Deletes the files that look incomplete among the chosen titles (only what
  // the view shows can be chosen) and searches for them again.
  async function redoIncompleteChosen() {
    const req = redoRequest(chosen)
    if (req.files === 0) {
      toast.info(t('None of the selected titles has a file that looks incomplete. Pick the Looks incomplete filter to find them.'))
      return
    }
    const titles = chosen.filter((i) => i.shortFiles > 0)
    const answer = await confirm({
      title: plural(req.files, 'Delete {n} incomplete file and search again?', 'Delete {n} incomplete files and search again?'),
      body: (
        <>
          <p>
            <strong>{nameList(titles.map((i) => i.title))}</strong>
          </p>
          <p>{t('These files are much shorter than the movie or episode should be, so they are most likely fakes or cut-off downloads. Each one is deleted (it goes to the recycle bin if that is on), the release it came from is blocklisted when Mediarium knows it, and Mediarium searches for another.')}</p>
          <p>{t('A real file that is short for another reason would be deleted too. Open a title first if you are not sure.')}</p>
        </>
      ),
      confirmLabel: t('Delete and search again'),
      danger: true,
    })
    if (!answer) return
    setBusy(true)
    setNote(null)
    try {
      toast.info(redoAnswerText(await api.redoIncomplete({ movieIds: req.movieIds, seriesIds: req.seriesIds })))
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
      title: t('Remove {title} from your library?', { title: what }),
      body: (
        <>
          <p>
            <strong>{nameList(chosen.map((i) => i.title))}</strong>
          </p>
          <p>
            {plural(n, 'Mediarium stops tracking it and cancels anything still downloading for it. You can add it back any time.', 'Mediarium stops tracking all {n} and cancels anything still downloading for them. You can add them back any time.')}
          </p>
          <p>
            {tx('{files} unless you tick the box below.', { files: <strong>{t('Your files stay where they are')}</strong> })}
          </p>
        </>
      ),
      confirmLabel: t('Remove {what}', { what }),
      danger: true,
      option: {
        label: n === 1 ? t('Also delete its files from the disk') : t('Also delete the files of all {n} from the disk', { n }),
        hint: t('This deletes their folders in your library too. It cannot be undone.'),
        defaultChecked: false,
        warning:
          n === 1
            ? noun === 'movie'
              ? t('The files of this movie will be deleted for good.')
              : t('The files of this show will be deleted for good.')
            : t('The files of all {what} will be deleted for good.', { what }),
      },
    })
    if (!answer) return
    await runBulk(() => api.bulkRemove(bulkTitles(), answer.checked))
  }

  const defaultChoiceLabel = useMemo(() => {
    const movieName = profiles.find((p) => p.id === defaultProfile)?.name
    const tvName = profiles.find((p) => p.id === defaultTvProfile)?.name
    if (movieName && tvName && movieName !== tvName) return t('Default ({movies} for movies, {tv} for TV)', { movies: movieName, tv: tvName })
    return t('Default ({name})', { name: movieName ?? t('the default profile') })
  }, [profiles, defaultProfile, defaultTvProfile])
  const profileChoices = useMemo(
    () => [
      { id: '0', label: defaultChoiceLabel },
      ...sortProfiles(profiles).map((p) => ({ id: String(p.id), label: p.name })),
    ],
    [profiles, defaultChoiceLabel],
  )

  const total = items?.length ?? 0

  // Only the kinds of media that are switched on get a tab.
  const bookCount = (f: BookFormat) => bookList?.filter((b) => b[f].wanted || b[f].status === 'downloaded').length
  const counts: Record<Tab, number | undefined> = {
    movie: active === 'movie' && movies ? movies.length : titleCounts?.movies,
    tv: onTV && shows ? (animeOn ? shows.filter((s) => !s.anime).length : shows.length) : titleCounts?.shows,
    anime: onTV && shows ? shows.filter((s) => s.anime).length : titleCounts?.anime,
    music: artists?.length,
    ebook: bookCount('ebook'),
    audiobook: bookCount('audiobook'),
  }
  const kindSwitch = (
    <div className="seg kind-tabs">
      {kinds.map((k) => (
        <button key={k} aria-pressed={active === k} className={active === k ? 'active' : ''} style={{ ['--kc' as string]: TAB_COLOR[k] }} onClick={() => chooseKind(k)}>
          <KindGlyph kind={k === 'ebook' ? 'book' : k === 'anime' ? 'tv' : k} size={17} />
          {TAB_LABEL[k]} {counts[k] !== undefined ? <small className="mono">{counts[k]}</small> : null}
        </button>
      ))}
    </div>
  )

  if (active === 'music') return <MusicLibrary artists={artists} error={error} switcher={kindSwitch} admin={admin} reload={load} />
  if (active === 'ebook' || active === 'audiobook') return <BookLibrary format={active} books={bookList} queue={queueNow} error={error} switcher={kindSwitch} reload={load} admin={admin} />

  return (
    <div>
      <div className={`toolbar lib-toolbar collapsible${filtersOpen ? ' filters-open' : ''}`}>
        {kindSwitch}
        <input type="search" placeholder={kind === 'movie' ? t('Find in your movies…') : onAnime ? t('Find in your anime…') : t('Find in your shows…')} value={text} onChange={(e) => setText(e.target.value)} aria-label={t('Find in your library')} />
        <FilterToggle open={filtersOpen} onToggle={() => setFiltersOpen((o) => !o)} active={filtersActive} />
        <div className="fold">
        {tagOptions.length > 0 && (
          <Dropdown icon="bookmark" label={t('Tag')} value={tag} onChange={setTag} options={[{ value: '', label: t('All tags') }, ...tagOptions.map((t) => ({ value: t, label: t }))]} />
        )}
        {genreOptions.length > 0 && (
          <Dropdown icon="sparkles" label={t('Genre')} value={genre} onChange={setGenre} options={[{ value: '', label: t('All genres') }, ...genreOptions.map((g) => ({ value: g, label: g }))]} />
        )}
        {kind === 'movie' && collections.length > 0 && (
          <Dropdown icon="film" label={t('Collection')} value={collection} onChange={setCollection} options={[{ value: '', label: t('All collections') }, ...collections.map((c) => ({ value: String(c.id), label: c.name }))]} />
        )}
        <Dropdown icon="calendar" label={t('Year')} value={decade} onChange={setDecade} options={[{ value: '', label: t('Any year') }, ...decadeOptions.map((d) => ({ value: String(d), label: t('{decade}s', { decade: String(d) }) }))]} />
        {profileOptions.length > 0 && (
          <Dropdown icon="sliders" label={t('Quality profile')} value={profileFilter} onChange={setProfileFilter} options={[{ value: '', label: t('All profiles') }, ...profileOptions.map((p) => ({ value: String(p.id), label: `${p.name} (${p.count})` }))]} />
        )}
        {fileOptions.videoCodecs.length > 0 && (
          <>
            <Dropdown icon="film" label={t('Video codec')} value={ui.vc} onChange={(v) => patchUi({ vc: v })} options={[{ value: '', label: t('Video codec') }, ...fileOptions.videoCodecs.map((c) => ({ value: c, label: c }))]} />
            <Dropdown icon="sparkles" label={t('HDR')} value={ui.hdr} onChange={(v) => patchUi({ hdr: v })} options={[{ value: '', label: t('HDR') }, { value: 'SDR', label: 'SDR' }, ...(fileOptions.hdr.length > 0 ? [{ value: 'hdr', label: t('Any HDR') }] : []), ...fileOptions.hdr.map((k) => ({ value: k, label: k }))]} />
            <Dropdown icon="monitor" label={t('Resolution')} value={ui.res} onChange={(v) => patchUi({ res: v })} options={[{ value: '', label: t('Resolution') }, ...fileOptions.resolutions.map((r) => ({ value: r, label: r }))]} />
            <Dropdown icon="volume" label={t('Audio language')} value={ui.al} onChange={(v) => patchUi({ al: v })} options={[{ value: '', label: t('Audio language') }, ...fileOptions.audioLangs.map(langOption)]} />
            <Dropdown icon="volume-off" label={t('Missing audio language')} value={ui.nal} onChange={(v) => patchUi({ nal: v })} options={[{ value: '', label: t('Missing audio') }, ...fileOptions.audioLangs.map((c) => ({ value: c, label: t('No {language} audio', { language: langName(c) }) }))]} />
            <Dropdown icon="headphones" label={t('Audio codec')} value={ui.ac} onChange={(v) => patchUi({ ac: v })} options={[{ value: '', label: t('Audio codec') }, ...fileOptions.audioCodecs.map((c) => ({ value: c, label: c }))]} />
            <Dropdown icon="chat" label={t('Subtitle language')} value={ui.sl} onChange={(v) => patchUi({ sl: v })} options={[{ value: '', label: t('Subtitles') }, ...fileOptions.subLangs.map(langOption)]} />
            <Dropdown icon="chat" label={t('Missing subtitle language')} value={ui.nsl} onChange={(v) => patchUi({ nsl: v })} options={[{ value: '', label: t('Missing subtitles') }, ...[...new Set([...fileOptions.audioLangs, ...fileOptions.subLangs])].sort((a, b) => a.localeCompare(b)).map((c) => ({ value: c, label: t('No {language} subtitles', { language: langName(c) }) }))]} />
          </>
        )}
        <Dropdown
          icon="sort"
          label={t('Sort')}
          value={sort}
          onChange={(v) => setSort(v as Sort)}
          options={[
            { value: 'added', label: t('Recently added') },
            { value: 'title', label: t('Title A–Z') },
            { value: 'released', label: t('Newest release') },
            { value: 'size', label: t('Largest on disk') },
            { value: 'status', label: t('Status') },
            { value: 'profile', label: t('Quality profile') },
          ]}
        />
        <button className="icon-btn" onClick={() => setReverse(!reverse)} aria-pressed={reverse} title={reverse ? t('Reversed order. Click for the normal order.') : t('Reverse the order')} aria-label={t('Reverse the order')}>
          <Icon name="sort" size={16} />
        </button>
        <div className="toolbar-end">
          {admin && (
            <button className={`btn-with-icon${selecting ? ' primary' : ''}`} aria-pressed={selecting} onClick={() => (selecting ? stopSelecting() : setSelecting(true))}>
              <Icon name="check" size={16} /> {t('Select')}
            </button>
          )}
          {admin && <HelpLink topic="library#selecting-many-titles" text={t('Pick several titles, then change them all at once: monitoring, quality profile, where they download from, tags, folder, searching or removing. Nothing changes until you press one of the actions.')} />}
          {view === 'grid' && (
            <label className="poster-size" title={t('Poster size')}>
              <Icon name="image" size={14} />
              <input
                type="range"
                min={POSTER_MIN}
                max={POSTER_MAX}
                step={2}
                value={posterSize}
                onChange={(e) => setPosterSize(Number(e.target.value))}
                aria-label={t('Poster size')}
              />
            </label>
          )}
          <div className="seg">
            <button aria-pressed={view === 'grid'} className={view === 'grid' ? 'active' : ''} onClick={() => chooseView('grid')} aria-label={t('Grid view')}>
              <Icon name="grid" size={16} />
            </button>
            <button aria-pressed={view === 'list'} className={view === 'list' ? 'active' : ''} onClick={() => chooseView('list')} aria-label={t('List view')}>
              <Icon name="list" size={16} />
            </button>
          </div>
          {admin && (
            <button className="btn-with-icon" onClick={() => navigate(`/import?kind=${kind}`)}>
              <Icon name="folder" size={16} /> {t('Import')}
            </button>
          )}
        </div>
        </div>
        <button className="primary btn-with-icon toolbar-add" onClick={() => navigate('/search')}>
          <Icon name="plus" size={16} /> {t('Add')}
        </button>
      </div>

      {fileProgress && fileProgress.total > 0 && fileProgress.done < fileProgress.total && (
        <p className="file-note">{t('Reading the video and audio details of your files in the background: {done} of {total} done. The file filters cover the files read so far.', { done: fileProgress.done.toLocaleString(), total: fileProgress.total.toLocaleString() })}</p>
      )}
      {totalText && (
        <p className="lib-total-line mono">
          <Icon name="folder" size={14} /> {totalText}
        </p>
      )}
      <div className="chip-row state-chips" style={{ marginBottom: 18, alignItems: 'center' }}>
        <InfoTip
          text={t(
            'Downloaded: the file is in your library.\nDownloading: coming in now.\nPending: queued and waiting its turn.\nWaiting for a release: monitored, and Mediarium keeps checking.\nPartial: some episodes are downloaded.\nNot monitored: in your library, but nothing downloads by itself.\nFailed: the last attempt failed.',
          )}
        />
        {FILTERS[kind].filter((f) => f.id !== 'incomplete' || filter === f.id || items?.some(f.test)).map((f) => (
          <button key={f.id} className={`chip st-${f.id}${filter === f.id ? ' active' : ''}${f.id === 'downloading' && items?.some(f.test) ? ' live' : ''}`} onClick={() => setFilter(f.id)} title={f.id === 'incomplete' ? t('Files that are much shorter than the movie or episode should be: fakes, samples or cut-off downloads.') : undefined}>
            <Icon name={FILTER_ICON[f.id] ?? 'grid'} size={14} />
            {f.label} {items ? <small>{items.filter(f.test).length}</small> : null}
          </button>
        ))}
        {filtersActive && (
          <button className="btn-sm" onClick={clearFilters}>
            {t('Clear filters')}
          </button>
        )}
        {totalText && (
          <span className="lib-total mono" title={t('What the downloaded files take on disk')}>
            <Icon name="folder" size={14} /> {totalText}
          </span>
        )}
      </div>

      {selecting && admin && (
        <BulkBar text={selText} onSelectAll={() => shown && setSelected(selectAllKeys(shown))} onSelectNone={() => setSelected(new Set())} onDone={stopSelecting} busy={busy}>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkMonitored(bulkTitles(), true))}>
            <Icon name="eye" size={15} /> {t('Monitor')}
          </button>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkMonitored(bulkTitles(), false))}>
            <Icon name="eye-off" size={15} /> {t('Stop monitoring')}
          </button>
          <InfoTip text={t('Monitor turns automatic searching on for the selected titles; Stop monitoring turns it off and also removes downloads that were only waiting for them.')} />
          <ActionMenu
            label={t('Better versions')}
            disabled={chosen.length === 0 || busy}
            onPick={(id) => void runBulk(() => api.bulkNoUpgrade(bulkTitles(), id === 'leave'))}
            choices={[
              { id: 'look', label: t('Look for better versions'), hint: t('Swap a download for a better one when its quality profile allows it.') },
              { id: 'leave', label: t('Leave what I have alone'), hint: t('Keep the files as they are, even if a better one is out there.') },
            ]}
          />
          <InfoTip text={t("Look for better versions lets Mediarium swap a download for a better one when the title's quality profile allows it. Leave what I have alone stops that for these titles, whatever their profile says.")} />
          <ActionMenu label={t('Quality profile')} disabled={chosen.length === 0 || busy || profiles.length === 0} choices={profileChoices} onPick={(id) => void runBulk(() => api.bulkProfile(bulkTitles(), Number(id)))} />
          <InfoTip text={t("Gives the selected titles another quality profile, or the default. A title whose file is below the new profile's cutoff starts looking for that quality straight away.")} />
          <BulkTagMenu
            disabled={chosen.length === 0 || busy}
            inSelection={[...new Set(chosen.flatMap((i) => i.tags))].sort((a, b) => a.localeCompare(b))}
            onAdd={(t) => void runBulk(() => api.bulkTags(bulkTitles(), [t], []))}
            onRemove={(t) => void runBulk(() => api.bulkTags(bulkTitles(), [], [t]))}
          />
          <ActionMenu
            label={t('Download from')}
            disabled={chosen.length === 0 || busy}
            onPick={(id) => void runBulk(() => api.bulkSources(bulkTitles(), (id === 'default' ? '' : id) as SourcePref))}
            choices={[
              { id: 'default', label: t('Use the default') },
              { id: 'usenet', label: t('Usenet only') },
              { id: 'torrent', label: t('Torrents only') },
              { id: 'both', label: t('Usenet and torrents') },
            ]}
          />
          <InfoTip text={t('Which downloaders the selected titles may use: Usenet only, torrents only, both, or whatever your settings say.')} />
          {folderChoices.length > 1 && (
            <ActionMenu
              label={t('Move to folder')}
              disabled={chosen.length === 0 || busy}
              choices={folderChoices.map((f, i) => ({ id: String(i), label: f, hint: i === 0 ? t('The main folder') : undefined }))}
              onPick={(id) => setMoveTo({ folder: folderChoices[Number(id)] ?? folderChoices[0], titles: bulkTitles() })}
            />
          )}
          {folderChoices.length > 1 && <InfoTip text={t('Sends the next files of the selected titles to another library folder. You can also move the files that are already there; Mediarium shows what would happen before it starts.')} />}
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void searchChosen()}>
            <Icon name="search" size={15} /> {t('Search now')}
          </button>
          <InfoTip text={t('Looks for releases of the selected titles that are monitored and missing something, at most 25 at a time. The search runs in the background and anything it finds shows up in Activity.')} />
          {kind === 'movie' && (
            <>
              <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void replaceChosen()}>
                <Icon name="download" size={15} /> {t('Replace with smaller')}
              </button>
              <InfoTip text={t('For movies you moved to a lower quality profile. Looks for a smaller release the profile accepts, downloads it, and only then removes the big file. At most 25 at a time.')} />
            </>
          )}
          {shown?.some((i) => i.shortFiles > 0) && (
            <>
              <button className="btn-sm btn-danger btn-with-icon" disabled={!chosen.some((i) => i.shortFiles > 0) || busy} onClick={() => void redoIncompleteChosen()}>
                <Icon name="warning" size={15} /> {t('Delete incomplete files and search again')} <small className="mono">{redoRequest(chosen).files}</small>
              </button>
              <InfoTip text={t('For the selected titles that have a file much shorter than the movie or episode should be. Deletes those files, blocklists their releases when known, and searches again. Only the titles you selected are touched.')} />
            </>
          )}
          <button className="btn-sm btn-danger btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void removeChosen()}>
            <Icon name="trash" size={15} /> {t('Remove from library')}
          </button>
        </BulkBar>
      )}

      {moveTo !== null && (
        <MoveToFolderDialog
          kind={kind}
          titles={moveTo.titles}
          folders={folderChoices}
          initial={moveTo.folder}
          onClose={() => setMoveTo(null)}
          onChanged={load}
        />
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
            {t('Dismiss')}
          </button>
        </div>
      )}

      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid" key={`grid-${active}`} style={{ '--poster-min': `${posterSize}px` } as CSSProperties}>
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <>
        <div className="empty-state" key={`empty-${active}`}>
          <Icon name={kind === 'movie' ? 'film' : 'tv'} size={44} />
          {total === 0 ? (
            <>
              <p>{kind === 'movie' ? t('Your movie library is empty.') : onAnime ? t("No anime yet. An anime show you add lands here, and so does a show whose episode numbering you set to Anime.") : t('Your TV library is empty.')}</p>
              <div className="row-actions">
                <button className="primary btn-with-icon" onClick={() => navigate('/search')}>
                  <Icon name="search" size={16} /> {kind === 'movie' ? t('Search to add a movie') : t('Search to add a show')}
                </button>
                {admin && (
                  <button className="btn-with-icon" onClick={() => navigate(`/import?kind=${kind}`)}>
                    <Icon name="folder" size={16} /> {t('Import what you already have')}
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
              <p>{t('Nothing matches these filters.')}</p>
              <button className="btn-with-icon" onClick={clearFilters}>
                {t('Clear filters')}
              </button>
            </>
          )}
        </div>
        {total === 0 && <StarterPicks key={`picks-${kind}`} kind={kind} onAdded={load} />}
        </>
      ) : view === 'grid' || pane ? (
        <WindowedGrid
          key={`grid-${active}`}
          style={{ '--poster-min': `${posterSize}px` } as CSSProperties}
          items={shown}
          render={(i) => (
            <PosterCard
              key={i.key}
              to={openPath(i)}
              poster={i.posterUrl}
              title={withNameTag(i.title, i.tag, i.year)}
              meta={[i.year || null, i.kind === 'movie' ? i.quality : i.gettingDetails ? null : t('{have}/{total} episodes', { have: i.have, total: i.total }), i.copies ? plural(i.copies, '+{n} copy', '+{n} copies') : null, i.monitored ? null : t('unmonitored')].filter(Boolean).join(' · ')}
              state={i.state}
              kind={i.kind}
              progress={i.live ? { pct: i.live.progressPct, label: `${i.live.sizeBytes ? t('{pct}% of {size}', { pct: Math.round(i.live.progressPct), size: formatBytes(i.live.sizeBytes) }) : `${Math.round(i.live.progressPct)}%`} · ${i.live.protocol}` } : undefined}
              dim={!i.monitored}
              ribbon={showRibbon(i.showStatus)}
              actions={actionsFor(i)}
              selection={selecting ? { selected: selected.has(i.key), onToggle: (e) => pick(i.key, e.shiftKey) } : undefined}
              footer={
                (i.kind === 'tv' && i.total > 0 && !i.gettingDetails) || i.tags.length > 0 ? (
                  <>
                    {i.kind === 'tv' && i.total > 0 && !i.gettingDetails && (
                      <div className="bar" style={{ width: '100%', marginTop: 6 }} title={t('{have} of {total} episodes', { have: i.have, total: i.total })}>
                        <span style={{ width: `${(i.have / i.total) * 100}%` }} />
                      </div>
                    )}
                    {i.tags.length > 0 && (
                      <span className="pcard-tags">
                        <TagChips tags={i.tags} />
                      </span>
                    )}
                  </>
                ) : undefined
              }
            />
          )}
        />
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
                    aria-label={t('Select all in this view')}
                  />
                </th>
              )}
              <th>{t('Title')}</th>
              <th>{t('Status')}</th>
              <th>{kind === 'movie' ? t('Quality') : t('Episodes')}</th>
              <th aria-sort={sort === 'profile' ? (reverse ? 'descending' : 'ascending') : 'none'}>
                <button type="button" className="th-sort" onClick={() => (sort === 'profile' ? setReverse(!reverse) : setSort('profile'))} title={t('Sort by quality profile')}>
                  {t('Profile')}
                  <span className="th-arrow" aria-hidden="true">
                    {sort === 'profile' ? (reverse ? '▼' : '▲') : ''}
                  </span>
                </button>
              </th>
              <th />
            </tr>
          </thead>
          <WindowedBody
            items={shown}
            columns={selecting ? 6 : 5}
            render={(i) => (
              <tr key={i.key} className={selecting && selected.has(i.key) ? 'selected' : undefined} style={i.monitored ? undefined : { opacity: 0.65 }}>
                {selecting && (
                  <td>
                    <input type="checkbox" checked={selected.has(i.key)} onChange={() => undefined} onClick={(e) => pick(i.key, e.shiftKey)} aria-label={t('Select {title}', { title: i.title })} />
                  </td>
                )}
                <td>
                  <Link to={openPath(i)} className="lib-title" onClick={selecting ? (e) => { e.preventDefault(); pick(i.key, e.shiftKey) } : undefined}>
                    <span className="lib-thumb">{i.posterUrl ? <img src={i.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</span>
                    <span>
                      <strong>{withNameTag(i.title, i.tag, i.year)}</strong>
                      <small>
                        {i.year || '—'}
                        {i.monitored ? '' : ` · ${t('unmonitored')}`}
                        {isFinishedShow(i.showStatus) ? ` · ${showStatusLabel(i.showStatus)}` : ''}
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
                <td style={{ color: 'var(--text-dim)' }}>{profileName(i, profiles, defaultProfile, defaultTvProfile) || '—'}</td>
                <td>
                  <div className="row-actions">
                    {actionsFor(i).map((a) => (
                      <button key={a.label} className={`icon-btn${a.active ? ' on' : ''}${a.danger ? ' danger' : ''}`} title={a.label} aria-label={a.label} aria-pressed={a.active === undefined ? undefined : a.active} disabled={a.disabled} onClick={a.onClick}>
                        <Icon name={a.icon} size={17} />
                      </button>
                    ))}
                  </div>
                </td>
              </tr>
            )}
          />
        </table>
      )}
    </div>
  )
}
