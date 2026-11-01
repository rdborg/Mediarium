import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, can, isAdmin, type Book, type BookFormat, type QueueItem, type SearchResult } from '../api'
import { useAuth } from '../AuthContext'
import ChangeCoverDialog from '../components/ChangeCoverDialog'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import KindGlyph from '../components/KindGlyph'
import { PosterFallback } from '../components/PosterCard'
import ReleaseNotice from '../components/ReleaseNotice'
import ReleaseTable from '../components/ReleaseTable'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { useTorrentsSwitch } from '../useTorrentsSwitch'
import AuthorBooks from '../components/AuthorBooks'
import BookSeries from '../components/BookSeries'
import { audioDetails, localDate } from '../bookSeries'
import { useDocumentTitle } from '../documentTitle'
import { locale, t, st as serverText } from '../i18n'
import { useModules } from '../ModulesContext'
import { useLive } from '../useLive'
import { bookState } from './BookLibrary'
import { listenPath, openBookApp, readPath } from '../bookshelf/open'
import type { Unanswered } from '../releaseHint'
import type { Searched } from '../searched'

const FORMAT_TITLE: Record<BookFormat, string> = { ebook: t('Ebook'), audiobook: t('Audiobook') }

// One book: its details from Open Library, and a panel for each format that is
// switched on, with where it stands, the file and the releases to choose from.
export default function BookDetail() {
  const { id } = useParams()
  // Keyed by the book, so opening another one from a rail starts clean instead of showing the last book's panels.
  return <BookPage key={id} id={id} />
}

function BookPage({ id }: { id?: string }) {
  const bookId = Number(id)
  const navigate = useNavigate()
  const confirm = useConfirm()
  const toast = useToast()
  const me = useAuth().user
  const admin = isAdmin(me)
  const { on } = useModules()
  const [book, setBook] = useState<Book | null>(null)
  const [changingCover, setChangingCover] = useState(false)
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [error, setError] = useState('')
  const [links, setLinks] = useState<{ serverId: number; name: string; kind: string; url: string }[]>([])
  useDocumentTitle(book?.title)
  useEffect(() => {
    api.bookLinks(bookId).then(setLinks).catch(() => setLinks([]))
  }, [bookId])

  const load = useCallback(() => {
    api
      .getBook(bookId)
      .then((b) => {
        setBook(b)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.listQueue().then(setQueue).catch(() => undefined)
  }, [bookId])
  useEffect(load, [load])
  useLive(load, 5000)

  async function remove() {
    if (!book) return
    const hasFiles = !!(book.ebook.path || book.audiobook.path)
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: book.title }),
      body: <p>{t('Mediarium stops looking for this book and cancels anything still downloading for it. You can add it back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: hasFiles ? { label: t('Also delete its files'), hint: t('The ebook file and the audiobook folder, if there are any.'), defaultChecked: false } : undefined,
    })
    if (!answer) return
    try {
      await api.deleteBook(book.id, answer.checked)
      toast.success(t('{title} removed.', { title: book.title }))
      navigate('/library')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (error && !book) return <p className="error-text">{error}</p>
  if (!book) return <div className="skeleton" style={{ height: 320, borderRadius: 22 }} />

  // A book added from Audible has no Open Library page, so the link and the series lookup are left out.
  const fromAudible = book.olKey.startsWith('audnexus:')
  const fromHardcover = book.olKey.startsWith('hc-')
  const formats = (['ebook', 'audiobook'] as BookFormat[]).filter((f) => on(f === 'ebook' ? 'ebooks' : 'audiobooks') || book[f].status === 'downloaded')
  return (
    <div>
      <section className="title-hero kind-book">
        <div className="title-poster">{book.coverUrl ? <img src={book.coverUrl.replace('-M.jpg', '-L.jpg')} alt={book.title} /> : <PosterFallback />}</div>
        <div className="title-info">
          <div className="title-kicker">
            <span className="kind-tag kind-book">
              <Icon name="book" size={13} /> {t('Book')}
            </span>
          </div>
          <h1>{book.title}</h1>
          <p className="title-tagline">
            {[book.author, book.year, book.seriesName && (book.seriesPosition ? t('Book {n} of {series}', { n: book.seriesPosition, series: book.seriesName }) : book.seriesName)].filter(Boolean).join(' · ')}
          </p>
          {book.releaseDate && book.releaseDate > localDate(new Date()) && (
            <p className="notice notice-info" style={{ marginTop: 8 }}>
              {t('Comes out on {date}. Mediarium starts looking for it then.', { date: new Date(book.releaseDate + 'T00:00:00').toLocaleDateString(locale(), { day: 'numeric', month: 'long', year: 'numeric' }) })}
            </p>
          )}
          {book.description && <p className="title-overview">{book.description}</p>}
          <div className="title-links">
            {links.map((l) => (
              <a key={l.serverId} className="link-pill primary-pill" href={l.url} target="_blank" rel="noreferrer">
                <Icon name={l.kind === 'audiobookshelf' ? 'headphones' : 'book'} size={13} /> {l.kind === 'audiobookshelf' ? t('Listen in {name}', { name: l.name }) : t('Read in {name}', { name: l.name })}
              </a>
            ))}
            {!fromAudible && !fromHardcover && (
              <a className="link-pill" href={`https://openlibrary.org/works/${book.olKey}`} target="_blank" rel="noreferrer">
                <Icon name="external" size={13} /> {t('More about this book')}
              </a>
            )}
            {fromHardcover && (
              <a className="link-pill" href={`https://hardcover.app/search?q=${encodeURIComponent(book.title)}`} target="_blank" rel="noreferrer">
                <Icon name="external" size={13} /> {t('Hardcover')}
              </a>
            )}
          </div>
          {(admin || can(me, 'manage')) && (
            <div className="title-actions">
              {can(me, 'manage') && (
                <button className="btn-with-icon" onClick={() => setChangingCover(true)}>
                  <Icon name="image" size={16} /> {t('Change cover')}
                </button>
              )}
              {admin && (
                <button className="btn-with-icon danger-ghost" onClick={() => void remove()}>
                  <Icon name="trash" size={16} /> {t('Remove')}
                </button>
              )}
            </div>
          )}
        </div>
      </section>

      {changingCover && <ChangeCoverDialog book={book} onClose={() => setChangingCover(false)} onChanged={load} />}

      <div className="book-formats">
        {formats.map((f) => (
          <FormatPanel key={f} book={book} format={f} queue={queue} onChanged={load} />
        ))}
      </div>
      {!fromAudible && <BookSeries workKey={book.olKey} title={book.title} author={book.author} />}
      {book.authorKey && <AuthorBooks authorKey={book.authorKey} author={book.author} exclude={book.olKey} />}
      {formats.length === 0 && (
        <p className="hint">
          {t('Ebooks and audiobooks are both switched off.')} {admin ? <Link to="/settings/modules">{t('Switch one on in Settings > Media types.')}</Link> : null}
        </p>
      )}
    </div>
  )
}

function FormatPanel({ book, format, queue, onChanged }: { book: Book; format: BookFormat; queue: QueueItem[]; onChanged: () => void }) {
  const me = useAuth().user
  const toast = useToast()
  const { grabError } = useTorrentsSwitch()
  const st = book[format]
  const state = bookState(book, format, queue)
  const [searching, setSearching] = useState(false)
  const [releases, setReleases] = useState<SearchResult[] | null>(null)
  const [answered, setAnswered] = useState<{ sources: number; unanswered: Unanswered[]; warnings?: string[]; skipped: string[]; searched?: Searched[] }>({ sources: 0, unanswered: [], skipped: [] })
  const [open, setOpen] = useState(false)
  const [releaseError, setReleaseError] = useState('')
  const [grabbing, setGrabbing] = useState<string | null>(null)

  async function setWanted(v: boolean) {
    try {
      await api.setBookWanted(book.id, format, v)
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function searchNow() {
    setSearching(true)
    try {
      const r = await api.searchNowBook(book.id, format)
      if (r.grabbed > 0) toast.success(serverText(r.message))
      else toast.info(serverText(r.message))
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSearching(false)
    }
  }

  function showReleases() {
    setOpen(true)
    setReleases(null)
    setReleaseError('')
    api
      .bookReleases(book.id, format)
      .then((a) => {
        setAnswered({ sources: a.sources, unanswered: a.unanswered, warnings: a.warnings, skipped: a.skipped ?? [], searched: a.searched })
        setReleases(a.results)
      })
      .catch((e) => setReleaseError(e instanceof Error ? e.message : String(e)))
  }

  async function grab(r: SearchResult) {
    setGrabbing(r.downloadUrl)
    try {
      await api.grabBook(book.id, { format, releaseTitle: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol })
      toast.success(t('Started downloading "{title}". Follow it in Activity.', { title: r.title }))
      setOpen(false)
      setTimeout(onChanged, 500)
    } catch (e) {
      grabError(e, () => void grab(r))
    } finally {
      setGrabbing(null)
    }
  }

  return (
    <section className={`card book-format kind-${format}`}>
      <header className="book-format-head">
        <h2>
          <KindGlyph kind={format === 'ebook' ? 'book' : 'audiobook'} size={30} variant="solid" /> {FORMAT_TITLE[format]}
        </h2>
        <span className={`state-tag st-${state.key}`} title={state.hint}>
          <Icon name={state.icon} size={13} /> {state.label}
        </span>
      </header>
      {format === 'audiobook' && audioDetails(book.narrators, book.runtimeMin) && <p className="book-audio-details">{audioDetails(book.narrators, book.runtimeMin)}</p>}
      {st.status === 'downloaded' ? (
        <p className="book-file">
          {st.format ? <strong>{st.format.toUpperCase()}</strong> : null}
          {st.path ? <code>{st.path}</code> : null}
        </p>
      ) : (
        <p className="hint">
          {st.wanted
            ? format === 'ebook'
              ? t('Mediarium looks for the ebook on your indexers and downloads the best one it finds.')
              : t('Mediarium looks for the audiobook on your indexers and downloads the best one it finds.')
            : format === 'ebook'
              ? t('Not wanted as an ebook.')
              : t('Not wanted as an audiobook.')}
        </p>
      )}
      <Switch checked={st.wanted} onChange={(v) => void setWanted(v)} label={format === 'ebook' ? t('Want the ebook') : t('Want the audiobook')} />
      <div className="row-actions">
        {st.status === 'downloaded' && can(me, 'play') && (
          <button className="primary btn-with-icon" onClick={() => openBookApp(format === 'ebook' ? readPath(book.id) : listenPath(book.id))}>
            <Icon name={format === 'ebook' ? 'book' : 'play'} size={16} /> {format === 'ebook' ? t('Read') : t('Listen')}
          </button>
        )}
        {can(me, 'manage') && (
          <button className={`${st.status === 'downloaded' ? '' : 'primary '}btn-with-icon`} disabled={searching || !st.wanted} onClick={() => void searchNow()}>
            <Icon name="search" size={16} /> {searching ? t('Searching…') : t('Search now')}
          </button>
        )}
        {can(me, 'releases') && (
          <button className="btn-with-icon" onClick={() => (open ? setOpen(false) : showReleases())}>
            <Icon name="list" size={16} /> {open ? t('Hide releases') : t('Choose a release')}
          </button>
        )}
      </div>
      {open && (
        <div className="book-releases">
          {releaseError && <p className="error-text">{releaseError}</p>}
          {!releaseError && releases === null && <div className="skeleton" style={{ height: 120 }} />}
          {releases && releases.length > 0 && (
            <div className="table-scroll">
              <ReleaseTable results={releases} searched={answered.searched} grabbing={grabbing} onGrab={grab} onClose={() => setOpen(false)} />
            </div>
          )}
          {releases && <ReleaseNotice sources={answered.sources} unanswered={answered.unanswered} warnings={answered.warnings} skipped={answered.skipped} searched={answered.searched} results={releases.length} />}
        </div>
      )}
    </section>
  )
}
