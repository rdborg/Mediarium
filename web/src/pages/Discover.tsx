import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type DiscoverMovie } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import Icon from '../components/Icon'
import PosterCard from '../components/PosterCard'
import { movieState, seriesState, type ItemStateInput } from '../components/state'

type Kind = 'movie' | 'tv'

// Discover: things worth adding. Every poster has an Add button that opens the
// same "add to library" dialog as search (quality, monitoring, where to
// download from), and titles you already own say so instead of offering Add.
export default function Discover() {
  const navigate = useNavigate()
  const [trending, setTrending] = useState<DiscoverMovie[] | null>(null)
  const [popular, setPopular] = useState<DiscoverMovie[]>([])
  const [forYou, setForYou] = useState<DiscoverMovie[]>([])
  const [trendingTV, setTrendingTV] = useState<DiscoverMovie[]>([])
  const [popularTV, setPopularTV] = useState<DiscoverMovie[]>([])
  const [ownedMovies, setOwnedMovies] = useState<Map<number, ItemStateInput>>(new Map())
  const [ownedShows, setOwnedShows] = useState<Map<number, ItemStateInput>>(new Map())
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<AddTarget | null>(null)

  const [listUrl, setListUrl] = useState('')
  const [importedFrom, setImportedFrom] = useState('')
  const [imported, setImported] = useState<DiscoverMovie[] | null>(null)
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  const [showImport, setShowImport] = useState(false)

  const loadOwned = useCallback(() => {
    api.listQueue().catch(() => []).then((queue) => {
      api.listMovies().then((m) => setOwnedMovies(new Map(m.map((x) => [x.tmdbId, { id: x.id, state: movieState(x, queue) }])))).catch(() => undefined)
      api.listSeries().then((s) => setOwnedShows(new Map(s.map((x) => [x.tmdbId, { id: x.id, state: seriesState(x, queue) }])))).catch(() => undefined)
    })
  }, [])

  useEffect(() => {
    loadOwned()
    Promise.all([api.discoverTrending(), api.discoverPopular(), api.discoverForYou()])
      .then(([t, p, f]) => {
        setTrending(t)
        setPopular(p)
        setForYou(f)
      })
      .catch((e) => {
        setTrending([])
        setError(e instanceof Error ? e.message : String(e))
      })
    // TV rails load independently so a TV failure never blanks the movie rails.
    Promise.all([api.discoverTrendingTV(), api.discoverPopularTV()])
      .then(([t, p]) => {
        setTrendingTV(t)
        setPopularTV(p)
      })
      .catch(() => undefined)
  }, [loadOwned])

  async function onImport() {
    setImporting(true)
    setImportError('')
    try {
      setImported(await api.importList(listUrl))
      setImportedFrom(listUrl)
    } catch (e) {
      setImportError(e instanceof Error ? e.message : String(e))
    } finally {
      setImporting(false)
    }
  }

  function Rail({ title, hint, list, kind }: { title: string; hint?: string; list: DiscoverMovie[]; kind: Kind }) {
    if (list.length === 0) return null
    const owned = kind === 'movie' ? ownedMovies : ownedShows
    return (
      <section style={{ marginBottom: 34 }}>
        <div className="rail-head">
          <h2>{title}</h2>
          {hint && <span>{hint}</span>}
        </div>
        <div className="poster-grid">
          {list.map((m) => {
            const own = owned.get(m.tmdbId)
            const libraryId = own?.id
            const path = kind === 'movie' ? `/title/${m.tmdbId}` : libraryId ? `/series/${libraryId}` : `/show/${m.tmdbId}`
            return (
              <PosterCard
                key={m.tmdbId}
                to={path}
                poster={m.posterUrl}
                title={m.title}
                meta={m.year || undefined}
                genres={m.genres}
                rating={m.rating}
                kind={kind}
                state={own?.state}
                footer={
                  <div className="pcard-footer">
                    {libraryId ? (
                      <button className="btn-sm btn-with-icon" onClick={() => navigate(path!)}>
                        <Icon name="open" size={15} /> Open
                      </button>
                    ) : (
                      <button
                        className="primary btn-sm btn-with-icon"
                        onClick={() => setAdding({ kind, tmdbId: m.tmdbId, title: m.title, year: m.year, posterUrl: m.posterUrl, overview: m.overview })}
                      >
                        <Icon name="plus" size={15} /> Add
                      </button>
                    )}
                  </div>
                }
              />
            )
          })}
        </div>
      </section>
    )
  }

  const nothing = trending !== null && trending.length === 0 && popular.length === 0

  return (
    <div>
      <div className="page-header">
        <h1>Discover</h1>
        <button className="btn-with-icon" onClick={() => setShowImport((v) => !v)}>
          <Icon name="list" size={16} /> {showImport ? 'Close list import' : 'Import a Trakt list'}
        </button>
      </div>

      {error && <p className="error-text">{error}</p>}

      {showImport && (
        <section className="card grid-form" style={{ marginBottom: 24 }}>
          <h2>Import a Trakt list</h2>
          <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: 0 }}>
            Paste the address of any public Trakt list to see everything on it here. Trakt is a free site where people keep lists
            of what they watch. This needs a Trakt client ID under <Link to="/settings/metadata">Settings &gt; Metadata</Link>.
          </p>
          <input value={listUrl} onChange={(e) => setListUrl(e.target.value)} placeholder="trakt.tv/users/…/lists/…" />
          <div>
            <button className="primary" disabled={!listUrl || importing} onClick={onImport}>
              {importing ? 'Importing…' : 'Import'}
            </button>
          </div>
          {importError && <p className="error-text" style={{ margin: 0 }}>{importError}</p>}
        </section>
      )}

      {imported !== null && <Rail title={`Imported from ${importedFrom}`} list={imported} kind="movie" />}

      {trending === null && (
        <div className="poster-grid">
          {Array.from({ length: 14 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />
          ))}
        </div>
      )}

      {nothing && (
        <div className="empty-state">
          <Icon name="compass" size={44} />
          <p>
            Nothing to show yet. Add a TMDB API key under <Link to="/settings/metadata">Settings &gt; Metadata</Link> to switch Discover on.
          </p>
        </div>
      )}

      <Rail title="More like your library" hint="based on what you own" list={forYou} kind="movie" />
      <Rail title="Trending movies" hint="this week" list={trending ?? []} kind="movie" />
      <Rail title="Trending shows" hint="this week" list={trendingTV} kind="tv" />
      <Rail title="Popular movies" list={popular} kind="movie" />
      <Rail title="Popular shows" list={popularTV} kind="tv" />

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            const t = adding
            setAdding(null)
            loadOwned()
            navigate(t.kind === 'movie' ? `/title/${t.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </div>
  )
}
