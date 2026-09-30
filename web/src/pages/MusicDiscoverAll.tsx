import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, type MusicDiscoverArtist, type MusicDiscoverItem, type MusicRange, type MusicType } from '../api'
import AddMusicDialog, { type AddMusicTarget } from '../components/AddMusicDialog'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import { AlbumCards, ArtistCards, MUSIC_RANGES, MusicFilters, musicListTitle, useKnownGenres, type MusicListId } from '../components/MusicRails'
import { scrollToTop } from '../scrollTop'

const LISTS: MusicListId[] = ['popular', 'new', 'upcoming', 'artists']
const RANGES: MusicRange[] = ['week', 'month', 'year', 'all_time']
const TYPES: MusicType[] = ['all', 'album', 'ep', 'single']
const PAGE_SIZE = 24

// One music list on its own page: filters on top, a grid of cards and pages
// to step through. The filters live in the address, so a page can be shared.
export default function MusicDiscoverAll() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const list = LISTS.find((l) => l === params.get('list')) ?? 'popular'
  const range = RANGES.find((r) => r === params.get('range')) ?? 'week'
  const type = TYPES.find((t) => t === params.get('type')) ?? 'all'
  const genre = params.get('genre') ?? ''
  const artists = list === 'artists'
  const ranged = list === 'popular' || artists

  const [page, setPage] = useState(1)
  const [albums, setAlbums] = useState<MusicDiscoverItem[]>([])
  const [people, setPeople] = useState<MusicDiscoverArtist[]>([])
  const [totalPages, setTotalPages] = useState<number | undefined>()
  const [note, setNote] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<AddMusicTarget | null>(null)
  const [genres, addGenres] = useKnownGenres()

  useEffect(() => {
    if (genre) addGenres([genre])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function change(next: Record<string, string>) {
    const q = new URLSearchParams(params)
    for (const [k, v] of Object.entries(next)) {
      if (v && !(k === 'type' && v === 'all')) q.set(k, v)
      else q.delete(k)
    }
    setPage(1)
    setParams(q, { replace: true })
  }

  const key = `${list}|${range}|${type}|${genre}|${page}`
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    const done = (n: string, total?: number) => {
      if (cancelled) return
      setNote(n)
      setTotalPages(total)
      setLoading(false)
    }
    const failed = (e: unknown) => {
      if (cancelled) return
      setError(e instanceof Error ? e.message : String(e))
      setAlbums([])
      setPeople([])
      setLoading(false)
    }
    if (artists) {
      api
        .musicDiscoverArtists({ range, page })
        .then((r) => {
          if (cancelled) return
          setPeople(r.items ?? [])
          done(r.note ?? '', r.totalPages)
        })
        .catch(failed)
    } else {
      api
        .musicDiscover({ list: list as 'popular' | 'new' | 'upcoming', type, range: list === 'popular' ? range : undefined, genre: genre || undefined, page, pageSize: PAGE_SIZE })
        .then((r) => {
          if (cancelled) return
          const items = r.items ?? []
          setAlbums(items)
          addGenres(items.flatMap((i) => i.genres ?? []))
          done(r.note ?? '', r.totalPages)
        })
        .catch(failed)
    }
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  const empty = !loading && (artists ? people.length === 0 : albums.length === 0)
  // Without a page count (artists) Next stays open while the page has anything.
  const hasMore = totalPages !== undefined ? page < totalPages : !empty
  const pageCount = totalPages ?? page + (hasMore ? 1 : 0)

  function goTo(n: number) {
    setPage(n)
    scrollToTop()
  }

  const numbers = useMemo(() => {
    const out: (number | '…')[] = []
    const from = Math.max(1, page - 2)
    const to = Math.min(pageCount, page + 2)
    if (from > 1) out.push(1, ...(from > 2 ? (['…'] as const) : []))
    for (let n = from; n <= to; n++) out.push(n)
    if (to < pageCount) out.push('…')
    return out
  }, [page, pageCount])

  return (
    <div>
      <div className="toolbar filter-bar">
        <Link to="/discover?kind=music" className="btn btn-with-icon">
          <Icon name="chevron-left" size={16} /> Discover
        </Link>
        <h2 style={{ margin: 0 }}>{musicListTitle(list, range)}</h2>
        <span className="spacer" />
        {ranged && <Dropdown label="Time" value={range} onChange={(v) => change({ range: v })} options={MUSIC_RANGES.map((r) => ({ value: r.value, label: r.label }))} />}
        {!artists && <MusicFilters type={type} onType={(t) => change({ type: t })} genre={genre} onGenre={(g) => change({ genre: g })} genres={genres} />}
      </div>

      <section className="discover-rail paged-grid">
        {error && <p className="error-text">{error}</p>}
        {note && <p className="music-note">{note}</p>}
        {empty && !error && !note && (
          <div className="empty-state">
            <Icon name="compass" size={36} />
            <p>{page > 1 ? "That's the end of the list." : 'Nothing matches these filters.'}</p>
          </div>
        )}
        {artists ? (
          <div className="artist-results">
            {loading ? Array.from({ length: 12 }, (_, i) => <div key={i} className="skeleton" style={{ height: 130 }} />) : <ArtistCards items={people} onAdd={setAdding} />}
          </div>
        ) : (
          <div className="poster-grid">
            {loading ? Array.from({ length: PAGE_SIZE }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '1 / 1.9' }} />) : <AlbumCards items={albums} onAdd={setAdding} />}
          </div>
        )}

        <nav className="pager" aria-label="Pages">
          <button className="btn-sm btn-with-icon" onClick={() => goTo(page - 1)} disabled={page <= 1}>
            <Icon name="chevron-left" size={15} /> Previous
          </button>
          <div className="pager-numbers">
            {numbers.map((n, i) =>
              n === '…' ? (
                <span key={`e${i}`} className="pager-gap">
                  …
                </span>
              ) : (
                <button key={n} className={`btn-sm${n === page ? ' active' : ''}`} onClick={() => goTo(n)} aria-current={n === page ? 'page' : undefined}>
                  {n}
                </button>
              ),
            )}
          </div>
          <button className="btn-sm btn-with-icon" onClick={() => goTo(page + 1)} disabled={!hasMore}>
            Next <Icon name="chevron-right" size={15} />
          </button>
        </nav>
      </section>

      {adding && (
        <AddMusicDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(artistId, albumId) => {
            setAdding(null)
            navigate(`/music/artist/${artistId}${albumId ? `#album-${albumId}` : ''}`)
          }}
        />
      )}
    </div>
  )
}
