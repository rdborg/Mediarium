import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import Icon from '../components/Icon'
import PagedGrid from '../components/PagedGrid'
import { sourceFromQuery, sourceTitle, sourceToQuery } from '../discoverSources'
import { useOwned } from '../useOwned'

// One Discover list on its own page, split into pages you can step through
// (or extend with Load more).
export default function DiscoverAll() {
  const [params] = useSearchParams()
  const source = useMemo(() => sourceFromQuery(params), [params])
  const navigate = useNavigate()
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [genreName, setGenreName] = useState<string | undefined>()

  useEffect(() => {
    if (!source.genre) return setGenreName(undefined)
    api
      .discoverGenres(source.kind)
      .then((g) => setGenreName(g.find((x) => String(x.id) === source.genre)?.name))
      .catch(() => undefined)
  }, [source])

  return (
    <div>
      <div className="toolbar filter-bar">
        <Link to="/discover" className="btn btn-with-icon">
          <Icon name="chevron-left" size={16} /> Discover
        </Link>
        <h2 style={{ margin: 0 }}>{sourceTitle(source, genreName)}</h2>
      </div>

      <PagedGrid key={sourceToQuery(source)} source={source} kind={source.kind} owned={owned} onAdd={setAdding} rows={4} />

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
