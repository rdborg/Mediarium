import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, type BookFound, type MusicArtistResult, type MusicDiscoverItem, type TitleResult } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import AddMusicDialog from '../components/AddMusicDialog'
import { artistKind } from '../components/artistKind'
import { BookAddDialog, BookCard } from '../components/BookDiscover'
import Icon from '../components/Icon'
import PosterCard from '../components/PosterCard'
import { capitalizeFirst } from '../components/SearchBox'
import { describeState, resultState } from '../components/state'
import { useModules } from '../ModulesContext'
import { booksTabLabel, musicTabLabel, resultsLink, resultsParams, resultsTab, splitArtists, splitBooks } from '../searchTabs'
import { t, tc } from '../i18n'
import KindGlyph from '../components/KindGlyph'
import '../search.css'

// Search the movie and TV databases as you type, like Radarr's and Sonarr's
// "Add New": pick a title, choose how to add it, and Mediarium then searches
// the indexers for releases of that title. Titles you already own are listed
// first and say where they stand. With ebooks or audiobooks switched on, books
// have a tab of their own, found the way the search box finds them, and so
// does music (artists from MusicBrainz).
export default function Search() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { on } = useModules()
  const titlesOn = on('movies') || on('tv')
  const booksOn = on('ebooks') || on('audiobooks')
  const musicOn = on('music')
  const tab = resultsTab(params.get('tab'), titlesOn, booksOn, musicOn)
  const urlQ = params.get('q') ?? ''
  const [q, setQ] = useState(urlQ)
  const [results, setResults] = useState<TitleResult[] | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [bookHits, setBookHits] = useState<BookFound[] | null>(null)
  const [bookError, setBookError] = useState('')
  const [addingBook, setAddingBook] = useState<BookFound | null>(null)
  const [booksAdded, setBooksAdded] = useState<Record<string, number>>({}) // added while on the page
  const [artistHits, setArtistHits] = useState<MusicArtistResult[] | null>(null)
  const [artistError, setArtistError] = useState('')
  const [addingArtist, setAddingArtist] = useState<MusicArtistResult | null>(null)
  // Music can be searched by artist, album or song (a soundtrack by a song from the film).
  const [musicBy, setMusicBy] = useState<'artist' | 'album' | 'song'>('artist')
  const [albumHits, setAlbumHits] = useState<(MusicDiscoverItem & { song?: string })[] | null>(null)
  const [addingAlbum, setAddingAlbum] = useState<MusicDiscoverItem | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => inputRef.current?.focus(), [])
  // A new query from the top bar replaces what is typed here. The address is
  // updated with the trimmed text after each pause, so leave what is typed
  // alone when it only differs by spaces (or the word being typed loses its space).
  useEffect(() => setQ((cur) => (cur.trim() === urlQ ? cur : urlQ)), [urlQ])

  // Live: search shortly after the last keystroke, dropping stale answers.
  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setResults(null)
      setError('')
      // Deleting down to one letter (or nothing) must not leave the old search in the address.
      if (term !== urlQ) setParams(resultsParams(tab, term), { replace: true })
      return
    }
    const ctl = new AbortController()
    const timer = setTimeout(() => {
      setError('')
      if (titlesOn) {
        api
          .discoverSearch(term, ctl.signal)
          .then(setResults)
          .catch((e) => {
            if (e instanceof DOMException && e.name === 'AbortError') return
            setError(e instanceof Error ? e.message : String(e))
          })
      } else {
        setResults([])
      }
      setParams(resultsParams(tab, term), { replace: true })
    }, 250)
    return () => {
      clearTimeout(timer)
      ctl.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q])

  // Books come from Open Library, a little after the titles, whichever tab is
  // open, so the Books tab can say how many it found.
  useEffect(() => {
    const term = q.trim()
    if (!booksOn || term.length < 2) {
      setBookHits(null)
      setBookError('')
      return
    }
    let stale = false
    const timer = setTimeout(() => {
      api
        .searchBooks(term)
        .then((r) => {
          if (stale) return
          setBookHits(r)
          setBookError('')
        })
        .catch((e) => {
          if (stale) return
          setBookHits([])
          setBookError(e instanceof Error ? e.message : String(e))
        })
    }, 450)
    return () => {
      stale = true
      clearTimeout(timer)
    }
  }, [q, booksOn])

  // Artists come from MusicBrainz, which allows about one lookup a second, so
  // they are asked for a little later still. Like the search box, only when
  // music is on.
  useEffect(() => {
    const term = q.trim()
    if (!musicOn || term.length < 2) {
      setArtistHits(null)
      setAlbumHits(null)
      setArtistError('')
      return
    }
    const ctl = new AbortController()
    const timer = setTimeout(() => {
      if (musicBy !== 'artist') {
        setAlbumHits(null)
        api
          .musicSearchTitles(term, musicBy, ctl.signal)
          .then((r) => {
            setAlbumHits(r)
            setArtistError('')
          })
          .catch((e) => {
            if (e instanceof DOMException && e.name === 'AbortError') return
            setAlbumHits([])
            setArtistError(e instanceof Error ? e.message : String(e))
          })
        return
      }
      api
        .musicSearch(term, ctl.signal)
        .then((r) => {
          setArtistHits(r)
          setArtistError('')
        })
        .catch((e) => {
          if (e instanceof DOMException && e.name === 'AbortError') return
          setArtistHits([])
          setArtistError(e instanceof Error ? e.message : String(e))
        })
    }, 600)
    return () => {
      clearTimeout(timer)
      ctl.abort()
    }
  }, [q, musicOn, musicBy])

  // A book added from a card on this page moves up to "In your library".
  useEffect(() => {
    const onAdded = (e: Event) => {
      const d = (e as CustomEvent<{ key: string; id: number }>).detail
      setBooksAdded((o) => ({ ...o, [d.key]: d.id }))
    }
    window.addEventListener('mediarium:book-added', onAdded)
    return () => window.removeEventListener('mediarium:book-added', onAdded)
  }, [])

  const open = (r: TitleResult) => navigate(r.kind === 'movie' ? `/title/${r.tmdbId}` : `/series/${r.libraryId}`)

  const grid = (list: TitleResult[]) => (
    <div className="poster-grid">
      {list.map((r) => (
        <PosterCard
          key={`${r.kind}-${r.tmdbId}`}
          to={r.inLibrary ? (r.kind === 'movie' ? `/title/${r.tmdbId}` : `/series/${r.libraryId}`) : r.kind === 'movie' ? `/title/${r.tmdbId}` : `/show/${r.tmdbId}`}
          poster={r.posterUrl}
          title={r.title}
          meta={[r.year || null, r.kind === 'tv' ? t('TV show') : t('Movie')].filter(Boolean).join(' · ')}
          state={resultState(r)}
          kind={r.kind}
          genres={r.genres}
          rating={r.rating}
          footer={
            <div className="pcard-footer">
              {r.inLibrary ? (
                <button className="btn-sm btn-with-icon" onClick={() => open(r)}>
                  <Icon name="open" size={15} /> {t('Open')}
                </button>
              ) : (
                <button
                  className="primary btn-sm btn-with-icon"
                  onClick={() => setAdding({ kind: r.kind, tmdbId: r.tmdbId, title: r.title, year: r.year, posterUrl: r.posterUrl, overview: r.overview })}
                >
                  <Icon name="plus" size={15} /> {t('Add')}
                </button>
              )}
            </div>
          }
        />
      ))}
    </div>
  )

  const artistGrid = (list: MusicArtistResult[]) => (
    <div className="poster-grid">
      {list.map((a) => (
        <PosterCard
          key={a.mbid}
          to={a.artistId > 0 ? `/music/artist/${a.artistId}` : undefined}
          title={a.name}
          meta={[artistKind(a.type), a.disambiguation || a.country].filter(Boolean).join(' · ')}
          kind="music"
          state={a.artistId > 0 ? { ...describeState('downloaded'), label: t('In library') } : undefined}
          actions={
            a.artistId > 0
              ? [{ icon: 'open', label: t('Open'), onClick: () => navigate(`/music/artist/${a.artistId}`) }]
              : [{ icon: 'plus', label: t('Add'), onClick: () => setAddingArtist(a), primary: true }]
          }
        />
      ))}
    </div>
  )

  const albumGrid = (list: (MusicDiscoverItem & { song?: string })[]) => (
    <div className="poster-grid">
      {list.map((a) => (
        <PosterCard
          key={a.mbid}
          to={a.albumId ? `/music/album/${a.albumId}` : undefined}
          poster={a.coverUrl}
          title={a.title}
          meta={[a.artistName, a.releaseDate?.slice(0, 4) || null, a.song ? t('has “{song}”', { song: a.song }) : null].filter(Boolean).join(' · ')}
          kind="music"
          state={a.inLibrary ? { ...describeState('downloaded'), label: t('In library') } : undefined}
          actions={
            a.inLibrary && a.albumId
              ? [{ icon: 'open', label: t('Open'), onClick: () => navigate(`/music/album/${a.albumId}`) }]
              : [{ icon: 'plus', label: t('Add'), onClick: () => setAddingAlbum(a), primary: true }]
          }
        />
      ))}
    </div>
  )

  const owned = results?.filter((r) => r.inLibrary) ?? []
  const fresh = results?.filter((r) => !r.inLibrary) ?? []
  const books = splitBooks(bookHits ?? [], booksAdded)
  const artists = splitArtists(artistHits ?? [])
  const onBooks = tab === 'books'
  const onMusic = tab === 'music'
  const onTitles = tab === 'titles'
  const term = q.trim()

  return (
    <div className="search-page">
      <div className="page-header">
        <h1>{tc('noun', 'Search')}</h1>
      </div>
      <nav className="search-modes" aria-label={tc('noun', 'Search')}>
        {titlesOn && (
          <Link to={resultsLink('titles', term)} className={onTitles ? 'active' : undefined} aria-current={onTitles ? 'page' : undefined}>
            <Icon name="film" size={15} /> {t('Movies and shows')}
          </Link>
        )}
        {booksOn && (
          <Link to={resultsLink('books', term)} className={onBooks ? 'active' : undefined} aria-current={onBooks ? 'page' : undefined}>
            <Icon name="book" size={15} /> {booksTabLabel(bookHits === null ? null : bookHits.length)}
          </Link>
        )}
        {musicOn && (
          <Link to={resultsLink('music', term)} className={onMusic ? 'active' : undefined} aria-current={onMusic ? 'page' : undefined}>
            <Icon name="music" size={15} /> {musicTabLabel(artistHits === null ? null : artistHits.length)}
          </Link>
        )}
        <Link to="/search/releases">
          <Icon name="list" size={15} /> {t('Releases')}
        </Link>
      </nav>

      <div className="search-bar">
        <Icon name="search" size={22} />
        <input
          ref={inputRef}
          type="search"
          value={q}
          onChange={(e) => setQ(capitalizeFirst(e.target.value))}
          placeholder={onBooks ? t('Type a book title or an author…') : onMusic ? (musicBy === 'album' ? t('Type an album title…') : musicBy === 'song' ? t('Type a song title…') : t('Type an artist or band name…')) : t('Type a movie or show name…')}
          aria-label={t('Search')}
        />
      </div>

      {onBooks && (
        <>
          {bookError && <p className="error-text">{bookError}</p>}
          {bookHits === null && term.length < 2 && (
            <div className="search-hint">
              <span className="search-hint-shapes" aria-hidden="true">
                <KindGlyph kind="book" size={30} variant="solid" />
                <KindGlyph kind="audiobook" size={30} variant="solid" />
              </span>
              <p>{t('Start typing to search books. Anything already in your library shows where it stands.')}</p>
            </div>
          )}
          {bookHits === null && term.length >= 2 && <div className="search-hint">{t('Searching…')}</div>}
          {bookHits !== null && bookHits.length === 0 && !bookError && <div className="search-hint">{t('Nothing found for “{q}”.', { q: term })}</div>}
          {books.owned.length > 0 && (
            <section style={{ marginBottom: 28 }}>
              <div className="rail-head">
                <h2>{t('In your library')}</h2>
                <span>{books.owned.length}</span>
              </div>
              <div className="poster-grid">
                {books.owned.map((b) => (
                  <BookCard key={b.key} book={b} onAdd={setAddingBook} />
                ))}
              </div>
            </section>
          )}
          {books.fresh.length > 0 && (
            <section>
              <div className="rail-head">
                <h2>{t('Add new')}</h2>
                <span>{books.fresh.length}</span>
              </div>
              <div className="poster-grid">
                {books.fresh.map((b) => (
                  <BookCard key={b.key} book={b} onAdd={setAddingBook} />
                ))}
              </div>
            </section>
          )}
          <BookAddDialog target={addingBook} onClose={() => setAddingBook(null)} />
        </>
      )}

      {onMusic && (
        <div className="seg search-by" role="group" aria-label={t('Search music by')}>
          {(['artist', 'album', 'song'] as const).map((by) => (
            <button key={by} type="button" aria-pressed={musicBy === by} className={musicBy === by ? 'active' : ''} onClick={() => setMusicBy(by)}>
              {by === 'artist' ? t('Artists') : by === 'album' ? t('Albums') : t('Songs')}
            </button>
          ))}
        </div>
      )}

      {onMusic && musicBy !== 'artist' && (
        <>
          {artistError && <p className="error-text">{artistError}</p>}
          {albumHits === null && term.length < 2 && <div className="search-hint">{musicBy === 'song' ? t('Type a song to find the albums it is on, a soundtrack too.') : t('Type an album title.')}</div>}
          {albumHits === null && term.length >= 2 && <div className="search-hint">{t('Searching…')}</div>}
          {albumHits !== null && albumHits.length === 0 && !artistError && <div className="search-hint">{t('Nothing found for “{q}”.', { q: term })}</div>}
          {albumHits !== null && albumHits.length > 0 && albumGrid(albumHits)}
        </>
      )}

      {onMusic && musicBy === 'artist' && (
        <>
          {artistError && <p className="error-text">{artistError}</p>}
          {artistHits === null && term.length < 2 && (
            <div className="search-hint">
              <span className="search-hint-shapes" aria-hidden="true">
                <KindGlyph kind="music" size={30} variant="solid" />
              </span>
              <p>{t('Start typing to search music. Anything already in your library shows where it stands.')}</p>
            </div>
          )}
          {artistHits === null && term.length >= 2 && <div className="search-hint">{t('Searching…')}</div>}
          {artistHits !== null && artistHits.length === 0 && !artistError && <div className="search-hint">{t('Nothing found for “{q}”.', { q: term })}</div>}
          {artists.owned.length > 0 && (
            <section style={{ marginBottom: 28 }}>
              <div className="rail-head">
                <h2>{t('In your library')}</h2>
                <span>{artists.owned.length}</span>
              </div>
              {artistGrid(artists.owned)}
            </section>
          )}
          {artists.fresh.length > 0 && (
            <section>
              <div className="rail-head">
                <h2>{t('Add new')}</h2>
                <span>{artists.fresh.length}</span>
              </div>
              {artistGrid(artists.fresh)}
            </section>
          )}
        </>
      )}

      {onTitles && error && <p className="error-text">{error}</p>}
      {onTitles && results === null && !error && q.trim().length < 2 && (
        <div className="search-hint">
          <span className="search-hint-shapes" aria-hidden="true">
            <KindGlyph kind="movie" size={30} variant="solid" />
            <KindGlyph kind="tv" size={30} variant="solid" />
          </span>
          <p>{t('Start typing to search movies and TV shows. Anything already in your library shows where it stands.')}</p>
        </div>
      )}
      {onTitles && results !== null && results.length === 0 && <div className="search-hint">{t('Nothing found for “{q}”.', { q: q.trim() })}</div>}
      {onTitles && owned.length > 0 && (
        <section style={{ marginBottom: 28 }}>
          <div className="rail-head">
            <h2>{t('In your library')}</h2>
            <span>{owned.length}</span>
          </div>
          {grid(owned)}
        </section>
      )}
      {onTitles && fresh.length > 0 && (
        <section>
          <div className="rail-head">
            <h2>{t('Add new')}</h2>
            <span>{fresh.length}</span>
          </div>
          {grid(fresh)}
        </section>
      )}

      {addingAlbum && (
        <AddMusicDialog
          target={{ kind: 'album', item: addingAlbum }}
          onClose={() => setAddingAlbum(null)}
          onAdded={(id, albumId) => {
            setAddingAlbum(null)
            navigate(albumId ? `/music/album/${albumId}` : `/music/artist/${id}`)
          }}
        />
      )}
      {addingArtist && (
        <AddMusicDialog
          target={{ kind: 'artist', mbid: addingArtist.mbid, name: addingArtist.name, type: addingArtist.type, country: addingArtist.country, disambiguation: addingArtist.disambiguation }}
          onClose={() => setAddingArtist(null)}
          onAdded={(id) => {
            setAddingArtist(null)
            navigate(`/music/artist/${id}`)
          }}
        />
      )}
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            const added = adding
            setAdding(null)
            navigate(added.kind === 'movie' ? `/title/${added.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </div>
  )
}
