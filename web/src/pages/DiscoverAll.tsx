import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import Icon from '../components/Icon'
import PagedGrid from '../components/PagedGrid'
import Switch from '../components/Switch'
import { KIND_LABEL, useKinds } from '../ModulesContext'
import { sourceFromQuery, sourceTitle, sourceToQuery } from '../discoverSources'
import { useOwned } from '../useOwned'
import { useIncludeOlder } from '../useIncludeOlder'
import { useHideOwned } from '../useHideOwned'

// One Discover list on its own page, split into pages you can step through
// (or extend with Load more).
export default function DiscoverAll() {
  const [params, setParams] = useSearchParams()
  const [includeOlder, setIncludeOlder] = useIncludeOlder()
  const [hideOwned, setHideOwned] = useHideOwned()
  const kindsOn = useKinds().filter((k) => k !== 'music')
  const parsed = useMemo(() => sourceFromQuery(params), [params])
  const similar = parsed.list === 'similar'
  // "More like your library" follows the switch for older titles, which is
  // remembered in this browser, not the address.
  const source = useMemo(() => (similar ? { ...parsed, older: includeOlder || undefined } : parsed), [parsed, similar, includeOlder])
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
        <Switch checked={hideOwned} onChange={setHideOwned} label="Hide what I have" />
        {similar && (
          <>
            {kindsOn.length > 1 && (
              <div className="seg">
                {kindsOn.map((k) => (
                  <button key={k} className={source.kind === k ? 'active' : ''} onClick={() => setParams({ list: 'similar', kind: k }, { replace: true })}>
                    {KIND_LABEL[k]}
                  </button>
                ))}
              </div>
            )}
            <span className="spacer" />
            <Switch checked={includeOlder} onChange={setIncludeOlder} label="Include older titles" />
          </>
        )}
      </div>

      <PagedGrid key={sourceToQuery(source)} source={source} kind={source.kind} owned={owned} onAdd={setAdding} rows={4} />

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            // Stay here so you can keep browsing: the card switches to "In library" by itself.
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}
