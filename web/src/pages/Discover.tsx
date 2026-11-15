import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, isRequested, ApiError, type BookFound, type DiscoverMovie, type MusicType } from '../api'
import { PicksProvider, pickKey, usePicks } from '../discoverPicks'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import type { AddMusicTarget } from '../components/AddMusicDialog'
import DiscoverRail from '../components/DiscoverRail'
import { EagerImagesProvider } from '../components/PosterCard'
import { BookAddDialog, BookBody } from '../components/BookDiscover'
import { MusicAddDialog, MusicBody } from '../components/MusicDiscover'
import { MUSIC_GENRES, MUSIC_TYPE_OPTIONS } from '../components/MusicRails'
import { KIND_LABEL, useKinds, useModules, type MediaKind } from '../ModulesContext'
import Collapsible from '../components/Collapsible'
import Dropdown from '../components/Dropdown'
import PagedGrid from '../components/PagedGrid'
import Icon from '../components/Icon'
import KindGlyph from '../components/KindGlyph'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { DISCOVER_KINDS, SHARED_ORDERS } from '../discoverKinds'
import { sourceTitle, sourceToQuery, type Kind, type ListName, type Source } from '../discoverSources'
import { useOwned, type Owned } from '../useOwned'
import { useIncludeOlder } from '../useIncludeOlder'
import { useHideOwned } from '../useHideOwned'
import { firstError, minMax, required, url } from '../validate'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import FilterToggle from '../components/FilterToggle'
import StreamingServicesFilter from '../components/StreamingServicesFilter'
import { useMyServices } from '../streamingServices'
import { useUrlState } from '../useUrlState'
import { activeExtraFilters, extraFiltersSummary, languageOptions, ratingLabel, RATINGS, runtimeLabel, RUNTIMES } from '../discoverFilters'
import { plural, t, tx } from '../i18n'

const LIST_EXAMPLE = 'https://trakt.tv/users/name/lists/my-list'

// A public Trakt list address: the trakt.tv site, then /users/<name>/lists/<list>.
function traktList(value: string): string | null {
  const v = value.trim()
  if (!v) return null
  const bad = t("That isn't the address of a Trakt list. It should look like {example}.", { example: LIST_EXAMPLE })
  const web = url(v, { example: LIST_EXAMPLE })
  if (web) return web
  let u: URL
  try {
    u = new URL(/^https?:\/\//i.test(v) ? v : `https://${v}`)
  } catch {
    return bad
  }
  if (!/(^|\.)trakt\.tv$/i.test(u.hostname)) return t("That address isn't on trakt.tv. It should look like {example}.", { example: LIST_EXAMPLE })
  const parts = u.pathname.split('/').filter(Boolean)
  if (parts.length < 4 || parts[0] !== 'users' || parts[2] !== 'lists') return bad
  return null
}

// A Discover section: a kind of media, or books (ebooks and audiobooks share one).
type Section = MediaKind | 'book'
type Show = 'all' | Section
const isSection = (v: string | null): v is Section => v === 'movie' || v === 'tv' || v === 'music' || v === 'book'

const LISTS: ListName[] = ['trending', 'popular', 'upcoming']
const LIST_HINT: Record<ListName, string> = { trending: t('this week'), popular: t('all-time favourites'), upcoming: t('add them now and they download once released'), similar: t('based on what is in your library') }

interface Filters {
  genre: string
  genreName?: string
  from: string
  to: string
  sort: string
  language: string
  minRating: string
  runtime: string
  // Streaming service keys whose titles are left out; empty while the switch is off.
  hide: string
  musicType: MusicType
}

// One kind of title (movies or shows): "more like your library", then trending,
// popular and coming soon; or, with a filter picked, everything that matches.
function TitleRails({ kind, filters, filtering, includeOlder, setIncludeOlder, owned, onAdd, onClear, short }: { kind: Kind; filters: Filters; filtering: boolean; includeOlder: boolean; setIncludeOlder: (v: boolean) => void; owned: Owned; onAdd: (t: AddTarget) => void; onClear: () => void; short: boolean }) {
  if (filtering) {
    const src: Source = { kind, list: 'browse', genre: filters.genre || undefined, yearFrom: filters.from || undefined, yearTo: filters.to || undefined, sort: filters.sort, language: filters.language || undefined, minRating: filters.minRating || undefined, runtime: filters.runtime || undefined, hide: filters.hide || undefined }
    return <PagedGrid key={sourceToQuery(src)} title={sourceTitle(src, filters.genreName)} source={src} kind={kind} owned={owned} onAdd={onAdd} rows={3} onClear={onClear} />
  }
  const similar: Source = { kind, list: 'similar', older: includeOlder || undefined }
  return (
    <>
      <DiscoverRail
        key={sourceToQuery(similar)}
        title={short ? t('More like your library') : sourceTitle(similar)}
        hint={LIST_HINT.similar}
        source={similar}
        kind={kind}
        owned={owned}
        onAdd={onAdd}
        rows={1}
        moreLabel={t('Browse more')}
        headExtra={<Switch checked={includeOlder} onChange={setIncludeOlder} label={t('Include older titles')} />}
      />
      {LISTS.map((list) => {
        const src: Source = { kind, list, hide: filters.hide || undefined }
        return <DiscoverRail key={sourceToQuery(src)} title={sourceTitle(src)} hint={LIST_HINT[list]} source={src} kind={kind} owned={owned} onAdd={onAdd} rows={2} />
      })}
    </>
  )
}

// On the All tab every kind of media gets its own section, so it is clear
// where one group ends and the next begins.
function SectionBlock({ kind, children }: { kind: Section; children: ReactNode }) {
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
function SelectToggle() {
  const picks = usePicks()
  if (!picks) return null
  return (
    <button
      className={`icon-btn select-btn${picks.selecting ? ' primary active' : ''}`}
      aria-pressed={picks.selecting}
      aria-label={t('Select')}
      title={t('Select several titles to add at once')}
      onClick={() => picks.setSelecting(!picks.selecting)}
    >
      <Icon name="check" size={17} />
    </button>
  )
}

export default function Discover() {
  return (
    <PicksProvider>
      <EagerImagesProvider>
      <DiscoverPage />
      </EagerImagesProvider>
    </PicksProvider>
  )
}

// Discover's Select mode bar: add every picked title at once, with the usual
// defaults (monitored, the default quality profile, and a search right away).
function PickBar({ onAdded }: { onAdded: () => void }) {
  const picks = usePicks()
  const toast = useToast()
  const [busy, setBusy] = useState<{ done: number; total: number } | null>(null)
  if (!picks?.selecting) return null
  const list = [...picks.picked.values()]
  async function addAll() {
    setBusy({ done: 0, total: list.length })
    let failed = 0
    let asked = 0
    for (let i = 0; i < list.length; i++) {
      const p = list[i]
      try {
        const res = p.kind === 'movie' ? await api.addMovie(p.tmdbId, { searchNow: true }) : await api.addSeries(p.tmdbId, { searchNow: true })
        if (isRequested(res)) asked++
      } catch {
        failed++
      }
      setBusy({ done: i + 1, total: list.length })
    }
    setBusy(null)
    if (failed === 0 && asked > 0) toast.success(plural(asked, 'Asked for {n} title. An administrator will look at it under Activity > Requests.', 'Asked for {n} titles. An administrator will look at them under Activity > Requests.'))
    else if (failed === 0) toast.success(plural(list.length, 'Added {n} title. Mediarium is looking for it now.', 'Added {n} titles. Mediarium is looking for them now.'))
    else toast.error(t('{failed} of {total} could not be added.', { failed, total: list.length }))
    picks?.setSelecting(false)
    onAdded()
  }
  return (
    <div className="pick-bar" role="region" aria-label={t('Picked titles')}>
      <span>{list.length === 0 ? t('Pick the titles you want, then add them all at once.') : t('{n} picked', { n: list.length })}</span>
      <span className="spacer" />
      {!picks.readOnly && (
        <button className="btn-sm" disabled={list.length === 0 || busy !== null} onClick={() => {
            list.forEach((p) => picks.notInterested(p))
            picks.clear()
          }}>
          {t('Not interested')}
        </button>
      )}
      <button className="primary btn-sm" disabled={list.length === 0 || busy !== null} onClick={() => void addAll()}>
        {busy ? t('Adding {done} of {total}…', { done: busy.done, total: busy.total }) : list.length ? plural(list.length, 'Add {n} title', 'Add {n} titles') : t('Add titles')}
      </button>
      <button className="btn-sm" onClick={() => picks.setSelecting(false)}>
        {t('Done')}
      </button>
    </div>
  )
}

// The "Not interested" list, to show a title again.
function NotInterestedList() {
  const picks = usePicks()
  const [open, setOpen] = useState(false)
  const [list, setList] = useState<{ kind: 'movie' | 'tv'; tmdbId: number; title: string; year?: number }[] | null>(null)
  useEffect(() => {
    if (open) api.listExclusions().then(setList).catch(() => setList([]))
  }, [open])
  if (!picks || picks.excluded.size === 0) return null
  return (
    <>
      <button className="btn-sm" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {t('Not interested ({n})', { n: picks.excluded.size })}
      </button>
      {open && list && (
        <div className="card not-interested-list">
          {list.filter((e) => picks.excluded.has(pickKey(e))).map((e) => (
            <div key={pickKey(e)} className="row-actions">
              <span>
                {e.title}
                {e.year ? ` (${e.year})` : ''} <small style={{ color: 'var(--text-dim)' }}>{e.kind === 'tv' ? t('show') : t('movie')}</small>
              </span>
              <button className="btn-sm" onClick={() => picks.showAgain(e.kind, e.tmdbId)}>
                {t('Show again')}
              </button>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function DiscoverPage() {
  const [filtersOpen, setFiltersOpen] = useState(false)
  const owned = useOwned()
  const [includeOlder, setIncludeOlder] = useIncludeOlder()
  const [hideOwned, setHideOwned] = useHideOwned()
  const { hide } = useMyServices()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [addingMusic, setAddingMusic] = useState<AddMusicTarget | null>(null)
  const [addingBook, setAddingBook] = useState<BookFound | null>(null)

  // The tab and the filters live in the address, so Back from a title or an album brings Discover back as it was.
  const [ui, patchUi] = useUrlState({ kind: 'all', genre: '', from: '', to: '', sort: 'popular', type: 'all', language: '', minRating: '', runtime: '' })
  const picked: Show = isSection(ui.kind) ? ui.kind : 'all'
  const setPicked = (k: Show) => patchUi({ kind: k, genre: '', type: 'all', sort: ui.sort === 'rating' && k !== 'movie' && k !== 'tv' ? 'popular' : ui.sort })
  // Only the kinds of media that are switched on get a tab, and a section on
  // the All tab. Ebooks and audiobooks share one Books section.
  const { on } = useModules()
  const booksOn = on('ebooks') || on('audiobooks')
  const mediaKinds = useKinds()
  // Ebooks and audiobooks share one tab: the same books, each marked with the editions that exist.
  const kindsOn: Section[] = [...(on('movies') || on('tv') || on('music') || !booksOn ? mediaKinds : []), ...(booksOn ? (['book'] as const) : [])]
  const tabs: Show[] = ['all', ...kindsOn]
  const show: Show = tabs.includes(picked) ? picked : tabs[0]
  const [subjects, setSubjects] = useState<{ label: string; subject: string }[]>([])
  const genre = ui.genre
  const setGenre = (v: string) => patchUi({ genre: v })
  const from = ui.from
  const setFrom = (v: string) => patchUi({ from: v })
  const to = ui.to
  const setTo = (v: string) => patchUi({ to: v })
  const sort = ui.sort
  const setSort = (v: string) => patchUi({ sort: v })
  // The extra filters (original language, lowest rating, runtime) are for movies and shows only.
  const extraOn = show === 'movie' || show === 'tv'
  const extra = { language: extraOn ? ui.language : '', minRating: extraOn ? ui.minRating : '', runtime: extraOn ? ui.runtime : '' }
  const extraCount = activeExtraFilters(extra)
  const musicType = ui.type as MusicType
  const setMusicType = (v: MusicType) => patchUi({ type: v })
  const [genres, setGenres] = useState<{ id: number; name: string }[]>([])
  const [browseMissing, setBrowseMissing] = useState(false)
  const filtering = !!(genre || from || to || sort !== 'popular' || extraCount > 0 || (show === 'music' && musicType !== 'all'))

  const [showImport, setShowImport] = useState(false)
  const [listUrl, setListUrl] = useState('')
  const [importedFrom, setImportedFrom] = useState('')
  const [imported, setImported] = useState<DiscoverMovie[] | undefined>()
  // The signed-in account's Plex watchlist, when it connected one (Your profile).
  const [watchlist, setWatchlist] = useState<DiscoverMovie[]>([])
  useEffect(() => {
    let live = true
    api
      .plexWatchlist()
      .then((st) => (st.connected ? api.plexWatchlistItems() : []))
      .then((items) => live && setWatchlist(items))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [])
  const watchMovies = watchlist.filter((m) => m.mediaType !== 'tv')
  const watchShows = watchlist.filter((m) => m.mediaType === 'tv')
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  const v = useValidation({
    listUrl: firstError(required(listUrl, t('Paste the address of a Trakt list, for example trakt.tv/users/name/lists/my-list.')), traktList(listUrl)),
  })
  const yearProblem = minMax(from, to, t("The 'from' year can't be later than the 'to' year. Swap them or pick a different year."))

  // Movie and show genres come from the movie database and differ from each
  // other; music has its own list. The All tab has no genre filter.
  useEffect(() => {
    if (show === 'book') {
      setGenres([])
      if (subjects.length === 0) api.bookSubjects().then(setSubjects).catch(() => undefined)
      return
    }
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show])

  const years = useMemo(() => Array.from({ length: new Date().getFullYear() + 2 - 1920 }, (_, i) => String(new Date().getFullYear() + 1 - i)), [])
  const genreOptions =
    show === 'music' ? MUSIC_GENRES.map((g) => ({ value: g, label: g })) : show === 'book' ? subjects.map((s) => ({ value: s.subject, label: s.label })) : genres.map((g) => ({ value: String(g.id), label: g.name }))
  // Movies and shows browse through the movie database; music and books have their own.
  const titleKind = show !== 'music' && show !== 'book'
  const bookFilters = { subject: genre, subjectName: subjects.find((s) => s.subject === genre)?.label, from, to, sort }
  const orders = show === 'all' ? SHARED_ORDERS : DISCOVER_KINDS[show].orders
  const filters: Filters = { genre, genreName: genres.find((g) => String(g.id) === genre)?.name, from, to, sort, ...extra, hide: titleKind ? hide : '', musicType }
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
    patchUi({ genre: '', from: '', to: '', sort: 'popular', type: 'all', language: '', minRating: '', runtime: '' })
  }

  const switcher = (
    <div className="seg kind-tabs">
      {tabs.map((k) => (
        <button
          key={k}
          className={`k-${k}${show === k ? ' active' : ''}`}
          style={k === 'all' ? undefined : { ['--kc' as string]: DISCOVER_KINDS[k].color }}
          onClick={() => setPicked(k)}
          title={k === 'book' ? t('eBooks and audiobooks') : undefined}
        >
          {k === 'all' ? <Icon name="grid" size={15} /> : <KindGlyph kind={k === 'book' ? 'book' : k} size={17} />}
          {k === 'all' ? t('All') : k === 'book' ? t('Books') : KIND_LABEL[k]}
        </button>
      ))}
    </div>
  )

  // The rails of one kind, for its own tab or for its section on the All tab.
  function body(kind: Section, short: boolean) {
    if (kind === 'book') return <BookBody filters={bookFilters} filtering={filtering} onAdd={setAddingBook} onClear={clear} />
    if (kind === 'music') return <MusicBody filters={musicFilters} filtering={filtering} onAdd={setAddingMusic} onClear={clear} />
    return <TitleRails kind={kind} filters={filters} filtering={filtering} includeOlder={includeOlder} setIncludeOlder={setIncludeOlder} owned={owned} onAdd={setAdding} onClear={clear} short={short} />
  }

  const trakt = titleKind

  return (
    <div>
      <div className={`toolbar filter-bar collapsible${filtersOpen ? ' filters-open' : ''}`}>
        {switcher}
        <FilterToggle open={filtersOpen} onToggle={() => setFiltersOpen((o) => !o)} active={filtering} />
        <div className="fold">
        <Dropdown
          icon="sparkles"
          label={t('Genre')}
          value={genre}
          onChange={setGenre}
          disabled={(titleKind && browseMissing) || show === 'all'}
          title={show === 'all' ? t('Pick a kind of media to filter by genre') : undefined}
          options={[{ value: '', label: show === 'all' ? t('Genre: pick a kind') : t('All genres') }, ...genreOptions]}
        />
        <Dropdown icon="calendar" label={t('From year')} value={from} onChange={setFrom} disabled={titleKind && browseMissing} options={[{ value: '', label: t('From year') }, ...years.map((y) => ({ value: y, label: t('From {year}', { year: y }) }))]} />
        <Dropdown icon="calendar" label={t('To year')} value={to} onChange={setTo} disabled={titleKind && browseMissing} options={[{ value: '', label: t('To year') }, ...years.map((y) => ({ value: y, label: t('To {year}', { year: y }) }))]} />
        <Dropdown icon="sort" label={t('Order')} value={sort} onChange={setSort} disabled={titleKind && browseMissing} options={orders} />
        {show === 'music' && <Dropdown icon="disc" label={t('Type')} value={musicType} onChange={(m) => setMusicType(m as MusicType)} options={MUSIC_TYPE_OPTIONS} />}
        {filtering && (
          <button className="btn-sm" onClick={clear}>
            {t('Clear filters')}
          </button>
        )}
        <Switch checked={hideOwned} onChange={setHideOwned} label={t('Hide owned')} />
        <span className="spacer" />
        <NotInterestedList />
        {titleKind && <SelectToggle />}
        {trakt && (
          <button className={`icon-btn trakt-btn${showImport ? ' active' : ''}`} onClick={() => setShowImport((v) => !v)} title={showImport ? t('Close the Trakt list import') : t('Import a Trakt list')} aria-label={showImport ? t('Close the Trakt list import') : t('Import a Trakt list')} aria-pressed={showImport}>
            <Icon name="trakt" size={17} />
          </button>
        )}
        </div>
      </div>
      {extraOn && (
        <div className="more-filters">
          <Collapsible id="discover-more-filters" label={t('More filters')} showLabel summary={extraFiltersSummary(extra)} forceOpen={extraCount > 0}>
            <div className="toolbar filter-bar" style={{ marginBottom: 0 }}>
              <Dropdown
                icon="globe"
                label={t('Original language')}
                value={extra.language}
                onChange={(v) => patchUi({ language: v })}
                disabled={browseMissing}
                title={t('The language the title was first made in. This is separate from the language Mediarium shows names and summaries in, which you pick under Settings > Info, lists and subtitles > Movie and show info.')}
                options={languageOptions(t('Original language: any'))}
              />
              <Dropdown
                icon="star"
                label={t('Lowest rating')}
                value={extra.minRating}
                onChange={(v) => patchUi({ minRating: v })}
                disabled={browseMissing}
                title={t('Only titles with at least this average rating. Titles with very few votes are left out.')}
                options={[{ value: '', label: t('Any rating') }, ...RATINGS.map((r) => ({ value: r, label: ratingLabel(r) }))]}
              />
              <Dropdown
                icon="clock"
                label={t('Runtime')}
                value={extra.runtime}
                onChange={(v) => patchUi({ runtime: v })}
                disabled={browseMissing}
                title={show === 'tv' ? t('How long an episode is.') : t('How long the film is.')}
                options={[{ value: '', label: t('Any runtime') }, ...RUNTIMES.map((r) => ({ value: r, label: runtimeLabel(r) }))]}
              />
            </div>
          </Collapsible>
        </div>
      )}
      {(show === 'all' || extraOn) && <StreamingServicesFilter />}
      {yearProblem && (
        <p className="field-error" role="alert" style={{ margin: '0 0 14px' }}>
          {yearProblem}
        </p>
      )}

      {trakt && showImport && (
        <section className="card grid-form" style={{ marginBottom: 24 }}>
          <h2>{t('Import a Trakt list')}</h2>
          <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: 0 }}>
            {tx('Paste the address of any public Trakt list to see everything on it here. Trakt is a free site where people keep lists of what they watch. See {link}.', {
              link: <Link to="/settings/metadata">{t('Settings > Info, lists and subtitles > Movie and show info')}</Link>,
            })}
          </p>
          <input value={listUrl} onChange={(e) => setListUrl(e.target.value)} placeholder="trakt.tv/users/…/lists/…" aria-label={t('Trakt list address')} {...v.bind('listUrl', listUrl, setListUrl)} />
          <FieldError v={v} name="listUrl" />
          <div>
            <button className="primary" disabled={importing} onClick={onImport}>
              {importing ? t('Importing…') : t('Import')}
            </button>
          </div>
          <FormProblem v={v} action="import" />
          {importError && <p className="error-text" style={{ margin: 0 }}>{importError}</p>}
        </section>
      )}

      <PickBar onAdded={() => owned.reload()} />

      {watchMovies.length > 0 && (show === 'all' || show === 'movie') && kindsOn.includes('movie') && (
        <DiscoverRail title={t('Your Plex watchlist: movies')} items={watchMovies} kind="movie" owned={owned} onAdd={setAdding} rows={1} />
      )}
      {watchShows.length > 0 && (show === 'all' || show === 'tv') && kindsOn.includes('tv') && (
        <DiscoverRail title={t('Your Plex watchlist: shows')} items={watchShows} kind="tv" owned={owned} onAdd={setAdding} rows={1} />
      )}

      {trakt && imported && <DiscoverRail title={t('Imported from {url}', { url: importedFrom })} items={imported} kind="movie" owned={owned} onAdd={setAdding} rows={3} />}

      {show === 'all'
        ? kindsOn.map((kind) => (
            <SectionBlock key={kind} kind={kind}>
              {body(kind, false)}
            </SectionBlock>
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
      <BookAddDialog target={addingBook} onClose={() => setAddingBook(null)} />
    </div>
  )
}
