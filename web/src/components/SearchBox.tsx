import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, type BookFound, type MusicArtistResult, type TitleResult } from '../api'
import { useModules } from '../ModulesContext'
import AddMusicDialog from './AddMusicDialog'
import { BookAddDialog } from './BookDiscover'
import { artistKind } from './artistKind'
import Icon from './Icon'
import { MusicFallback, PosterFallback } from './PosterCard'
import { resultState } from './state'
import { isTypingTarget } from '../shortcuts'

// The always-visible search bar. It searches the movie and TV databases live
// as you type and lists matches right under the box, with posters, so you can
// jump to something you own or add something new without leaving the page.
// The first letter is always a capital, as titles are written.
export function capitalizeFirst(v: string): string {
  return v.length > 0 ? v.charAt(0).toUpperCase() + v.slice(1) : v
}

export default function SearchBox() {
  const navigate = useNavigate()
  const location = useLocation()
  const [q, setQ] = useState('')
  const [results, setResults] = useState<TitleResult[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const box = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const { on } = useModules()
  const musicOn = on('music')
  const titlesOn = on('movies') || on('tv')
  const [artists, setArtists] = useState<MusicArtistResult[] | null>(null)
  const [addingArtist, setAddingArtist] = useState<MusicArtistResult | null>(null)
  const booksOn = on('ebooks') || on('audiobooks')
  const [bookHits, setBookHits] = useState<BookFound[] | null>(null)
  const [addingBook, setAddingBook] = useState<BookFound | null>(null)

  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setResults(null)
      setError('')
      setLoading(false)
      return
    }
    if (!titlesOn) {
      setResults([])
      setLoading(false)
      return
    }
    setLoading(true)
    const ctl = new AbortController()
    const t = setTimeout(() => {
      api
        .discoverSearch(term, ctl.signal)
        .then((r) => {
          setResults(r.slice(0, 8))
          setError('')
          setActive(-1)
        })
        .catch((e) => {
          if (e instanceof DOMException && e.name === 'AbortError') return
          setError(e instanceof Error ? e.message : String(e))
          setResults(null)
        })
        .finally(() => !ctl.signal.aborted && setLoading(false))
    }, 220)
    return () => {
      clearTimeout(t)
      ctl.abort()
    }
  }, [q, titlesOn])

  // Artists come from MusicBrainz, which allows about one lookup a second, so
  // they are asked for a little later than titles.
  useEffect(() => {
    const term = q.trim()
    if (!musicOn || term.length < 2) {
      setArtists(null)
      return
    }
    const ctl = new AbortController()
    const t = setTimeout(() => {
      api
        .musicSearch(term, ctl.signal)
        .then((r) => setArtists(r.slice(0, 5)))
        .catch((e) => {
          if (e instanceof DOMException && e.name === 'AbortError') return
          setArtists([])
        })
    }, 600)
    return () => {
      clearTimeout(t)
      ctl.abort()
    }
  }, [q, musicOn])

  // Books come from Open Library, asked a moment after titles too.
  useEffect(() => {
    const term = q.trim()
    if (!booksOn || term.length < 2) {
      setBookHits(null)
      return
    }
    let stale = false
    const t = setTimeout(() => {
      api
        .searchBooks(term)
        .then((r) => !stale && setBookHits(r.slice(0, 5)))
        .catch(() => !stale && setBookHits([]))
    }, 450)
    return () => {
      stale = true
      clearTimeout(t)
    }
  }, [q, booksOn])

  // Close when the page changes or you click elsewhere.
  useEffect(() => setOpen(false), [location.pathname])
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [])

  // "/" jumps to the search box from anywhere, unless you are typing in a
  // field or a window is open.
  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return
      if (isTypingTarget(e.target) || document.querySelector('[role="dialog"], [role="alertdialog"]')) return
      e.preventDefault()
      input.current?.focus()
      input.current?.select()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])

  const openItem = (r: TitleResult) => {
    setOpen(false)
    navigate(r.kind === 'movie' ? `/title/${r.tmdbId}` : `/series/${r.libraryId}`)
  }
  // A title you have goes to its page; one you don't opens its full info page,
  // fetched live, with an Add button.
  const choose = (r: TitleResult) => {
    if (r.inLibrary) return openItem(r)
    setOpen(false)
    navigate(r.kind === 'movie' ? `/title/${r.tmdbId}` : `/show/${r.tmdbId}`)
  }
  const chooseArtist = (a: MusicArtistResult) => {
    setOpen(false)
    if (a.artistId > 0) navigate(`/music/artist/${a.artistId}`)
    else setAddingArtist(a)
  }
  const chooseBook = (b: BookFound) => {
    setOpen(false)
    navigate(b.libraryId ? `/book/${b.libraryId}` : `/books/work/${b.key}`)
  }
  const seeAll = () => {
    setOpen(false)
    if (q.trim()) navigate(`/search?q=${encodeURIComponent(q.trim())}`)
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    const titleCount = results?.length ?? 0
    const artistEnd = titleCount + (artists?.length ?? 0)
    const count = artistEnd + (bookHits?.length ?? 0)
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setOpen(true)
      setActive((a) => (a + 1 >= count ? -1 : a + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => (a < 0 ? count - 1 : a - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (active >= 0 && active < titleCount && results?.[active]) choose(results[active])
      else if (active >= titleCount && active < artistEnd && artists?.[active - titleCount]) chooseArtist(artists[active - titleCount])
      else if (active >= artistEnd && bookHits?.[active - artistEnd]) chooseBook(bookHits[active - artistEnd])
      else seeAll()
    } else if (e.key === 'Escape') {
      // Escape closes the list; pressed again (nothing left to close) it leaves the box.
      if (showPanel) setOpen(false)
      else e.currentTarget.blur()
    }
  }

  const showPanel = open && q.trim().length >= 2
  const titleCount = results?.length ?? 0
  const artistEnd = titleCount + (artists?.length ?? 0)
  const nothingFound = results !== null && results.length === 0 && (!musicOn || (artists !== null && artists.length === 0)) && (!booksOn || (bookHits !== null && bookHits.length === 0))

  // A plain function, not a component: a component made inside another one is a
  // new type on every render, which would rebuild every row each time the mouse moves.
  function row(r: TitleResult, i: number) {
    const st = resultState(r)
    return (
      <div
        key={`${r.kind}-${r.tmdbId}`}
        className={`searchbox-row${i === active ? ' active' : ''}`}
        role="option"
        aria-selected={i === active}
        onMouseEnter={() => setActive(i)}
        onClick={() => choose(r)}
      >
        {r.posterUrl ? <img src={r.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}
        <div className="searchbox-text">
          <strong>{r.title}</strong>
          <span>
            <i className={`kind-dot kind-${r.kind}`} /> {r.kind === 'tv' ? 'TV show' : 'Movie'} · {r.year || '—'}
          </span>
        </div>
        {st ? (
          <span className={`state-tag st-${st.key}`} title={st.hint}>
            <Icon name={st.icon} size={12} /> {st.label}
          </span>
        ) : (
          <button
            className="primary btn-sm btn-with-icon"
            onClick={(e) => {
              e.stopPropagation()
              choose(r)
            }}
          >
            <Icon name="plus" size={14} /> Add
          </button>
        )}
      </div>
    )
  }

  const placeholder = `Search ${[on('movies') && 'movies', on('tv') && (on('movies') ? 'shows' : 'TV shows'), musicOn && 'artists', booksOn && 'books'].filter(Boolean).join(', ').replace(/, ([^,]*)$/, ' and $1') || 'Mediarium'}`

  return (
    <div className="searchbox" ref={box}>
      <span className="searchbox-icon" aria-hidden="true">
        <Icon name="search" size={17} />
      </span>
      <input
        ref={input}
        type="search"
        aria-label={placeholder}
        aria-keyshortcuts="/"
        title="Press / to search"
        role="combobox"
        aria-expanded={showPanel}
        aria-controls="searchbox-results"
        placeholder={placeholder}
        value={q}
        onChange={(e) => {
          setQ(capitalizeFirst(e.target.value))
          setOpen(true)
        }}
        autoCapitalize="sentences"
        onFocus={() => setOpen(true)}
        onKeyDown={onKey}
        autoComplete="off"
      />
      {showPanel && (
        <div className="searchbox-panel" id="searchbox-results" role="listbox">
          {loading && !results && (
            <>
              {[0, 1, 2].map((i) => (
                <div key={i} className="searchbox-row">
                  <div className="skeleton" style={{ width: 38, height: 56 }} />
                  <div style={{ flex: 1 }}>
                    <div className="skeleton" style={{ height: 14, width: '60%', marginBottom: 8 }} />
                    <div className="skeleton" style={{ height: 11, width: '30%' }} />
                  </div>
                </div>
              ))}
            </>
          )}
          {error && <div className="searchbox-empty error-text">{error}</div>}
          {nothingFound && <div className="searchbox-empty">Nothing found for “{q.trim()}”.</div>}
          {results && results.some((r) => r.inLibrary) && <div className="searchbox-group">In your library</div>}
          {results?.map((r, i) => r.inLibrary && row(r, i))}
          {results && results.some((r) => !r.inLibrary) && <div className="searchbox-group">Add new</div>}
          {results?.map((r, i) => !r.inLibrary && row(r, i))}
          {musicOn && artists && artists.length > 0 && <div className="searchbox-group">Artists</div>}
          {artists?.map((a, i) => (
            <div
              key={a.mbid}
              className={`searchbox-row${titleCount + i === active ? ' active' : ''}`}
              role="option"
              aria-selected={titleCount + i === active}
              onMouseEnter={() => setActive(titleCount + i)}
              onClick={() => chooseArtist(a)}
            >
              <MusicFallback />
              <div className="searchbox-text">
                <strong>{a.name}</strong>
                <span>
                  <i className="kind-dot kind-music" /> {artistKind(a.type)}
                  {a.disambiguation ? ` · ${a.disambiguation}` : a.country ? ` · ${a.country}` : ''}
                </span>
              </div>
              {a.artistId > 0 ? (
                <span className="state-tag st-downloaded" title="Already in your library">
                  <Icon name="check" size={12} /> In library
                </span>
              ) : (
                <button
                  className="primary btn-sm btn-with-icon"
                  onClick={(e) => {
                    e.stopPropagation()
                    chooseArtist(a)
                  }}
                >
                  <Icon name="plus" size={14} /> Add
                </button>
              )}
            </div>
          ))}
          {booksOn && bookHits && bookHits.length > 0 && <div className="searchbox-group">Books</div>}
          {bookHits?.map((b, i) => (
            <div
              key={b.key}
              className={`searchbox-row${artistEnd + i === active ? ' active' : ''}`}
              role="option"
              aria-selected={artistEnd + i === active}
              onMouseEnter={() => setActive(artistEnd + i)}
              onClick={() => chooseBook(b)}
            >
              {b.coverUrl ? <img src={b.coverUrl.replace('-M.jpg', '-S.jpg')} alt="" loading="lazy" /> : <PosterFallback />}
              <div className="searchbox-text">
                <strong>{b.title}</strong>
                <span>
                  <i className="kind-dot kind-book" /> {[b.author, b.year].filter(Boolean).join(' · ') || 'Book'}
                </span>
              </div>
              {b.libraryId ? (
                <span className="state-tag st-downloaded" title="Already in your library">
                  <Icon name="check" size={12} /> In library
                </span>
              ) : (
                <button
                  className="primary btn-sm btn-with-icon"
                  onClick={(e) => {
                    e.stopPropagation()
                    setOpen(false)
                    setAddingBook(b)
                  }}
                >
                  <Icon name="plus" size={14} /> Add
                </button>
              )}
            </div>
          ))}
          {results && results.length > 0 && (
            <button className="searchbox-all" onClick={seeAll}>
              See all results for “{q.trim()}”
              <Icon name="open" size={15} />
            </button>
          )}
        </div>
      )}
      <BookAddDialog target={addingBook} onClose={() => setAddingBook(null)} />
      {addingArtist && (
        <AddMusicDialog
          target={{ kind: 'artist', mbid: addingArtist.mbid, name: addingArtist.name, type: addingArtist.type, country: addingArtist.country, disambiguation: addingArtist.disambiguation }}
          onClose={() => setAddingArtist(null)}
          onAdded={(id) => {
            setAddingArtist(null)
            setQ('')
            navigate(`/music/artist/${id}`)
          }}
        />
      )}
    </div>
  )
}
