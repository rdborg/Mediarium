import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, type BookFormat, type BookFound } from '../api'
import { useModules } from '../ModulesContext'
import { useFocusTrap } from '../useFocusTrap'
import { useGridColumns } from '../useGridColumns'
import { useHideOwned } from '../useHideOwned'
import Icon from './Icon'
import PosterCard, { PosterFallback, type CardRibbon } from './PosterCard'
import { describeState } from './state'
import { useToast } from './Toast'

// The books part of Discover: what people are reading on Open Library, a few
// popular genres, or (with a filter picked) one list of everything matching.

export interface BookFilters {
  subject: string
  subjectName?: string
  from: string
  to: string
  sort: string
}

type Loader = (page: number) => Promise<BookFound[]>

const browse = (f: { subject?: string; from?: string; to?: string; sort?: string }): Loader => (page) =>
  api.bookDiscover({ list: 'browse', subject: f.subject || undefined, from: f.from || undefined, to: f.to || undefined, sort: f.sort || 'popular', page })
const trending = (period: string): Loader => (page) => api.bookDiscover({ list: 'trending', period, page })

const RAILS: { id: string; title: string; hint: string; load: Loader }[] = [
  { id: 'week', title: 'Trending this week', hint: 'what people are reading and listening to', load: trending('weekly') },
  { id: 'year', title: 'Popular this year', hint: 'the most read in the last twelve months', load: trending('yearly') },
  { id: 'classics', title: 'Classics', hint: 'the most read classics', load: browse({ subject: 'classic literature' }) },
  { id: 'fantasy', title: 'Fantasy', hint: 'most read first', load: browse({ subject: 'fantasy' }) },
  { id: 'scifi', title: 'Science fiction', hint: 'most read first', load: browse({ subject: 'science fiction' }) },
  { id: 'mystery', title: 'Mystery and thrillers', hint: 'most read first', load: browse({ subject: 'mystery' }) },
  { id: 'bio', title: 'Biography and memoir', hint: 'most read first', load: browse({ subject: 'biography' }) },
  { id: 'selfhelp', title: 'Self-help', hint: 'most read first', load: browse({ subject: 'self-help' }) },
]

export function BookBody({ filters, filtering, onAdd, onClear }: { filters: BookFilters; filtering: boolean; onAdd: (b: BookFound) => void; onClear: () => void }) {
  const rails = RAILS
  const key = `${filters.subject}|${filters.from}|${filters.to}|${filters.sort}`
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const load = useMemo(() => browse(filters), [key])
  // Open Library puts the most read books under many genres at once, so each
  // row leaves out what a row above it already shows.
  const [shownBy, setShownBy] = useState<Record<string, string>>({})
  const report = useCallback((id: string, keys: string) => setShownBy((cur) => (cur[id] === keys ? cur : { ...cur, [id]: keys })), [])
  if (filtering) {
    const parts = [filters.subjectName || 'Books', filters.from || filters.to ? `${filters.from || '…'}–${filters.to || 'now'}` : ''].filter(Boolean)
    return <BookRail key={key} title={parts.join(', ')} hint="" load={load} onAdd={onAdd} rows={3} onClear={onClear} />
  }
  return (
    <>
      {rails.map((r, i) => (
        <BookRail
          key={r.id}
          title={r.title}
          hint={r.hint}
          load={r.load}
          onAdd={onAdd}
          rows={r.id === 'week' ? 2 : 1}
          skip={rails.slice(0, i).map((above) => shownBy[above.id] ?? '').join('|')}
          onShown={(keys) => report(r.id, keys)}
        />
      ))}
    </>
  )
}

// One rail of book covers: whole rows only, with "Load more" for the next rows.
function BookRail({
  title,
  hint,
  load,
  onAdd,
  rows,
  onClear,
  skip = '',
  onShown,
}: {
  title: string
  hint: string
  load: Loader
  onAdd: (b: BookFound) => void
  rows: number
  onClear?: () => void
  skip?: string // keys shown by rows above, joined with |
  onShown?: (keys: string) => void
}) {
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid)
  const [hideOwned] = useHideOwned()
  const [extra, setExtra] = useState(0)
  const [items, setItems] = useState<BookFound[]>([])
  const [page, setPage] = useState(0)
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [owned, setOwned] = useState<Record<string, number>>({}) // added while on the page

  const skipped = useMemo(() => new Set(skip.split('|').filter(Boolean)), [skip])
  const shown = items.map((b) => (owned[b.key] ? { ...b, libraryId: owned[b.key] } : b)).filter((b) => (!hideOwned || !b.libraryId) && !skipped.has(b.key))
  const want = cols * (rows + extra)

  useEffect(() => {
    if (loading || done || error || shown.length >= want) return
    setLoading(true)
    load(page + 1)
      .then((more) => {
        setItems((cur) => {
          const seen = new Set(cur.map((b) => b.key))
          return [...cur, ...more.filter((b) => !seen.has(b.key))]
        })
        setPage((p) => p + 1)
        if (more.length === 0) setDone(true)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }, [loading, done, error, shown.length, want, page, load])

  useEffect(() => {
    const onAdded = (e: Event) => {
      const d = (e as CustomEvent<{ key: string; id: number }>).detail
      setOwned((o) => ({ ...o, [d.key]: d.id }))
    }
    window.addEventListener('mediarium:book-added', onAdded)
    return () => window.removeEventListener('mediarium:book-added', onAdded)
  }, [])

  const visible = shown.length >= want || done ? shown.slice(0, want) : shown.slice(0, Math.max(0, Math.floor(shown.length / cols) * cols))
  const visibleKeys = visible.map((b) => b.key).join('|')
  useEffect(() => onShown?.(visibleKeys), [visibleKeys, onShown])
  // On the overview a list Open Library can't give right now is left out.
  if (error && visible.length === 0 && !onClear) return null
  if (done && shown.length === 0 && !error) {
    return (
      <section className="discover-rail">
        <div className="rail-head">
          <h2>{title}</h2>
        </div>
        <p className="hint">
          No books found.{' '}
          {onClear && (
            <button className="btn-sm" onClick={onClear}>
              Clear filters
            </button>
          )}
        </p>
      </section>
    )
  }
  return (
    <section className="discover-rail book-rail">
      <div className="rail-head">
        <h2>{title}</h2>
        {hint && <span>{hint}</span>}
      </div>
      {error && <p className="error-text">{error}</p>}
      <div className="poster-grid" ref={grid}>
        {visible.map((b) => (
          <BookCard key={b.key} book={b} onAdd={onAdd} />
        ))}
        {visible.length === 0 && !error && Array.from({ length: cols * rows }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />)}
      </div>
      {!error && !(done && shown.length <= want) && visible.length > 0 && (
        <div className="rail-foot">
          <button className="btn-sm btn-with-icon" onClick={() => setExtra((x) => x + 2)} disabled={loading}>
            <Icon name="plus" size={15} /> Load more
          </button>
        </div>
      )}
    </section>
  )
}

// The banner on a cover: which editions of the book exist.
export function editionRibbon(b: { hasEbook?: boolean; hasAudio?: boolean }): CardRibbon | undefined {
  if (b.hasEbook && b.hasAudio) return { label: 'eBook + Audio', tone: 'both' }
  if (b.hasEbook) return { label: 'eBook', tone: 'ebook' }
  if (b.hasAudio) return { label: 'Audiobook', tone: 'audio' }
  return undefined
}

export function BookCard({ book, onAdd }: { book: BookFound; onAdd: (b: BookFound) => void }) {
  const navigate = useNavigate()
  const inLibrary = !!book.libraryId
  return (
    <PosterCard
      to={inLibrary ? `/book/${book.libraryId}` : `/books/work/${book.key}`}
      ribbon={editionRibbon(book)}
      poster={book.coverUrl}
      title={book.title}
      meta={[book.author, book.year].filter(Boolean).join(' · ')}
      state={inLibrary ? { ...describeState('downloaded'), label: 'In library' } : undefined}
      actions={
        inLibrary
          ? [{ icon: 'open', label: 'Open', onClick: () => navigate(`/book/${book.libraryId}`) }]
          : [{ icon: 'plus', label: 'Add', onClick: () => onAdd(book), primary: true }]
      }
    />
  )
}

// Adding a book from Discover: pick ebook, audiobook or both (only the formats
// that are switched on), and Mediarium starts looking straight away.
export function BookAddDialog({ target, onClose, prefer }: { target: BookFound | null; onClose: () => void; prefer?: BookFormat }) {
  if (!target) return null
  return <AddDialog target={target} onClose={onClose} prefer={prefer} />
}

function AddDialog({ target, onClose, prefer }: { target: BookFound; onClose: () => void; prefer?: BookFormat }) {
  const toast = useToast()
  const navigate = useNavigate()
  const { on } = useModules()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const formats = (['ebook', 'audiobook'] as BookFormat[]).filter((f) => on(f === 'ebook' ? 'ebooks' : 'audiobooks'))
  const [chosen, setChosen] = useState<Set<BookFormat>>(() => {
    if (prefer && formats.includes(prefer)) return new Set([prefer])
    // Start with the formats that have been published, of those switched on.
    const exist = formats.filter((f) => (f === 'ebook' ? target.hasEbook : target.hasAudio))
    return new Set(exist.length > 0 ? exist : formats.slice(0, 1))
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  function toggle(f: BookFormat) {
    setChosen((cur) => {
      const next = new Set(cur)
      if (next.has(f)) next.delete(f)
      else next.add(f)
      return next
    })
  }

  async function submit() {
    if (chosen.size === 0) {
      setError('Choose ebook, audiobook or both.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const b = await api.addBook({
        olKey: target.key,
        title: target.title,
        author: target.author,
        authorKey: target.authorKey,
        year: target.year,
        coverId: target.coverId,
        ebook: chosen.has('ebook'),
        audiobook: chosen.has('audiobook'),
        searchNow: true,
      })
      window.dispatchEvent(new CustomEvent('mediarium:book-added', { detail: { key: target.key, id: b.id } }))
      toast.success(`${b.title} added. Mediarium is looking for it now.`)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  const blurb: Record<BookFormat, string> = { ebook: 'EPUB first, then AZW3, MOBI or PDF.', audiobook: 'M4B first, then MP3 and the rest.' }
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={`Add ${target.title}`}>
        <div className="modal-head">
          <div className="modal-cover">{target.coverUrl ? <img src={target.coverUrl} alt="" /> : <PosterFallback />}</div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>Add {target.title}</h2>
            <p className="hint" style={{ margin: 0 }}>{[target.author, target.year].filter(Boolean).join(' · ')}</p>
          </div>
        </div>
        <div className="modal-body grid-form">
          {formats.length === 0 ? (
            <p>
              Ebooks and audiobooks are both switched off.{' '}
              <button className="btn-sm" onClick={() => navigate('/settings/modules')}>
                Open Media types
              </button>
            </p>
          ) : (
            <div className="choice-grid" role="group" aria-label="Formats">
              {formats.map((f) => (
                <button key={f} type="button" role="checkbox" aria-checked={chosen.has(f)} className={`choice-card${chosen.has(f) ? ' active' : ''}`} onClick={() => toggle(f)} disabled={busy}>
                  <span className="choice-box" aria-hidden="true">
                    {chosen.has(f) && <Icon name="check" size={13} />}
                  </span>
                  <strong>
                    <Icon name={f === 'ebook' ? 'book' : 'headphones'} size={15} /> {f === 'ebook' ? 'Ebook' : 'Audiobook'}
                  </strong>
                  <small>{blurb[f]}</small>
                </button>
              ))}
            </div>
          )}
          {error && <p className="error-text">{error}</p>}
        </div>
        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="primary btn-with-icon" onClick={() => void submit()} disabled={busy || formats.length === 0}>
            <Icon name="plus" size={16} /> {busy ? 'Adding…' : 'Add book'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
