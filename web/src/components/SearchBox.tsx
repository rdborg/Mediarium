import { lazy, Suspense, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, isAdmin, isViewer, type BookFound, type MusicArtistResult, type TitleResult } from '../api'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { artistKind } from './artistKind'
import { libraryFirst } from '../searchOrder'
import Icon from './Icon'
import { MusicFallback, PosterFallback } from './PosterCard'
import { resultState } from './state'
import { isTypingTarget } from '../shortcuts'
import { buildSettingsIndex, searchSettings, type SettingsEntry } from '../settingsIndex'
import { effectiveTab, planSettings, readSearchTab, rememberSearchTab, type SearchTab } from '../searchGroups'
import { t } from '../i18n'

// The two add dialogs are fetched when one is opened, not with every page.
const AddMusicDialog = lazy(() => import('./AddMusicDialog'))
const BookAddDialog = lazy(() => import('./BookDiscover').then((m) => ({ default: m.BookAddDialog })))

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
  const noKeyMessage = t('Searching movies and shows needs a TMDB API key. An administrator can add one under Settings > Library and files > Movie and show info.')
  const [results, setResults] = useState<TitleResult[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const box = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const { on, tmdbOn } = useModules()
  const { user } = useAuth()
  const musicOn = on('music')
  const titlesOn = on('movies') || on('tv')
  const [artists, setArtists] = useState<MusicArtistResult[] | null>(null)
  const [addingArtist, setAddingArtist] = useState<MusicArtistResult | null>(null)
  const booksOn = on('ebooks') || on('audiobooks')
  const [bookHits, setBookHits] = useState<BookFound[] | null>(null)
  const [bookError, setBookError] = useState('')
  const [addingBook, setAddingBook] = useState<BookFound | null>(null)
  // All | Media | Settings: the last one used is kept in this browser.
  const [tab, setTab] = useState<SearchTab>(() => readSearchTab())
  const [allSettings, setAllSettings] = useState(false)

  // A new search starts with the short list of settings again.
  useEffect(() => setAllSettings(false), [q])

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
    if (!tmdbOn) {
      // No TMDB key: the movie and show search can't answer, so say so instead of asking.
      setResults([])
      setError(noKeyMessage)
      setLoading(false)
      return
    }
    setLoading(true)
    const ctl = new AbortController()
    const t = setTimeout(() => {
      api
        .discoverSearch(term, ctl.signal)
        .then((r) => {
          // Library titles are drawn first, so the arrow keys step in that order too.
          setResults(libraryFirst(r.slice(0, 8)))
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
  }, [q, titlesOn, tmdbOn, noKeyMessage])

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
      setBookError('')
      return
    }
    let stale = false
    const t = setTimeout(() => {
      api
        .searchBooks(term)
        .then((r) => {
          if (stale) return
          setBookHits(r.slice(0, 5))
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

  // Settings pages and settings: found in the browser, so they show at once.
  const admin = isAdmin(user)
  const viewer = isViewer(user)
  const modKey = (['movies', 'tv', 'music', 'ebooks', 'audiobooks'] as const).map((m) => (on(m) ? 1 : 0)).join('')
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const settingsIndex = useMemo(() => buildSettingsIndex(admin, viewer, (m) => on(m)), [admin, viewer, modKey])
  const hits = useMemo(() => (q.trim().length >= 2 ? searchSettings(q, settingsIndex) : []), [q, settingsIndex])

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
  const chooseSetting = (e: SettingsEntry) => {
    setOpen(false)
    navigate(e.to, { state: e.anchor ? { find: e.anchor } : undefined })
  }
  const seeAll = () => {
    setOpen(false)
    if (q.trim()) navigate(`/search?q=${encodeURIComponent(q.trim())}`)
  }

  // The rows run in one list for the arrow keys. A settings match that is
  // what you typed (or starts with it) leads the list; a looser one comes
  // last, so it never pushes the titles down. Either way the All tab lists
  // three settings at most; the Media tab none and the Settings tab only them.
  const showPanel = open && q.trim().length >= 2
  const nHits = hits.length
  const shownTab = effectiveTab(tab, nHits)
  const plan = planSettings(nHits, hits[0]?.score ?? 0, shownTab, allSettings)
  const titleList = plan.media ? results : null
  const artistList = plan.media ? artists : null
  const bookList = plan.media ? bookHits : null
  const nSettings = plan.show ? plan.shown : 0
  const nSettingRows = nSettings + (plan.show && plan.more > 0 ? 1 : 0) // the "Show all" row is one more
  const settingsFirst = plan.show && plan.lead
  const lead = settingsFirst ? nSettingRows : 0
  const titleCount = titleList?.length ?? 0
  const titleEnd = lead + titleCount
  const artistEnd = titleEnd + (artistList?.length ?? 0)
  const mediaEnd = artistEnd + (bookList?.length ?? 0)
  const settingsStart = settingsFirst ? 0 : mediaEnd
  const count = mediaEnd + nSettingRows
  const nothingFound = !plan.show && results !== null && results.length === 0 && (!musicOn || (artists !== null && artists.length === 0)) && (!booksOn || (bookHits !== null && bookHits.length === 0)) && !bookError

  function pickTab(next: SearchTab) {
    setTab(next)
    rememberSearchTab(next)
    setActive(-1)
    panel.current?.scrollTo({ top: 0 })
    input.current?.focus()
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setOpen(true)
      setActive((a) => (a + 1 >= count ? -1 : a + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => (a < 0 ? count - 1 : a - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (active >= settingsStart && active < settingsStart + nSettings) chooseSetting(hits[active - settingsStart].entry)
      else if (plan.show && plan.more > 0 && active === settingsStart + nSettings) setAllSettings(true)
      else if (active >= lead && active < titleEnd && titleList?.[active - lead]) choose(titleList[active - lead])
      else if (active >= titleEnd && active < artistEnd && artistList?.[active - titleEnd]) chooseArtist(artistList[active - titleEnd])
      else if (active >= artistEnd && active < mediaEnd && bookList?.[active - artistEnd]) chooseBook(bookList[active - artistEnd])
      else if (settingsFirst && (!plan.media || (results !== null && results.length === 0))) chooseSetting(hits[0].entry)
      else seeAll()
    } else if (e.key === 'Escape') {
      // Escape closes the list; pressed again (nothing left to close) it leaves the box.
      if (showPanel) setOpen(false)
      else e.currentTarget.blur()
    }
  }

  // A plain function, not a component: a component made inside another one is a
  // new type on every render, which would rebuild every row each time the mouse moves.
  function row(r: TitleResult, i: number) {
    const st = resultState(r)
    return (
      <div
        key={`${r.kind}-${r.tmdbId}`}
        id={`searchbox-opt-${i}`}
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
            <i className={`kind-dot kind-${r.kind}`} /> {r.kind === 'tv' ? t('TV show') : t('Movie')} · {r.year || '—'}
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
            <Icon name="plus" size={14} /> {t('Add')}
          </button>
        )}
      </div>
    )
  }

  function settingsRows() {
    return (
      <>
        <div className="searchbox-group">{t('Settings')}</div>
        {hits.slice(0, nSettings).map(({ entry }, i) => {
          const idx = settingsStart + i
          return (
            <div
              key={`${entry.to}-${entry.anchor ?? ''}`}
              id={`searchbox-opt-${idx}`}
              className={`searchbox-row${idx === active ? ' active' : ''}`}
              role="option"
              aria-selected={idx === active}
              onMouseEnter={() => setActive(idx)}
              onClick={() => chooseSetting(entry)}
            >
              <span className="searchbox-settings-icon" aria-hidden="true">
                <Icon name="sliders" size={18} />
              </span>
              <div className="searchbox-text">
                <strong>{entry.label}</strong>
                <span className="searchbox-hit-trail">{entry.trail}</span>
              </div>
            </div>
          )
        })}
        {plan.more > 0 && (
          <div
            id={`searchbox-opt-${settingsStart + nSettings}`}
            className={`searchbox-row searchbox-more${settingsStart + nSettings === active ? ' active' : ''}`}
            role="option"
            aria-selected={settingsStart + nSettings === active}
            onMouseEnter={() => setActive(settingsStart + nSettings)}
            onClick={() => setAllSettings(true)}
          >
            {t('Show all {n} settings matches', { n: nHits })}
          </div>
        )}
      </>
    )
  }

  const tabLabels: [SearchTab, string][] = [
    ['all', t('All')],
    ['media', t('Media')],
    ['settings', t('Settings')],
  ]

  const kinds = [on('movies') && t('movies'), on('tv') && (on('movies') ? t('shows') : t('TV shows')), musicOn && t('artists'), booksOn && t('books')].filter((k): k is string => !!k)
  const placeholder = t('Search {what}', {
    what: kinds.length > 1 ? t('{list} and {last}', { list: kinds.slice(0, -1).join(', '), last: kinds[kinds.length - 1] }) : kinds[0] || 'Mediarium',
  })

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
        title={t('Press / to search')}
        role="combobox"
        aria-expanded={showPanel}
        aria-controls="searchbox-results"
        aria-activedescendant={showPanel && active >= 0 ? `searchbox-opt-${active}` : undefined}
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
        <div className="searchbox-panel" ref={panel}>
          {nHits > 0 && (
            <div className="searchbox-tabbar">
              <div className="seg" role="tablist" aria-label={t('Narrow the search results')}>
                {tabLabels.map(([k, label]) => (
                  <button key={k} type="button" role="tab" aria-selected={shownTab === k} className={shownTab === k ? 'active' : ''} onMouseDown={(e) => e.preventDefault()} onClick={() => pickTab(k)}>
                    {label}
                    {k === 'settings' && <span className="searchbox-tab-count">{nHits}</span>}
                  </button>
                ))}
              </div>
            </div>
          )}
          <div id="searchbox-results" role="listbox">
          {plan.media && loading && !results && (
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
          {plan.media && error && <div className="searchbox-empty error-text">{error}</div>}
          {plan.media && booksOn && bookError && <div className="searchbox-empty error-text">{bookError}</div>}
          {nothingFound && <div className="searchbox-empty">{t('Nothing found for “{q}”.', { q: q.trim() })}</div>}
          {settingsFirst && settingsRows()}
          {titleList && titleList.some((r) => r.inLibrary) && <div className="searchbox-group">{t('In your library')}</div>}
          {titleList?.map((r, i) => r.inLibrary && row(r, lead + i))}
          {titleList && titleList.some((r) => !r.inLibrary) && <div className="searchbox-group">{t('Add new')}</div>}
          {titleList?.map((r, i) => !r.inLibrary && row(r, lead + i))}
          {musicOn && artistList && artistList.length > 0 && <div className="searchbox-group">{t('Artists')}</div>}
          {artistList?.map((a, i) => (
            <div
              key={a.mbid}
              id={`searchbox-opt-${titleEnd + i}`}
              className={`searchbox-row${titleEnd + i === active ? ' active' : ''}`}
              role="option"
              aria-selected={titleEnd + i === active}
              onMouseEnter={() => setActive(titleEnd + i)}
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
                <span className="state-tag st-downloaded" title={t('Already in your library')}>
                  <Icon name="check" size={12} /> {t('In library')}
                </span>
              ) : (
                <button
                  className="primary btn-sm btn-with-icon"
                  onClick={(e) => {
                    e.stopPropagation()
                    chooseArtist(a)
                  }}
                >
                  <Icon name="plus" size={14} /> {t('Add')}
                </button>
              )}
            </div>
          ))}
          {booksOn && bookList && bookList.length > 0 && <div className="searchbox-group">{t('Books')}</div>}
          {bookList?.map((b, i) => (
            <div
              key={b.key}
              id={`searchbox-opt-${artistEnd + i}`}
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
                  <i className="kind-dot kind-book" /> {[b.author, b.year].filter(Boolean).join(' · ') || t('Book')}
                </span>
              </div>
              {b.libraryId ? (
                <span className="state-tag st-downloaded" title={t('Already in your library')}>
                  <Icon name="check" size={12} /> {t('In library')}
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
                  <Icon name="plus" size={14} /> {t('Add')}
                </button>
              )}
            </div>
          ))}
          {!settingsFirst && plan.show && settingsRows()}
          </div>
          {plan.media && results && results.length > 0 && (
            <button className="searchbox-all" onClick={seeAll}>
              {t('See all results for “{q}”', { q: q.trim() })}
              <Icon name="open" size={15} />
            </button>
          )}
        </div>
      )}
      <Suspense fallback={null}>
        {addingBook && <BookAddDialog target={addingBook} onClose={() => setAddingBook(null)} />}
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
      </Suspense>
    </div>
  )
}
