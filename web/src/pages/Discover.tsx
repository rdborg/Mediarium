import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, ApiError, type DiscoverMovie } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import Dropdown from '../components/Dropdown'
import PagedGrid from '../components/PagedGrid'
import Icon from '../components/Icon'
import { sourceTitle, sourceToQuery, type Kind, type ListName, type Source } from '../discoverSources'
import { useOwned } from '../useOwned'

type Show = 'all' | Kind

const LISTS: ListName[] = ['trending', 'popular', 'upcoming']
const LIST_HINT: Record<ListName, string> = { trending: 'this week', popular: 'all time favourites', upcoming: 'add them now, they download when out' }

// Discover: things worth adding. Movies first, then shows, each with
// trending, popular and coming soon; filters switch to a browse of everything
// matching. Every section shows whole rows and has "Show all" for paging
// through the full list.
export default function Discover() {
  const navigate = useNavigate()
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [forYou, setForYou] = useState<DiscoverMovie[] | undefined>()

  const [show, setShow] = useState<Show>('all')
  const [genre, setGenre] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [sort, setSort] = useState('popular')
  const [genres, setGenres] = useState<{ id: number; name: string }[]>([])
  const [browseMissing, setBrowseMissing] = useState(false)
  const filtering = !!(genre || from || to || sort !== 'popular')

  const [showImport, setShowImport] = useState(false)
  const [listUrl, setListUrl] = useState('')
  const [importedFrom, setImportedFrom] = useState('')
  const [imported, setImported] = useState<DiscoverMovie[] | undefined>()
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')

  useEffect(() => {
    api.discoverForYou().then(setForYou).catch(() => setForYou([]))
  }, [])

  // Genres differ between movies and shows, so the genre filter needs one of them.
  useEffect(() => {
    setGenre('')
    if (show === 'all') return setGenres([])
    api
      .discoverGenres(show)
      .then(setGenres)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) setBrowseMissing(true)
      })
  }, [show])

  const kinds: Kind[] = show === 'all' ? ['movie', 'tv'] : [show]
  const years = useMemo(() => Array.from({ length: new Date().getFullYear() + 2 - 1920 }, (_, i) => String(new Date().getFullYear() + 1 - i)), [])

  const rails = useMemo(() => {
    const out: { source: Source; title: string; hint?: string }[] = []
    for (const kind of kinds) {
      if (filtering) {
        const src: Source = { kind, list: 'browse', genre: genre || undefined, yearFrom: from || undefined, yearTo: to || undefined, sort }
        out.push({ source: src, title: sourceTitle(src, genres.find((g) => String(g.id) === genre)?.name) })
      } else {
        for (const list of LISTS) out.push({ source: { kind, list }, title: sourceTitle({ kind, list }), hint: LIST_HINT[list] })
      }
    }
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, filtering, genre, from, to, sort, genres])

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

  function clear() {
    setGenre('')
    setFrom('')
    setTo('')
    setSort('popular')
  }

  return (
    <div>
      <div className="toolbar filter-bar">
        <div className="seg">
          {(['all', 'movie', 'tv'] as const).map((k) => (
            <button key={k} className={show === k ? 'active' : ''} onClick={() => setShow(k)}>
              {k === 'all' ? 'All' : k === 'movie' ? 'Movies' : 'TV shows'}
            </button>
          ))}
        </div>
        <Dropdown
          label="Genre"
          value={genre}
          onChange={setGenre}
          disabled={browseMissing || show === 'all'}
          title={show === 'all' ? 'Pick Movies or TV shows to filter by genre' : undefined}
          options={[{ value: '', label: show === 'all' ? 'Genre: pick Movies or TV' : 'All genres' }, ...genres.map((g) => ({ value: String(g.id), label: g.name }))]}
        />
        <Dropdown label="From year" value={from} onChange={setFrom} disabled={browseMissing} options={[{ value: '', label: 'From any year' }, ...years.map((y) => ({ value: y, label: `From ${y}` }))]} />
        <Dropdown label="To year" value={to} onChange={setTo} disabled={browseMissing} options={[{ value: '', label: 'To any year' }, ...years.map((y) => ({ value: y, label: `To ${y}` }))]} />
        <Dropdown
          label="Order"
          value={sort}
          onChange={setSort}
          disabled={browseMissing}
          options={[
            { value: 'popular', label: 'Most popular' },
            { value: 'rating', label: 'Highest rated' },
            { value: 'newest', label: 'Newest first' },
            { value: 'oldest', label: 'Oldest first' },
          ]}
        />
        {filtering && (
          <button className="btn-sm" onClick={clear}>
            Clear
          </button>
        )}
        <span className="spacer" />
        <button className="btn-with-icon" onClick={() => setShowImport((v) => !v)}>
          <Icon name="list" size={16} /> {showImport ? 'Close list import' : 'Import a Trakt list'}
        </button>
      </div>

      {showImport && (
        <section className="card grid-form" style={{ marginBottom: 24 }}>
          <h2>Import a Trakt list</h2>
          <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: 0 }}>
            Paste the address of any public Trakt list to see everything on it here. Trakt is a free site where people keep lists
            of what they watch. See <Link to="/settings/metadata">Settings &gt; Lists &amp; Subtitles</Link>.
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

      {imported && <DiscoverRail title={`Imported from ${importedFrom}`} items={imported} kind="movie" owned={owned} onAdd={setAdding} rows={3} />}

      {!filtering && show !== 'tv' && forYou && forYou.length > 0 && (
        <DiscoverRail title="More like your library" hint="based on what you own" items={forYou} kind="movie" owned={owned} onAdd={setAdding} rows={1} />
      )}

      {rails.map((r) =>
        filtering ? (
          <PagedGrid key={sourceToQuery(r.source)} title={r.title} source={r.source} kind={r.source.kind} owned={owned} onAdd={setAdding} rows={3} />
        ) : (
          <DiscoverRail key={sourceToQuery(r.source)} title={r.title} hint={r.hint} source={r.source} kind={r.source.kind} owned={owned} onAdd={setAdding} rows={2} />
        ),
      )}

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            const t = adding
            setAdding(null)
            owned.reload()
            navigate(t.kind === 'movie' ? `/title/${t.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </div>
  )
}
