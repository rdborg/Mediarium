import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Book, type BookFormat, type BookProgress } from '../api'
import BrandMark from '../components/BrandMark'
import Icon from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { useDocumentTitle } from '../documentTitle'
import { listenPath, readPath } from './open'

type Tab = 'all' | BookFormat

// The shelf: what you were reading or listening to last, then every book
// that is downloaded, with Read and Listen.
export default function Shelf() {
  useDocumentTitle('Mediarium Books')
  const [books, setBooks] = useState<Book[] | null>(null)
  const [progress, setProgress] = useState<BookProgress[]>([])
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('all')
  const [q, setQ] = useState('')

  useEffect(() => {
    api.listBooks().then(setBooks).catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.listBookProgress().then(setProgress).catch(() => undefined)
  }, [])

  const byId = useMemo(() => new Map((books ?? []).map((b) => [b.id, b])), [books])
  const progressOf = (id: number, f: BookFormat) => progress.find((p) => p.bookId === id && p.format === f)
  const continuing = progress.filter((p) => !p.finished && byId.get(p.bookId)?.[p.format].status === 'downloaded' && (tab === 'all' || tab === p.format)).slice(0, 6)
  const needle = q.trim().toLowerCase()
  const shelf = (books ?? []).filter(
    (b) =>
      (tab === 'all' ? b.ebook.status === 'downloaded' || b.audiobook.status === 'downloaded' : b[tab].status === 'downloaded') &&
      (!needle || b.title.toLowerCase().includes(needle) || b.author.toLowerCase().includes(needle)),
  )
  shelf.sort((a, b) => a.title.localeCompare(b.title))

  return (
    <div className="bks-shelf">
      <header className="bks-top">
        <span className="bks-brand">
          <BrandMark className="" /> <strong>Mediarium</strong> Books
        </span>
        <div className="seg">
          {(['all', 'ebook', 'audiobook'] as Tab[]).map((t) => (
            <button key={t} className={tab === t ? 'active' : ''} onClick={() => setTab(t)}>
              {t === 'all' ? 'All' : t === 'ebook' ? 'Reading' : 'Listening'}
            </button>
          ))}
        </div>
        <input type="search" placeholder="Find a book" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Find a book" />
        <a className="bks-back" href="/library?kind=ebook" target="_blank" rel="noreferrer" title="Open Mediarium to add more books">
          <Icon name="plus" size={15} /> Get more books
        </a>
      </header>

      {error && <p className="error-text">{error}</p>}

      {continuing.length > 0 && !needle && (
        <section className="bks-section">
          <h2>Continue</h2>
          <div className="bks-continue">
            {continuing.map((p) => {
              const b = byId.get(p.bookId)!
              return (
                <Link key={`${p.bookId}-${p.format}`} className="bks-cont" to={p.format === 'ebook' ? readPath(b.id) : listenPath(b.id)}>
                  <span className="bks-cont-cover">{b.coverUrl ? <img src={b.coverUrl} alt="" /> : <PosterFallback />}</span>
                  <span className="bks-cont-text">
                    <strong>{b.title}</strong>
                    <small>
                      <Icon name={p.format === 'ebook' ? 'book' : 'headphones'} size={12} /> {p.format === 'ebook' ? 'Reading' : 'Listening'} · {Math.round(p.percent)}%
                    </small>
                    <span className="bks-bar">
                      <span style={{ width: `${Math.min(100, p.percent)}%` }} />
                    </span>
                  </span>
                </Link>
              )
            })}
          </div>
        </section>
      )}

      <section className="bks-section">
        <h2>{tab === 'ebook' ? 'Ebooks' : tab === 'audiobook' ? 'Audiobooks' : 'Your books'}</h2>
        {books === null ? (
          <div className="bks-grid">
            {Array.from({ length: 8 }, (_, i) => (
              <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />
            ))}
          </div>
        ) : shelf.length === 0 ? (
          <div className="empty-state">
            <Icon name="book" size={44} />
            <p>{needle ? 'No book matches that.' : 'No books downloaded yet. Add some in Mediarium and they appear here once they are downloaded.'}</p>
          </div>
        ) : (
          <div className="bks-grid">
            {shelf.map((b) => {
              const read = b.ebook.status === 'downloaded' && tab !== 'audiobook'
              const listen = b.audiobook.status === 'downloaded' && tab !== 'ebook'
              const pe = progressOf(b.id, 'ebook')
              const pa = progressOf(b.id, 'audiobook')
              const pct = Math.max(read ? (pe?.percent ?? 0) : 0, listen ? (pa?.percent ?? 0) : 0)
              return (
                <div key={b.id} className="bks-book">
                  <Link className="bks-cover" to={read ? readPath(b.id) : listenPath(b.id)} title={b.title}>
                    {b.coverUrl ? <img src={b.coverUrl} alt="" loading="lazy" /> : <PosterFallback />}
                    {pct > 0 && (
                      <span className="bks-bar on-cover">
                        <span style={{ width: `${Math.min(100, pct)}%` }} />
                      </span>
                    )}
                  </Link>
                  <strong className="bks-title">{b.title}</strong>
                  <small className="bks-author">{b.author}</small>
                  <div className="bks-actions">
                    {read && (
                      <Link className="btn-sm primary btn-with-icon" to={readPath(b.id)}>
                        <Icon name="book" size={14} /> Read
                      </Link>
                    )}
                    {listen && (
                      <Link className="btn-sm btn-with-icon" to={listenPath(b.id)}>
                        <Icon name="headphones" size={14} /> Listen
                      </Link>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>
    </div>
  )
}
