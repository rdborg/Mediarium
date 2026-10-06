import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type BookFormat, type BookWork as Work } from '../api'
import AuthorBooks from '../components/AuthorBooks'
import BookSeries from '../components/BookSeries'
import { BookAddDialog } from '../components/BookDiscover'
import Icon from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { useDocumentTitle } from '../documentTitle'
import { useModules } from '../ModulesContext'

// A book's info page before it is in the library (like a movie's or show's
// page from Discover): cover, description, subjects, which editions exist,
// Add, and more by the same author. A book already in the library opens its
// own page instead.
export default function BookWork() {
  const { key = '' } = useParams()
  const { on } = useModules()
  const [work, setWork] = useState<Work | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<{ prefer?: BookFormat } | null>(null)
  const [libraryId, setLibraryId] = useState<number | undefined>()
  useDocumentTitle(work?.title)

  useEffect(() => {
    setWork(null)
    setError('')
    api
      .bookWork(key)
      .then((w) => {
        setWork(w)
        setLibraryId(w.libraryId)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [key])

  useEffect(() => {
    const onAdded = (e: Event) => {
      const d = (e as CustomEvent<{ key: string; id: number }>).detail
      if (d.key === key) setLibraryId(d.id)
    }
    window.addEventListener('mediarium:book-added', onAdded)
    return () => window.removeEventListener('mediarium:book-added', onAdded)
  }, [key])

  if (error) return <p className="error-text">{error}</p>
  if (!work) return <div className="skeleton" style={{ height: 320, borderRadius: 22 }} />

  const formats = (['ebook', 'audiobook'] as BookFormat[]).filter((f) => on(f === 'ebook' ? 'ebooks' : 'audiobooks'))
  return (
    <div>
      <section className="title-hero kind-book">
        <div className="title-poster">{work.coverUrl ? <img src={work.coverUrl} alt={work.title} /> : <PosterFallback />}</div>
        <div className="title-info">
          <div className="title-kicker">
            <span className="kind-tag kind-book">
              <Icon name="book" size={13} /> Book
            </span>
            {libraryId ? (
              <span className="state-tag st-downloaded">
                <Icon name="check" size={13} /> In your library
              </span>
            ) : null}
          </div>
          <h1>{work.title}</h1>
          <p className="title-tagline">{[work.author, work.year].filter(Boolean).join(' · ')}</p>
          <div className="title-chips">
            <span className={`chip-plain edition ${work.hasEbook ? 'yes' : 'no'}`} title="Whether an ebook edition has been published">
              <Icon name="book" size={13} /> {work.hasEbook ? 'eBook editions exist' : 'No ebook edition listed'}
            </span>
            <span className={`chip-plain edition ${work.hasAudio ? 'yes' : 'no'}`} title="Whether an audiobook edition has been published">
              <Icon name="headphones" size={13} /> {work.hasAudio ? 'Audiobook editions exist' : 'No audiobook edition listed'}
            </span>
            {work.subjects.slice(0, 6).map((s) => (
              <span key={s} className="chip-genre">
                {s}
              </span>
            ))}
          </div>
          {work.description && <p className="title-overview">{work.description}</p>}
          <div className="title-links">
            <a className="link-pill" href={`https://openlibrary.org/works/${work.key}`} target="_blank" rel="noreferrer">
              <Icon name="external" size={13} /> More about this book
            </a>
          </div>
          <div className="title-actions">
            {libraryId ? (
              <Link className="primary btn-with-icon" to={`/book/${libraryId}`}>
                <Icon name="open" size={16} /> Open in your library
              </Link>
            ) : formats.length === 0 ? (
              <p className="hint">Ebooks and audiobooks are switched off.</p>
            ) : (
              <>
                {formats.map((f, i) => (
                  <button key={f} className={`${i === 0 ? 'primary ' : ''}btn-with-icon big`} onClick={() => setAdding({ prefer: f })}>
                    <Icon name={f === 'ebook' ? 'book' : 'headphones'} size={18} /> Add as {f === 'ebook' ? 'eBook' : 'audiobook'}
                  </button>
                ))}
                {formats.length === 2 && (
                  <button className="btn-with-icon" onClick={() => setAdding({})}>
                    <Icon name="plus" size={16} /> Both
                  </button>
                )}
              </>
            )}
          </div>
          <p className="hint" style={{ marginTop: 10 }}>
            Book details come from Open Library. &quot;Editions exist&quot; means one has been published; whether a release turns up depends on your indexers.
          </p>
        </div>
      </section>

      <BookSeries workKey={work.key} title={work.title} author={work.author} />
      {work.authorKey && <AuthorBooks authorKey={work.authorKey} author={work.author} exclude={work.key} />}

      <BookAddDialog
        target={adding ? { ...work, hasEbook: adding.prefer ? adding.prefer === 'ebook' : true, hasAudio: adding.prefer ? adding.prefer === 'audiobook' : true, coverUrl: work.coverUrl } : null}
        onClose={() => setAdding(null)}
      />
    </div>
  )
}
