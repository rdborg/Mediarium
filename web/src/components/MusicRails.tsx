import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type MusicDiscoverArtist, type MusicDiscoverItem, type MusicList, type MusicRange, type MusicType } from '../api'
import { comingLabel } from '../discoverSources'
import { useGridColumns } from '../useGridColumns'
import { useHideOwned } from '../useHideOwned'
import { scrollToTop } from '../scrollTop'
import type { AddMusicTarget } from './AddMusicDialog'
import Cover from './Cover'
import Dropdown from './Dropdown'
import Icon from './Icon'
import PosterCard from './PosterCard'
import type { ItemState } from './state'

export type MusicListId = MusicList | 'artists'

export const MUSIC_RANGES: { value: MusicRange; label: string }[] = [
  { value: 'week', label: 'This week' },
  { value: 'month', label: 'This month' },
  { value: 'year', label: 'This year' },
  { value: 'all_time', label: 'All time' },
]

const TYPES: { value: MusicType; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'album', label: 'Albums' },
  { value: 'ep', label: 'EPs' },
  { value: 'single', label: 'Singles' },
]

// The genres offered in the genre picker of Discover. A pick also finds the
// kinds of it, so Rock finds punk rock and hard rock.
export const MUSIC_GENRES = ['Alternative', 'Ambient', 'Blues', 'Classical', 'Country', 'Dance', 'Electronic', 'Folk', 'Funk', 'Hip hop', 'Indie', 'Jazz', 'Latin', 'Metal', 'Pop', 'Punk', 'R&B', 'Reggae', 'Rock', 'Soul']

export const MUSIC_TYPE_OPTIONS: { value: MusicType; label: string }[] = [
  { value: 'all', label: 'All types' },
  { value: 'album', label: 'Albums' },
  { value: 'ep', label: 'EPs' },
  { value: 'single', label: 'Singles' },
]

const TYPE_WORD: Record<string, string> = { album: 'Album', ep: 'EP', single: 'Single' }

const RANGE_TITLE: Record<MusicRange, string> = { week: 'this week', month: 'this month', year: 'this year', all_time: 'of all time' }

export function musicListTitle(list: MusicListId, range: MusicRange = 'week'): string {
  if (list === 'new') return 'New releases'
  if (list === 'upcoming') return 'Coming soon'
  return `${list === 'artists' ? 'Popular artists' : 'Popular'} ${RANGE_TITLE[range]}`
}

// The rails on Discover keep their names short and fixed; the browse page
// adds the time range.
const RAIL_TITLE: Record<MusicListId, string> = { popular: 'Popular this week', new: 'New releases', upcoming: 'Coming soon', artists: 'Popular artists' }
const RAIL_HINT: Record<MusicListId, string | undefined> = { popular: undefined, new: 'just out', upcoming: 'add them now and they download when released', artists: 'what people are listening to' }

const IN_LIBRARY: ItemState = { key: 'downloaded', label: 'In your library', icon: 'check', hint: 'Already in your library.' }

// ---- Filters --------------------------------------------------------------

// The type buttons and the genre picker (only when some genre is known).
export function MusicFilters({ type, onType, genre, onGenre, genres }: { type: MusicType; onType: (t: MusicType) => void; genre: string; onGenre: (g: string) => void; genres: string[] }) {
  return (
    <>
      <div className="seg" role="group" aria-label="Kind of release">
        {TYPES.map((t) => (
          <button key={t.value} className={type === t.value ? 'active' : ''} onClick={() => onType(t.value)}>
            {t.label}
          </button>
        ))}
      </div>
      {genres.length > 0 && (
        <Dropdown label="Genre" value={genre} onChange={onGenre} options={[{ value: '', label: 'All genres' }, ...genres.map((g) => ({ value: g, label: g }))]} />
      )}
    </>
  )
}

// Genres seen so far across the loaded lists, kept in a stable order, so the
// genre picker fills up as rails arrive.
export function useKnownGenres(): [string[], (found: string[]) => void] {
  const [genres, setGenres] = useState<string[]>([])
  const add = (found: string[]) =>
    setGenres((cur) => {
      const fresh = found.filter((g) => g && !cur.includes(g))
      return fresh.length === 0 ? cur : [...cur, ...fresh].sort((a, b) => a.localeCompare(b))
    })
  return [genres, add]
}

// ---- Cards ----------------------------------------------------------------

function ownedPath(i: { artistId?: number; albumId?: number }): string | undefined {
  if (!i.artistId) return undefined
  return `/music/artist/${i.artistId}${i.albumId ? `#album-${i.albumId}` : ''}`
}

export function AlbumCards({ items, onAdd }: { items: MusicDiscoverItem[]; onAdd: (t: AddMusicTarget) => void }) {
  const navigate = useNavigate()
  return (
    <>
      {items.map((m) => {
        const path = m.inLibrary ? ownedPath(m) : undefined
        const coming = comingLabel(m.releaseDate)
        const year = m.releaseDate ? m.releaseDate.slice(0, 4) : ''
        return (
          <PosterCard
            key={m.mbid}
            to={path}
            poster={m.coverUrl}
            title={m.title}
            meta={
              <>
                {m.artistName}
                {coming ? (
                  <>
                    {' · '}
                    <span className="coming-soon">{coming}</span>
                  </>
                ) : year ? (
                  ` · ${year}`
                ) : (
                  ''
                )}
              </>
            }
            genres={m.genres}
            kind="music"
            state={m.inLibrary ? IN_LIBRARY : undefined}
            footer={
              <div className="pcard-footer music-foot">
                <span className="badge" title="Kind of release">
                  {TYPE_WORD[m.type] ?? 'Release'}
                </span>
                {m.inLibrary ? (
                  path && (
                    <button className="btn-sm btn-with-icon" onClick={() => navigate(path)}>
                      <Icon name="open" size={15} /> Open
                    </button>
                  )
                ) : (
                  <button className="primary btn-sm btn-with-icon" onClick={() => onAdd({ kind: 'album', item: m })}>
                    <Icon name="plus" size={15} /> {coming ? 'Add early' : 'Add'}
                  </button>
                )}
              </div>
            }
          />
        )
      })}
    </>
  )
}

export function ArtistCards({ items, onAdd }: { items: MusicDiscoverArtist[]; onAdd: (t: AddMusicTarget) => void }) {
  const navigate = useNavigate()
  return (
    <>
      {items.map((a) => {
        const path = a.inLibrary && a.artistId ? `/music/artist/${a.artistId}` : undefined
        return (
          <article key={a.mbid} className="artist-card">
            <div className="artist-cover">
              <Cover src={a.coverUrl} />
            </div>
            <div className="artist-text">
              <strong title={a.name}>{a.name}</strong>
              {a.listenCount > 0 && <small>{a.listenCount.toLocaleString()} listens</small>}
            </div>
            <div className="artist-action">
              {a.inLibrary ? (
                <>
                  <span className="state-tag st-downloaded">
                    <Icon name="check" size={12} /> In your library
                  </span>
                  {path && (
                    <button className="btn-sm btn-with-icon" onClick={() => navigate(path)}>
                      <Icon name="open" size={15} /> Open
                    </button>
                  )}
                </>
              ) : (
                <button className="primary btn-sm btn-with-icon" onClick={() => onAdd({ kind: 'artist', mbid: a.mbid, name: a.name })}>
                  <Icon name="plus" size={15} /> Add
                </button>
              )}
            </div>
          </article>
        )
      })}
    </>
  )
}

// ---- A rail ---------------------------------------------------------------

interface Fetched<T> {
  items: T[]
  totalPages?: number
  note?: string
}

// Loads pages of one list until `want` items are in hand. Starts over when
// `dep` changes (a filter was picked).
function useFilled<T>(fetchPage: (page: number) => Promise<Fetched<T>>, keyOf: (t: T) => string, dep: string, want: number, keep?: (t: T) => boolean) {
  const [state, setState] = useState<{ key: string; items: T[]; page: number; totalPages?: number; note: string; error: string; done: boolean }>({ key: dep, items: [], page: 0, note: '', error: '', done: false })
  const busy = useRef(false)
  const gen = useRef(0)

  useEffect(() => {
    gen.current++
    busy.current = false
    setState({ key: dep, items: [], page: 0, note: '', error: '', done: false })
  }, [dep])

  useEffect(() => {
    const kept = keep ? state.items.filter(keep).length : state.items.length
    if (state.key !== dep || busy.current || state.done || state.error || kept >= want) return
    busy.current = true
    const mine = gen.current
    fetchPage(state.page + 1)
      .then((r) => {
        if (mine !== gen.current) return
        setState((cur) => {
          const seen = new Set(cur.items.map(keyOf))
          const items = [...cur.items, ...r.items.filter((x) => !seen.has(keyOf(x)))]
          const done = r.items.length === 0 || (r.totalPages !== undefined && cur.page + 1 >= r.totalPages)
          return { ...cur, items, page: cur.page + 1, totalPages: r.totalPages, note: r.note ?? cur.note, error: '', done }
        })
      })
      .catch((e) => {
        if (mine !== gen.current) return
        setState((cur) => ({ ...cur, error: e instanceof Error ? e.message : String(e), done: true }))
      })
      .finally(() => {
        if (mine === gen.current) busy.current = false
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, want, dep, !!keep])

  return { ...state, loading: state.page === 0 && !state.done }
}

function Shell({ id, title, hint, browse, note, children, foot }: { id: string; title: string; hint?: string; browse: string; note?: string; children: ReactNode; foot?: ReactNode }) {
  return (
    <section className="discover-rail music-rail" data-rail={id}>
      <div className="rail-head">
        <h2>{title}</h2>
        {hint && <span>{hint}</span>}
        <Link to={`/discover/music?${browse}`} className="rail-more">
          Browse all <Icon name="chevron-right" size={15} />
        </Link>
      </div>
      {note && <p className="music-note">{note}</p>}
      {children}
      {foot && <div className="rail-foot">{foot}</div>}
    </section>
  )
}

function wholeRows<T>(items: T[], want: number, cols: number): T[] {
  return items.length >= want ? items.slice(0, want) : items.slice(0, Math.max(cols, Math.floor(items.length / cols) * cols))
}

interface RailProps {
  list: MusicListId
  type: MusicType
  genre: string
  onAdd: (t: AddMusicTarget) => void
  onGenres?: (g: string[]) => void
  rows?: number
}

// One row of albums (or a couple of rows) for a list, always whole rows.
function AlbumRail({ list, type, genre, onAdd, onGenres, rows = 2 }: RailProps & { list: MusicList }) {
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid)
  const [extra, setExtra] = useState(0)
  const want = cols * (rows + extra)
  const range: MusicRange = 'week'
  const dep = `${list}|${type}|${genre}`
  const [hideOwned] = useHideOwned()
  const r = useFilled<MusicDiscoverItem>(
    (page) => api.musicDiscover({ list, type, range: list === 'popular' ? range : undefined, genre: genre || undefined, page, pageSize: 24 }).then((p) => ({ items: p.items ?? [], totalPages: p.totalPages, note: p.note })),
    (m) => m.mbid,
    dep,
    want,
    hideOwned ? (m) => !m.inLibrary : undefined,
  )
  useEffect(() => {
    if (onGenres && r.items.length > 0) onGenres(r.items.flatMap((i) => i.genres ?? []))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [r.items])

  const browse = new URLSearchParams({ list, ...(list === 'popular' ? { range } : {}), ...(type !== 'all' ? { type } : {}), ...(genre ? { genre } : {}) }).toString()
  const items = hideOwned ? r.items.filter((m) => !m.inLibrary) : r.items
  const shown = wholeRows(items, want, cols)
  const fetching = !r.done && items.length < want
  const unavailable = !r.loading && r.items.length === 0
  // A list with nothing to say and no reason is left out, and so is one where
  // everything is already in the library while those are hidden.
  if (unavailable && !r.note && !r.error) return null
  if (!r.loading && r.done && items.length === 0 && r.items.length > 0) return null
  return (
    <Shell id={list} title={RAIL_TITLE[list]} hint={RAIL_HINT[list]} browse={browse} note={unavailable ? r.note || r.error : r.note} foot={!unavailable && !r.done && !r.error ? <MoreButton onClick={() => setExtra((x) => x + 2)} busy={fetching} /> : undefined}>
      <div className="poster-grid" ref={grid} style={unavailable ? { display: 'none' } : undefined}>
        {r.loading ? Array.from({ length: want }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '1 / 1.9' }} />) : <AlbumCards items={shown} onAdd={onAdd} />}
        {!r.loading && fetching && Array.from({ length: Math.max(0, want - shown.length) }, (_, i) => <div key={`s${i}`} className="skeleton" style={{ aspectRatio: '1 / 1.9' }} />)}
      </div>
    </Shell>
  )
}

function MoreButton({ onClick, busy }: { onClick: () => void; busy: boolean }) {
  return (
    <button className="btn-sm btn-with-icon" onClick={onClick} disabled={busy}>
      <Icon name="plus" size={15} /> Load more
    </button>
  )
}

function ArtistRail({ onAdd, rows = 3 }: Pick<RailProps, 'onAdd' | 'rows'>) {
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid, 3)
  const [extra, setExtra] = useState(0)
  const want = cols * (rows + extra)
  const range: MusicRange = 'week'
  const [hideOwned] = useHideOwned()
  const r = useFilled<MusicDiscoverArtist>(
    (page) => api.musicDiscoverArtists({ range, page }).then((p) => ({ items: p.items ?? [], totalPages: p.totalPages, note: p.note })),
    (a) => a.mbid,
    'artists',
    want,
    hideOwned ? (a) => !a.inLibrary : undefined,
  )
  const items = hideOwned ? r.items.filter((a) => !a.inLibrary) : r.items
  const shown = wholeRows(items, want, cols)
  const fetching = !r.done && items.length < want
  const unavailable = !r.loading && r.items.length === 0
  if (unavailable && !r.note && !r.error) return null
  if (!r.loading && r.done && items.length === 0 && r.items.length > 0) return null
  return (
    <Shell id="artists" title={RAIL_TITLE.artists} hint={RAIL_HINT.artists} browse={new URLSearchParams({ list: 'artists', range }).toString()} note={unavailable ? r.note || r.error : r.note} foot={!unavailable && !r.done && !r.error ? <MoreButton onClick={() => setExtra((x) => x + 2)} busy={fetching} /> : undefined}>
      <div className="artist-results" ref={grid} style={unavailable ? { display: 'none' } : undefined}>
        {r.loading ? Array.from({ length: want }, (_, i) => <div key={i} className="skeleton" style={{ height: 130 }} />) : <ArtistCards items={shown} onAdd={onAdd} />}
        {!r.loading && fetching && Array.from({ length: Math.max(0, want - shown.length) }, (_, i) => <div key={`s${i}`} className="skeleton" style={{ height: 130 }} />)}
      </div>
    </Shell>
  )
}

// All the music rails of Discover: what is popular, what is new, what is
// coming, and the artists people are playing.
export default function MusicRails({ type, genre, onAdd, onGenres, artists = true }: { type: MusicType; genre: string; onAdd: (t: AddMusicTarget) => void; onGenres?: (g: string[]) => void; artists?: boolean }) {
  return (
    <>
      <AlbumRail list="popular" type={type} genre={genre} onAdd={onAdd} onGenres={onGenres} />
      <AlbumRail list="new" type={type} genre={genre} onAdd={onAdd} onGenres={onGenres} />
      <AlbumRail list="upcoming" type={type} genre={genre} onAdd={onAdd} onGenres={onGenres} />
      {artists && <ArtistRail onAdd={onAdd} />}
    </>
  )
}

// ---- Filtered browse ------------------------------------------------------

export interface MusicBrowseFilters {
  type: MusicType
  genre: string
  yearFrom: string
  yearTo: string
  sort: string
}

const WORD_FOR_TYPE: Record<MusicType, string> = { all: 'music', album: 'albums', ep: 'EPs', single: 'singles' }

// "Rock albums from 1990 to 1999", named like the movie and show lists.
export function musicBrowseTitle(f: MusicBrowseFilters): string {
  const years = f.yearFrom && f.yearTo ? ` from ${f.yearFrom} to ${f.yearTo}` : f.yearFrom ? ` from ${f.yearFrom}` : f.yearTo ? ` up to ${f.yearTo}` : ''
  const what = WORD_FOR_TYPE[f.type]
  const named = f.genre ? `${f.genre} ${what}` : what.charAt(0).toUpperCase() + what.slice(1)
  return named + years
}

function Pager({ page, pages, more, onGo }: { page: number; pages?: number; more: boolean; onGo: (n: number) => void }) {
  const numbers: (number | '…')[] = []
  const from = Math.max(1, page - 2)
  const to = Math.min(pages ?? page + 2, page + 2)
  if (from > 1) numbers.push(1, ...(from > 2 ? (['…'] as const) : []))
  for (let n = from; n <= to; n++) numbers.push(n)
  if (more && to < (pages ?? Infinity)) numbers.push('…')
  return (
    <nav className="pager" aria-label="Pages">
      <button className="btn-sm btn-with-icon" onClick={() => onGo(page - 1)} disabled={page <= 1}>
        <Icon name="chevron-left" size={15} /> Previous
      </button>
      <div className="pager-numbers">
        {numbers.map((n, i) =>
          n === '…' ? (
            <span key={`e${i}`} className="pager-gap">
              …
            </span>
          ) : (
            <button key={n} className={`btn-sm${n === page ? ' active' : ''}`} onClick={() => onGo(n)} aria-current={n === page ? 'page' : undefined}>
              {n}
            </button>
          ),
        )}
      </div>
      <button className="btn-sm btn-with-icon" onClick={() => onGo(page + 1)} disabled={!more}>
        Next <Icon name="chevron-right" size={15} />
      </button>
    </nav>
  )
}

// Everything that matches the filters, in pages of whole rows: the music
// counterpart of the movie and show results. It reads the most listened-to
// albums of all time, so a year or a genre has plenty to choose from.
export function MusicBrowseGrid({ filters, onAdd, onClear, rows = 3 }: { filters: MusicBrowseFilters; onAdd: (t: AddMusicTarget) => void; onClear?: () => void; rows?: number }) {
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid)
  const size = cols * rows
  const [hideOwned] = useHideOwned()
  const [page, setPage] = useState(1)
  const [data, setData] = useState<{ items: MusicDiscoverItem[]; totalPages: number; note: string } | null>(null)
  const [error, setError] = useState('')
  const key = `${filters.type}|${filters.genre}|${filters.yearFrom}|${filters.yearTo}|${filters.sort}|${size}`

  useEffect(() => {
    setPage(1)
  }, [key])

  useEffect(() => {
    let cancelled = false
    setData(null)
    setError('')
    api
      .musicDiscover({
        list: 'popular',
        range: 'all_time',
        type: filters.type,
        genre: filters.genre || undefined,
        yearFrom: filters.yearFrom || undefined,
        yearTo: filters.yearTo || undefined,
        sort: filters.sort === 'popular' ? undefined : filters.sort,
        page,
        pageSize: size,
      })
      .then((p) => !cancelled && setData({ items: p.items ?? [], totalPages: p.totalPages, note: p.note ?? '' }))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, page])

  const items = data ? (hideOwned ? data.items.filter((m) => !m.inLibrary) : data.items) : []
  const empty = data !== null && data.items.length === 0 && page === 1
  function goTo(n: number) {
    setPage(n)
    scrollToTop()
  }

  return (
    <section className="discover-rail paged-grid music-rail">
      <div className="rail-head">
        <h2>{musicBrowseTitle(filters)}</h2>
        {page > 1 && <span className="page-at">Page {page}</span>}
      </div>
      {error && <p className="error-text">{error}</p>}
      {data?.note && <p className="music-note">{data.note}</p>}
      {empty && !data?.note ? (
        <div className="empty-state">
          <Icon name="compass" size={36} />
          <p>Nothing matches these filters.</p>
          {onClear && (
            <button className="btn-with-icon" onClick={onClear}>
              Clear filters
            </button>
          )}
        </div>
      ) : (
        <div className="poster-grid" ref={grid}>
          {data === null && !error ? Array.from({ length: size }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '1 / 1.9' }} />) : <AlbumCards items={items} onAdd={onAdd} />}
        </div>
      )}
      {data && !empty && <Pager page={page} pages={data.totalPages} more={page < data.totalPages} onGo={goTo} />}
    </section>
  )
}
