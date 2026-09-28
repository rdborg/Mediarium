import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, type TitleResult } from '../api'
import AddDialog, { type AddTarget } from './AddDialog'
import Icon from './Icon'
import { PosterFallback } from './PosterCard'
import { resultState } from './state'

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
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setResults(null)
      setError('')
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
  }, [q])

  // Close when the page changes or you click elsewhere.
  useEffect(() => setOpen(false), [location.pathname])
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [])

  const openItem = (r: TitleResult) => {
    setOpen(false)
    navigate(r.kind === 'movie' ? `/title/${r.tmdbId}` : `/series/${r.libraryId}`)
  }
  const choose = (r: TitleResult) => {
    if (r.inLibrary) return openItem(r)
    setOpen(false)
    setAdding({ kind: r.kind, tmdbId: r.tmdbId, title: r.title, year: r.year, posterUrl: r.posterUrl, overview: r.overview })
  }
  const seeAll = () => {
    setOpen(false)
    if (q.trim()) navigate(`/search?q=${encodeURIComponent(q.trim())}`)
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    const count = results?.length ?? 0
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setOpen(true)
      setActive((a) => (a + 1 >= count ? -1 : a + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => (a < 0 ? count - 1 : a - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (active >= 0 && results?.[active]) choose(results[active])
      else seeAll()
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  const showPanel = open && q.trim().length >= 2

  function Row({ r, i }: { r: TitleResult; i: number }) {
    const st = resultState(r)
    return (
      <div
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

  return (
    <div className="searchbox" ref={box}>
      <span className="searchbox-icon" aria-hidden="true">
        <Icon name="search" size={17} />
      </span>
      <input
        type="search"
        role="combobox"
        aria-expanded={showPanel}
        aria-controls="searchbox-results"
        placeholder="Search movies and TV shows"
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
          {results && results.length === 0 && <div className="searchbox-empty">Nothing found for “{q.trim()}”.</div>}
          {results && results.some((r) => r.inLibrary) && <div className="searchbox-group">In your library</div>}
          {results?.map((r, i) => r.inLibrary && <Row key={`${r.kind}-${r.tmdbId}`} r={r} i={i} />)}
          {results && results.some((r) => !r.inLibrary) && <div className="searchbox-group">Add new</div>}
          {results?.map((r, i) => !r.inLibrary && <Row key={`${r.kind}-${r.tmdbId}`} r={r} i={i} />)}
          {results && results.length > 0 && (
            <button className="searchbox-all" onClick={seeAll}>
              See all results for “{q.trim()}”
              <Icon name="open" size={15} />
            </button>
          )}
        </div>
      )}
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            const t = adding
            setAdding(null)
            setQ('')
            navigate(t.kind === 'movie' ? `/title/${t.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </div>
  )
}
