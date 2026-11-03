import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type BookFormat, type BookWork as Work } from '../api'
import AuthorBooks from '../components/AuthorBooks'
import BookSeries from '../components/BookSeries'
import { BookAddDialog } from '../components/BookDiscover'
import Icon from '../components/Icon'
import PageLoading from '../components/PageLoading'
import { subjectLabel } from '../subjectLabel'
import { PosterFallback } from '../components/PosterCard'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
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
  if (!work) return <PageLoading label={t('Looking up the book')} />

  const formats = (['ebook', 'audiobook'] as BookFormat[]).filter((f) => on(f === 'ebook' ? 'ebooks' : 'audiobooks'))
  return (
    <div>
      <section className="title-hero kind-book">
        {work.coverUrl && (
          <div className="title-scene cover-scene" aria-hidden="true">
            <img src={work.coverUrl} alt="" />
          </div>
        )}
        <div className="title-poster">{work.coverUrl ? <img src={work.coverUrl} alt={work.title} /> : <PosterFallback />}</div>
        <div className="title-info">
          <div className="title-kicker">
            <span className="kind-tag kind-book">
              <Icon name="book" size={13} /> {t('Book')}
            </span>
            {libraryId ? (
              <span className="state-tag st-downloaded">
                <Icon name="check" size={13} /> {t('In your library')}
              </span>
            ) : null}
          </div>
          <h1>{work.title}</h1>
          <p className="title-tagline">{[work.author, work.year].filter(Boolean).join(' · ')}</p>
          <div className="title-chips">
            <span className={`chip-plain edition ${work.hasEbook ? 'yes' : 'no'}`} title={t('Whether an ebook edition has been published')}>
              <Icon name="book" size={13} /> {work.hasEbook ? t('eBook editions exist') : t('No ebook edition listed')}
            </span>
            <span className={`chip-plain edition ${work.hasAudio ? 'yes' : 'no'}`} title={t('Whether an audiobook edition has been published')}>
              <Icon name="headphones" size={13} /> {work.hasAudio ? t('Audiobook editions exist') : t('No audiobook edition listed')}
            </span>
            {(work.subjects ?? []).slice(0, 6).map((s) => (
              <span key={s} className="chip-genre">
                {subjectLabel(s)}
              </span>
            ))}
          </div>
          {work.description && <p className="title-overview">{work.description}</p>}
          <div className="title-actions">
            {libraryId ? (
              <Link className="primary btn-with-icon" to={`/book/${libraryId}`}>
                <Icon name="open" size={16} /> {t('Open in your library')}
              </Link>
            ) : formats.length === 0 ? (
              <p className="hint">{t('Ebooks and audiobooks are switched off.')}</p>
            ) : (
              <>
                {formats.map((f, i) => (
                  <button key={f} className={`${i === 0 ? 'primary ' : ''}btn-with-icon big`} onClick={() => setAdding({ prefer: f })}>
                    <Icon name={f === 'ebook' ? 'book' : 'headphones'} size={18} /> {f === 'ebook' ? t('Add as eBook') : t('Add as audiobook')}
                  </button>
                ))}
                {formats.length === 2 && (
                  <button className="btn-with-icon" onClick={() => setAdding({})}>
                    <Icon name="plus" size={16} /> {t('Both')}
                  </button>
                )}
              </>
            )}
            {work.key.startsWith('hc-') ? (
              <a className="btn-with-icon link-quiet" href={`https://hardcover.app/books/${work.slug || work.key.slice(3)}`} target="_blank" rel="noreferrer" title={t('More about this book')}>
                <Icon name="external" size={16} /> {t('Hardcover')}
              </a>
            ) : work.key.startsWith('gb-') ? (
              <a className="btn-with-icon link-quiet" href={`https://books.google.com/books?id=${encodeURIComponent(work.key.slice(3))}`} target="_blank" rel="noreferrer" title={t('More about this book')}>
                <Icon name="external" size={16} /> {t('Google Books')}
              </a>
            ) : (
              <a className="btn-with-icon link-quiet" href={`https://openlibrary.org/works/${work.key}`} target="_blank" rel="noreferrer" title={t('More about this book')}>
                <Icon name="external" size={16} /> {t('Open Library')}
              </a>
            )}
          </div>
          <p className="hint source-note">
            {work.key.startsWith('hc-')
              ? t('Book details come from Hardcover. "Editions exist" means one has been published; whether a release turns up depends on your indexers.')
              : work.key.startsWith('gb-')
                ? t('Book details come from Google Books. "Editions exist" means one has been published; whether a release turns up depends on your indexers.')
                : t('Book details come from Open Library. "Editions exist" means one has been published; whether a release turns up depends on your indexers.')}
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
