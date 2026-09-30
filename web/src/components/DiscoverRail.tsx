import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import type { DiscoverMovie } from '../api'
import { comingLabel, fetchPage, sourceToQuery, type Kind, type Source } from '../discoverSources'
import { useGridColumns } from '../useGridColumns'
import { useHideOwned } from '../useHideOwned'
import type { Owned } from '../useOwned'
import type { AddTarget } from './AddDialog'
import Icon from './Icon'
import PosterCard from './PosterCard'

// The posters of one Discover list. Titles you already own say so and open
// their page; the rest offer Add, which opens the usual add dialog.
export function DiscoverCards({ items, kind, owned, onAdd }: { items: DiscoverMovie[]; kind: Kind; owned: Owned; onAdd: (t: AddTarget) => void }) {
  const navigate = useNavigate()
  const map = kind === 'movie' ? owned.movies : owned.shows
  return (
    <>
      {items.map((m) => {
        const own = map.get(m.tmdbId)
        const libraryId = own?.id
        const path = kind === 'movie' ? `/title/${m.tmdbId}` : libraryId ? `/series/${libraryId}` : `/show/${m.tmdbId}`
        const coming = comingLabel(m.releaseDate)
        return (
          <PosterCard
            key={m.tmdbId}
            to={path}
            poster={m.posterUrl}
            title={m.title}
            meta={coming ? <span className="coming-soon">{coming}</span> : m.year || undefined}
            genres={m.genres}
            rating={coming ? undefined : m.rating}
            kind={kind}
            state={own?.state}
            footer={
              <div className="pcard-footer">
                {libraryId ? (
                  <button className="btn-sm btn-with-icon" onClick={() => navigate(path)}>
                    <Icon name="open" size={15} /> Open
                  </button>
                ) : (
                  <button className="primary btn-sm btn-with-icon" onClick={() => onAdd({ kind, tmdbId: m.tmdbId, title: m.title, year: m.year, posterUrl: m.posterUrl, overview: m.overview })}>
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

// Most pages read to fill a row while titles in the library are hidden.
const MAX_PAGES_WHEN_HIDING = 8

interface RailProps {
  title: string
  hint?: string
  kind: Kind
  owned: Owned
  onAdd: (t: AddTarget) => void
  // Either a list to page through, or a fixed set of titles.
  source?: Source
  items?: DiscoverMovie[]
  rows?: number
  // Words for the link to the full list ("Browse all" when left out), and
  // something to put in the heading, such as a switch.
  moreLabel?: string
  headExtra?: ReactNode
}

// A Discover section that always shows whole rows: it measures how many
// posters fit across and loads just enough pages to fill `rows` of them.
// "Load more" adds rows in place; "Browse all" opens the list with pages.
export default function DiscoverRail({ title, hint, kind, owned, onAdd, source, items: fixed, rows = 2, moreLabel = 'Browse all', headExtra }: RailProps) {
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid)
  const [extra, setExtra] = useState(0)
  const want = cols * (rows + extra)
  const [items, setItems] = useState<DiscoverMovie[]>(fixed ?? [])
  const [page, setPage] = useState(0)
  const [totalPages, setTotalPages] = useState(1)
  const [failed, setFailed] = useState(false)
  const busy = useRef(false)
  const [hideOwned] = useHideOwned()
  const ownedMap = kind === 'movie' ? owned.movies : owned.shows
  // With "Hide what I have" on, titles in the library are left out and more
  // pages are read to still fill the rows (but not without end).
  const visible = useMemo(() => (hideOwned ? items.filter((m) => !ownedMap.has(m.tmdbId)) : items), [hideOwned, items, ownedMap])
  const pageLimit = hideOwned ? Math.min(totalPages, MAX_PAGES_WHEN_HIDING) : totalPages

  useEffect(() => {
    if (fixed) setItems(fixed)
  }, [fixed])

  useEffect(() => {
    if (!source || failed || busy.current || visible.length >= want || page >= pageLimit) return
    busy.current = true
    fetchPage(source, page + 1)
      .then((r) => {
        setItems((cur) => {
          const seen = new Set(cur.map((x) => x.tmdbId))
          return [...cur, ...r.results.filter((x) => !seen.has(x.tmdbId))]
        })
        setTotalPages(r.results.length === 0 ? page + 1 : r.totalPages)
        setPage(page + 1)
      })
      .catch(() => setFailed(true))
      .finally(() => {
        busy.current = false
      })
  }, [source, failed, visible.length, want, page, totalPages, pageLimit])

  const loading = !!source && page === 0 && !failed
  if (!loading && visible.length === 0) return null

  // Whole rows only, unless this is everything there is.
  const shown = visible.length >= want ? visible.slice(0, want) : visible.slice(0, Math.max(cols, Math.floor(visible.length / cols) * cols))
  const more = !!source && (page < pageLimit || visible.length > shown.length)
  const fetching = !!source && visible.length < want && page < pageLimit && !failed

  return (
    <section className="discover-rail">
      <div className="rail-head">
        <h2>{title}</h2>
        {hint && <span>{hint}</span>}
        {headExtra && <div className="rail-older">{headExtra}</div>}
        {source && (
          <Link to={`/discover/all?${sourceToQuery({ ...source, older: undefined })}`} className="rail-more">
            {moreLabel} <Icon name="chevron-right" size={15} />
          </Link>
        )}
      </div>
      <div className="poster-grid" ref={grid}>
        {loading ? Array.from({ length: want }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />) : <DiscoverCards items={shown} kind={kind} owned={owned} onAdd={onAdd} />}
        {!loading && fetching && Array.from({ length: Math.max(0, want - shown.length) }, (_, i) => <div key={`s${i}`} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />)}
      </div>
      {more && (
        <div className="rail-foot">
          <button className="btn-sm btn-with-icon" onClick={() => setExtra((x) => x + 2)} disabled={fetching}>
            <Icon name="plus" size={15} /> Load more
          </button>
        </div>
      )}
    </section>
  )
}
