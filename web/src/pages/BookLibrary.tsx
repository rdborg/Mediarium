import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, isRequested, type Book, type BookFormat, type BookFound, type QueueItem } from '../api'
import BookImport from '../components/BookImport'
import { listenPath, openBookApp, readPath } from '../bookshelf/open'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import PosterCard, { PosterFallback } from '../components/PosterCard'
import { describeState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { useModules } from '../ModulesContext'

type Sort = 'added' | 'title' | 'author'

export const FORMAT_WORD: Record<BookFormat, string> = { ebook: 'ebook', audiobook: 'audiobook' }
export const FORMAT_PLURAL: Record<BookFormat, string> = { ebook: 'ebooks', audiobook: 'audiobooks' }

// Where one format of a book stands, with the queue's word on it when a
// download for it is running or waiting.
export function bookState(b: Book, f: BookFormat, queue: QueueItem[] = []): ItemState {
  const q = queue.filter((x) => x.bookId === b.id && x.bookFormat === f)
  const running = q.find((x) => x.status === 'downloading' || x.status === 'importing')
  if (running) return describeState('downloading', `${Math.round(running.progressPct)}%`)
  if (q.some((x) => x.status === 'queued')) return describeState('pending')
  if (q.some((x) => x.status === 'paused')) return describeState('paused')
  const st = b[f].status
  if (st === 'downloaded') return describeState('downloaded')
  if (st === 'downloading') return describeState('downloading')
  if (!b[f].wanted) return describeState('added')
  return describeState('searching')
}

const FILTERS: { id: string; label: string; test: (s: ItemState) => boolean }[] = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'downloaded', label: 'Downloaded', test: (s) => s.key === 'downloaded' },
  { id: 'downloading', label: 'Downloading', test: (s) => s.key === 'downloading' || s.key === 'pending' || s.key === 'paused' },
  { id: 'missing', label: 'Waiting for a release', test: (s) => s.key === 'searching' },
]

// The Ebooks or Audiobooks tab of the library: the books wanted in that
// format, and a search to add more.
export default function BookLibrary({
  format,
  books,
  queue,
  error,
  switcher,
  reload,
  admin,
}: {
  format: BookFormat
  books: Book[] | null
  queue: QueueItem[]
  error: string
  switcher: ReactNode
  reload: () => void
  admin: boolean
}) {
  const [text, setText] = useState('')
  const [filter, setFilter] = useState('all')
  const [sort, setSort] = useState<Sort>('added')
  const [adding, setAdding] = useState(false)
  const [importing, setImporting] = useState(false)
  const [folder, setFolder] = useState('')
  useEffect(() => {
    if (admin && importing) api.getSettings().then((s) => setFolder((format === 'ebook' ? s.ebooksPath : s.audiobooksPath) ?? '')).catch(() => undefined)
  }, [admin, importing, format])

  const mine = useMemo(() => (books ?? []).filter((b) => b[format].wanted || b[format].status === 'downloaded'), [books, format])
  const shown = useMemo(() => {
    if (!books) return null
    const test = FILTERS.find((f) => f.id === filter)?.test ?? (() => true)
    const needle = text.trim().toLowerCase()
    const list = mine.filter((b) => test(bookState(b, format, queue)) && (!needle || b.title.toLowerCase().includes(needle) || b.author.toLowerCase().includes(needle)))
    if (sort === 'title') list.sort((a, b) => a.title.localeCompare(b.title))
    else if (sort === 'author') list.sort((a, b) => a.author.localeCompare(b.author) || a.title.localeCompare(b.title))
    else list.sort((a, b) => b.addedAt.localeCompare(a.addedAt))
    return list
  }, [books, mine, filter, text, sort, format, queue])

  const word = FORMAT_PLURAL[format]
  return (
    <div>
      <div className="toolbar">
        {switcher}
        <input type="search" style={{ flex: "1 1 180px", minWidth: 0, maxWidth: 300 }} placeholder={`Find in your ${word}…`} value={text} onChange={(e) => setText(e.target.value)} aria-label={`Find in your ${word}`} />
        <Dropdown
          label="Sort"
          value={sort}
          onChange={(v) => setSort(v as Sort)}
          options={[
            { value: 'added', label: 'Recently added' },
            { value: 'title', label: 'Title' },
            { value: 'author', label: 'Author' },
          ]}
        />
        <div className="toolbar-end">
          {admin && (
            <button className="btn-with-icon" onClick={() => setImporting((v) => !v)} aria-expanded={importing}>
              <Icon name={importing ? 'x' : 'folder'} size={16} /> {importing ? 'Close' : 'Import'}
            </button>
          )}
          <button className="primary btn-with-icon" onClick={() => setAdding((a) => !a)} aria-expanded={adding}>
            <Icon name={adding ? 'x' : 'plus'} size={16} /> {adding ? 'Close' : 'Add'}
          </button>
        </div>
      </div>

      {importing && admin && <BookImport format={format} folder={folder} onDone={reload} />}
      {adding && <AddBook format={format} onAdded={reload} />}

      <div className="chip-row" style={{ marginBottom: 18, alignItems: 'center' }}>
        {FILTERS.map((f) => (
          <button key={f.id} className={`chip${filter === f.id ? ' active' : ''}`} onClick={() => setFilter(f.id)}>
            {f.label} {books ? <small>{mine.filter((b) => f.test(bookState(b, format, queue))).length}</small> : null}
          </button>
        ))}
      </div>

      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid">
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="empty-state">
          <Icon name={format === 'ebook' ? 'book' : 'headphones'} size={44} />
          {mine.length === 0 ? (
            <>
              <p>No {word} yet.</p>
              {!adding && (
                <button className="primary btn-with-icon" onClick={() => setAdding(true)}>
                  <Icon name="search" size={16} /> Find a book to add
                </button>
              )}
            </>
          ) : (
            <>
              <p>Nothing matches these filters.</p>
              <button
                className="btn-with-icon"
                onClick={() => {
                  setText('')
                  setFilter('all')
                }}
              >
                Clear filters
              </button>
            </>
          )}
        </div>
      ) : (
        <div className="poster-grid">
          {shown.map((b) => {
            const st = b[format]
            return (
              <PosterCard
                key={b.id}
                to={`/book/${b.id}`}
                poster={b.coverUrl}
                title={b.title}
                meta={[b.author || null, b.year ? String(b.year) : null, st.format ? st.format.toUpperCase() : null].filter(Boolean).join(' · ')}
                state={bookState(b, format, queue)}
                dim={!st.wanted && st.status !== 'downloaded'}
                actions={
                  st.status === 'downloaded'
                    ? [
                        format === 'ebook'
                          ? { icon: 'book', label: 'Read', onClick: () => openBookApp(readPath(b.id)) }
                          : { icon: 'play', label: 'Listen', onClick: () => openBookApp(listenPath(b.id)) },
                      ]
                    : undefined
                }
              />
            )
          })}
        </div>
      )}
    </div>
  )
}

// The search that adds a book: Open Library by title or author, then Add.
function AddBook({ format, onAdded }: { format: BookFormat; onAdded: () => void }) {
  const toast = useToast()
  const { on } = useModules()
  const other: BookFormat = format === 'ebook' ? 'audiobook' : 'ebook'
  const otherOn = on(other === 'ebook' ? 'ebooks' : 'audiobooks')
  const [q, setQ] = useState('')
  const [results, setResults] = useState<BookFound[] | null>(null)
  const [error, setError] = useState('')
  const [both, setBoth] = useState(false)
  const [busy, setBusy] = useState('')

  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setResults(null)
      return
    }
    setError('')
    const t = setTimeout(() => {
      api
        .searchBooks(term)
        .then(setResults)
        .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    }, 350)
    return () => clearTimeout(t)
  }, [q])

  async function add(f: BookFound) {
    setBusy(f.key)
    try {
      const wantOther = both && otherOn
      const b = await api.addBook({
        olKey: f.key,
        title: f.title,
        author: f.author,
        authorKey: f.authorKey,
        year: f.year,
        coverId: f.coverId,
        ebook: format === 'ebook' || wantOther,
        audiobook: format === 'audiobook' || wantOther,
        searchNow: true,
      })
      if (isRequested(b)) {
        toast.success(b.message)
        return
      }
      toast.success(`${b.title} added. Mediarium is looking for it now.`)
      setResults((r) => r?.map((x) => (x.key === f.key ? { ...x, libraryId: b.id } : x)) ?? r)
      onAdded()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  return (
    <section className="card book-add">
      <div className="book-add-head">
        <input type="search" autoFocus placeholder="Title or author, for example The Hobbit" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Find a book" />
        {otherOn && (
          <label className="check-inline">
            <input type="checkbox" checked={both} onChange={(e) => setBoth(e.target.checked)} /> Get the {FORMAT_WORD[other]} too
          </label>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      {results !== null && results.length === 0 && <p className="hint">No books found. Try the author's name, or fewer words.</p>}
      {results && results.length > 0 && (
        <div className="book-results">
          {results.map((f) => (
            <div key={f.key} className="book-result">
              <span className="book-thumb">{f.coverUrl ? <img src={f.coverUrl.replace('-M.jpg', '-S.jpg')} alt="" loading="lazy" /> : <PosterFallback />}</span>
              <span className="book-result-text">
                <strong>{f.title}</strong>
                <small>{[f.author, f.year].filter(Boolean).join(' · ')}</small>
              </span>
              {f.libraryId ? (
                <Link className="btn-sm" to={`/book/${f.libraryId}`}>
                  In your library
                </Link>
              ) : (
                <button className="btn-sm primary" disabled={busy !== ''} onClick={() => void add(f)}>
                  {busy === f.key ? 'Adding…' : 'Add'}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
      <p className="hint">Book details and covers come from Open Library.</p>
    </section>
  )
}
