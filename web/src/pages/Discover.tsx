import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError, type DiscoverMovie, type MusicType } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import type { AddMusicTarget } from '../components/AddMusicDialog'
import DiscoverRail from '../components/DiscoverRail'
import { MusicAddDialog, MusicBody } from '../components/MusicDiscover'
import { MUSIC_GENRES, MUSIC_TYPE_OPTIONS } from '../components/MusicRails'
import { KIND_LABEL, useKinds, type MediaKind } from '../ModulesContext'
import Dropdown from '../components/Dropdown'
import PagedGrid from '../components/PagedGrid'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { DISCOVER_KINDS, SHARED_ORDERS } from '../discoverKinds'
import { sourceTitle, sourceToQuery, type Kind, type ListName, type Source } from '../discoverSources'
import { useOwned, type Owned } from '../useOwned'
import { useIncludeOlder } from '../useIncludeOlder'
import { useHideOwned } from '../useHideOwned'
import { firstError, minMax, required, url } from '../validate'
import { FieldError, FormProblem, useValidation } from '../useValidation'

const LIST_EXAMPLE = 'https://trakt.tv/users/name/lists/my-list'

// A public Trakt list address: the trakt.tv site, then /users/<name>/lists/<list>.
function traktList(value: string): string | null {
  const v = value.trim()
  if (!v) return null
  const bad = `That isn't the address of a Trakt list. It should look like ${LIST_EXAMPLE}.`
  const web = url(v, { example: LIST_EXAMPLE })
  if (web) return web
  let u: URL
  try {
    u = new URL(/^https?:\/\//i.test(v) ? v : `https://${v}`)
  } catch {
    return bad
  }
  if (!/(^|\.)trakt\.tv$/i.test(u.hostname)) return `That address isn't on trakt.tv. It should look like ${LIST_EXAMPLE}.`
  const parts = u.pathname.split('/').filter(Boolean)
  if (parts.length < 4 || parts[0] !== 'users' || parts[2] !== 'lists') return bad
  return null
}

type Show = 'all' | MediaKind

const LISTS: ListName[] = ['trending', 'popular', 'upcoming']
const LIST_HINT: Record<ListName, string> = { trending: 'this week', popular: 'all-time favourites', upcoming: 'add them now and they download once released', similar: 'based on what is in your library' }

interface Filters {
  genre: string
  genreName?: string
  from: string
  to: string
  sort: string
  musicType: MusicType
}

// One kind of title (movies or shows): "more like your library", then trending,
// popular and coming soon; or, with a filter picked, everything that matches.
function TitleRails({ kind, filters, filtering, includeOlder, setIncludeOlder, owned, onAdd, onClear, short }: { kind: Kind; filters: Filters; filtering: boolean; includeOlder: boolean; setIncludeOlder: (v: boolean) => void; owned: Owned; onAdd: (t: AddTarget) => void; onClear: () => void; short: boolean }) {
  if (filtering) {
    const src: Source = { kind, list: 'browse', genre: filters.genre || undefined, yearFrom: filters.from || undefined, yearTo: filters.to || undefined, sort: filters.sort }
    return <PagedGrid key={sourceToQuery(src)} title={sourceTitle(src, filters.genreName)} source={src} kind={kind} owned={owned} onAdd={onAdd} rows={3} onClear={onClear} />
  }
  const similar: Source = { kind, list: 'similar', older: includeOlder || undefined }
  return (
    <>
      <DiscoverRail
        key={sourceToQuery(similar)}
        title={short ? 'More like your library' : sourceTitle(similar)}
        hint={LIST_HINT.similar}
        source={similar}
        kind={kind}
        owned={owned}
        onAdd={onAdd}
        rows={1}
        moreLabel="Browse more"
        headExtra={<Switch checked={includeOlder} onChange={setIncludeOlder} label="Include older titles" />}
      />
      {LISTS.map((list) => {
        const src: Source = { kind, list }
        return <DiscoverRail key={sourceToQuery(src)} title={sourceTitle(src)} hint={LIST_HINT[list]} source={src} kind={kind} owned={owned} onAdd={onAdd} rows={2} />
      })}
    </>
  )
}

// On the All tab every kind of media gets its own section, so it is clear
// where one group ends and the next begins.
function Section({ kind, children }: { kind: MediaKind; children: ReactNode }) {
  const k = DISCOVER_KINDS[kind]
  return (
    <section className="discover-section" style={{ ['--kc' as string]: k.color }} aria-label={k.label}>
      <div className="section-divider">
        <span className="section-pill">
          <Icon name={k.icon} size={16} /> {k.label}
        </span>
      </div>
      {children}
    </section>
  )
}

// Discover: things worth adding. The All tab has a section for each kind of
// media that is switched on, each with its own rails (trending, popular,
// coming soon and more like your library); a tab of its own shows just that
// kind. The same filter row serves every tab; picking a filter switches to a
// browse of everything matching. Every rail shows whole rows and has a
// "Browse all" for paging through the full list.
export default function Discover() {
  const owned = useOwned()
  const [includeOlder, setIncludeOlder] = useIncludeOlder()
  const [hideOwned, setHideOwned] = useHideOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [addingMusic, setAddingMusic] = useState<AddMusicTarget | null>(null)

  const [params] = useSearchParams()
  const [picked, setPicked] = useState<Show>(() => {
    const asked = params.get('kind')
    return asked === 'music' || asked === 'movie' || asked === 'tv' ? asked : 'all'
  })
  // Only the kinds of media that are switched on get a tab, and a section on
  // the All tab.
  const kindsOn = useKinds()
  const tabs: Show[] = ['all', ...kindsOn]
  const show: Show = tabs.includes(picked) ? picked : tabs[0]
  useEffect(() => {
    const asked = params.get('kind')
    if (asked === 'music' || asked === 'movie' || asked === 'tv') setPicked(asked)
  }, [params])
  const [genre, setGenre] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [sort, setSort] = useState('popular')
  const [musicType, setMusicType] = useState<MusicType>('all')
  const [genres, setGenres] = useState<{ id: number; name: string }[]>([])
  const [browseMissing, setBrowseMissing] = useState(false)
  const filtering = !!(genre || from || to || sort !== 'popular' || (show === 'music' && musicType !== 'all'))

  const [showImport, setShowImport] = useState(false)
  const [listUrl, setListUrl] = useState('')
  const [importedFrom, setImportedFrom] = useState('')
  const [imported, setImported] = useState<DiscoverMovie[] | undefined>()
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  const v = useValidation({
    listUrl: firstError(required(listUrl, 'Paste the address of a Trakt list, for example trakt.tv/users/name/lists/my-list.'), traktList(listUrl)),
  })
  const yearProblem = minMax(from, to, "The 'from' year can't be later than the 'to' year. Swap them or pick a different year.")

  // Movie and show genres come from the movie database and differ from each
  // other; music has its own list. The All tab has no genre filter.
  useEffect(() => {
    setGenre('')
    setMusicType('all')
    // The order picked on one tab may not exist on another (music has no "highest rated").
    setSort((cur) => (cur === 'rating' && show !== 'movie' && show !== 'tv' ? 'popular' : cur))
    if (show === 'all' || show === 'music') return setGenres([])
    // Movie genres must never end up in the TV list (or the other way round) if the answers arrive out of order.
    let stale = false
    setGenres([])
    api
      .discoverGenres(show)
      .then((g) => !stale && setGenres(g))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) setBrowseMissing(true)
      })
    return () => {
      stale = true
    }
  }, [show])

  const years = useMemo(() => Array.from({ length: new Date().getFullYear() + 2 - 1920 }, (_, i) => String(new Date().getFullYear() + 1 - i)), [])
  const genreOptions = show === 'music' ? MUSIC_GENRES.map((g) => ({ value: g, label: g })) : genres.map((g) => ({ value: String(g.id), label: g.name }))
  const orders = show === 'all' ? SHARED_ORDERS : DISCOVER_KINDS[show].orders
  const filters: Filters = { genre, genreName: genres.find((g) => String(g.id) === genre)?.name, from, to, sort, musicType }
  const musicFilters = { type: musicType, genre, yearFrom: from, yearTo: to, sort }

  async function onImport() {
    if (!v.attempt()) return
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
    setMusicType('all')
  }

  const switcher = (
    <div className="seg">
      {tabs.map((k) => (
        <button key={k} className={show === k ? 'active' : ''} onClick={() => setPicked(k)}>
          {k === 'all' ? 'All' : KIND_LABEL[k]}
        </button>
      ))}
    </div>
  )

  // The rails of one kind, for its own tab or for its section on the All tab.
  function body(kind: MediaKind, short: boolean) {
    if (kind === 'music') return <MusicBody filters={musicFilters} filtering={filtering} onAdd={setAddingMusic} onClear={clear} />
    return <TitleRails kind={kind} filters={filters} filtering={filtering} includeOlder={includeOlder} setIncludeOlder={setIncludeOlder} owned={owned} onAdd={setAdding} onClear={clear} short={short} />
  }

  const trakt = show !== 'music'

  return (
    <div>
      <div className="toolbar filter-bar">
        {switcher}
        <Dropdown
          label="Genre"
          value={genre}
          onChange={setGenre}
          disabled={(show !== 'music' && browseMissing) || show === 'all'}
          title={show === 'all' ? 'Pick a kind of media to filter by genre' : undefined}
          options={[{ value: '', label: show === 'all' ? 'Genre: pick a kind' : 'All genres' }, ...genreOptions]}
        />
        <Dropdown label="From year" value={from} onChange={setFrom} disabled={show !== 'music' && browseMissing} options={[{ value: '', label: 'From any year' }, ...years.map((y) => ({ value: y, label: `From ${y}` }))]} />
        <Dropdown label="To year" value={to} onChange={setTo} disabled={show !== 'music' && browseMissing} options={[{ value: '', label: 'To any year' }, ...years.map((y) => ({ value: y, label: `To ${y}` }))]} />
        <Dropdown label="Order" value={sort} onChange={setSort} disabled={show !== 'music' && browseMissing} options={orders} />
        {show === 'music' && <Dropdown label="Type" value={musicType} onChange={(t) => setMusicType(t as MusicType)} options={MUSIC_TYPE_OPTIONS} />}
        {filtering && (
          <button className="btn-sm" onClick={clear}>
            Clear filters
          </button>
        )}
        <Switch checked={hideOwned} onChange={setHideOwned} label="Hide what I have" />
        <span className="spacer" />
        {trakt && (
          <button className="btn-with-icon" onClick={() => setShowImport((v) => !v)}>
            <Icon name="list" size={16} /> {showImport ? 'Close list import' : 'Import a Trakt list'}
          </button>
        )}
      </div>
      {yearProblem && (
        <p className="field-error" role="alert" style={{ margin: '0 0 14px' }}>
          {yearProblem}
        </p>
      )}

      {trakt && showImport && (
        <section className="card grid-form" style={{ marginBottom: 24 }}>
          <h2>Import a Trakt list</h2>
          <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: 0 }}>
            Paste the address of any public Trakt list to see everything on it here. Trakt is a free site where people keep lists
            of what they watch. See <Link to="/settings/metadata">Settings &gt; Info, lists and subtitles &gt; Movie info and lists</Link>.
          </p>
          <input value={listUrl} onChange={(e) => setListUrl(e.target.value)} placeholder="trakt.tv/users/…/lists/…" aria-label="Trakt list address" {...v.bind('listUrl', listUrl, setListUrl)} />
          <FieldError v={v} name="listUrl" />
          <div>
            <button className="primary" disabled={importing} onClick={onImport}>
              {importing ? 'Importing…' : 'Import'}
            </button>
          </div>
          <FormProblem v={v} verb="import" />
          {importError && <p className="error-text" style={{ margin: 0 }}>{importError}</p>}
        </section>
      )}

      {trakt && imported && <DiscoverRail title={`Imported from ${importedFrom}`} items={imported} kind="movie" owned={owned} onAdd={setAdding} rows={3} />}

      {show === 'all'
        ? kindsOn.map((kind) => (
            <Section key={kind} kind={kind}>
              {body(kind, false)}
            </Section>
          ))
        : body(show, true)}

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
      <MusicAddDialog target={addingMusic} onClose={() => setAddingMusic(null)} />
    </div>
  )
}
