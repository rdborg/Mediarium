import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  api,
  type ImportList,
  type ImportListInput,
  type ImportListPreview,
  type ImportListSource,
  type ImportListsAnswer,
  type PlexWatchlistRow,
  type QualityProfile,
  type SimklStatus,
} from '../api'
import { defaultProfileFor } from '../profileDefaults'
import { timeAgo } from '../format'
import { plural, t, tx } from '../i18n'
import type { LibraryFolders } from '../api'
import { laterChoices } from '../newTitleFolder'
import { sortProfiles } from './qualityBlurb'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { MediaServerMark } from './mediaServerBrand'
import Switch from './Switch'
import { TagChips, TagInput } from './Tags'
import { useToast } from './Toast'
import './ImportLists.css'
import TipLabel from './TipLabel'

// Settings > Info, lists and subtitles > Import lists: lists on Trakt, TMDB,
// IMDb, MDBList, Simkl and Letterboxd that Mediarium reads on a schedule, adding
// what is on them to the library. Nothing is ever removed because a title
// left a list.

const INTERVALS: { hours: number; label: string }[] = [
  { hours: 1, label: t('Every hour') },
  { hours: 3, label: t('Every 3 hours') },
  { hours: 6, label: t('Every 6 hours') },
  { hours: 12, label: t('Every 12 hours') },
  { hours: 24, label: t('Every day') },
  { hours: 48, label: t('Every 2 days') },
  { hours: 72, label: t('Every 3 days') },
  { hours: 168, label: t('Every week') },
]

const intervalLabel = (h: number) => INTERVALS.find((i) => i.hours === h)?.label ?? plural(h, 'Every {n} hour', 'Every {n} hours')

const LANGUAGES: [string, string][] = [
  ['', t('Any language')],
  ['en', 'English'],
  ['ko', 'Korean'],
  ['ja', 'Japanese'],
  ['fr', 'French'],
  ['de', 'German'],
  ['es', 'Spanish'],
  ['it', 'Italian'],
  ['sv', 'Swedish'],
  ['da', 'Danish'],
  ['hi', 'Hindi'],
  ['zh', 'Chinese'],
  ['tr', 'Turkish'],
]

const blank = (source: ImportListSource): ImportListInput => ({
  name: '',
  source: source.id,
  media: source.media === 'movie' ? 'movie' : 'both',
  config: source.id === 'simkl' ? { status: 'plantowatch' } : source.id.endsWith('chart') ? { chart: source.id === 'tmdb-chart' ? 'popular' : 'trending', count: source.id === 'tmdb-chart' ? 40 : 50 } : {},
  key: '',
  enabled: true,
  intervalHours: 24,
  profileId: 0,
  movieRoot: '',
  tvRoot: '',
  monitored: true,
  searchNow: true,
  tags: [],
  minRating: 0,
  minYear: 0,
  maxPerRun: 10,
  startFresh: false,
})

const toInput = (l: ImportList): ImportListInput => ({
  name: l.name,
  source: l.source,
  media: l.media,
  config: { ...l.config },
  key: '',
  enabled: l.enabled,
  intervalHours: l.intervalHours,
  profileId: l.profileId,
  movieRoot: l.movieRoot,
  tvRoot: l.tvRoot,
  monitored: l.monitored,
  searchNow: l.searchNow,
  tags: l.tags,
  minRating: l.minRating,
  minYear: l.minYear,
  maxPerRun: l.maxPerRun,
  startFresh: l.startFresh,
})

function inTime(iso: string | undefined): string {
  if (!iso) return ''
  const ms = new Date(iso).getTime() - Date.now()
  if (Number.isNaN(ms)) return ''
  if (ms < 60 * 60 * 1000) return t('within the hour')
  const h = Math.round(ms / 3600000)
  return h < 36 ? plural(h, 'in {n} hour', 'in {n} hours') : plural(Math.round(h / 24), 'in {n} day', 'in {n} days')
}

export default function ImportLists() {
  const toast = useToast()
  const confirm = useConfirm()
  const [data, setData] = useState<ImportListsAnswer | null>(null)
  const [error, setError] = useState('')
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfile, setDefaultProfile] = useState(0)
  const [defaultTvProfile, setDefaultTvProfile] = useState(0)
  const [folders, setFolders] = useState<LibraryFolders>({ movies: [], tv: [], newTitles: { movies: { folder: '', mostRoom: false }, tv: { folder: '', mostRoom: false } } })
  const [knownTags, setKnownTags] = useState<string[]>([])
  const [editing, setEditing] = useState<{ id?: number; draft: ImportListInput; hasKey: boolean; hasCsv: boolean } | null>(null)
  const [adding, setAdding] = useState(false)
  const [busy, setBusy] = useState(0)

  const load = useCallback(() => {
    api
      .importLists()
      .then((d) => {
        setData(d)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])
  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(sortProfiles(r.profiles))
      setDefaultProfile(r.defaultId)
      setDefaultTvProfile(defaultProfileFor('tv', r))
    }).catch(() => undefined)
    api.libraryFolders().then(setFolders).catch(() => undefined)
    api.listTags().then((l) => setKnownTags(l.map((x) => x.name))).catch(() => undefined)
  }, [])
  // While a list is being read, look again every few seconds.
  const anyRunning = !!data?.lists.some((l) => l.running)
  useEffect(() => {
    if (!anyRunning) return
    const id = setInterval(load, 3000)
    return () => clearInterval(id)
  }, [anyRunning, load])

  if (!data) {
    return error ? (
      <div>
        <p className="error-text">{error}</p>
        <button onClick={load}>{t('Try again')}</button>
      </div>
    ) : (
      <p>{t('Loading…')}</p>
    )
  }

  async function act(id: number, fn: () => Promise<unknown>, done?: string) {
    setBusy(id)
    try {
      await fn()
      if (done) toast.success(done)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(0)
    }
  }

  async function remove(l: ImportList) {
    const ok = await confirm({
      title: t('Remove the list {name}?', { name: l.name }),
      body: <p>{t('The list is forgotten. Titles it already added stay in your library.')}</p>,
      confirmLabel: t('Remove the list'),
      danger: true,
    })
    if (ok) void act(l.id, () => api.deleteImportList(l.id), t('List removed.'))
  }

  const sourceName = (id: string) => data.sources.find((s) => s.id === id)?.label ?? id

  return (
    <div className="settings-stack">
      <section className="card span-all">
        <h2><TipLabel help="adding#import-lists">{t('Import lists')}</TipLabel></h2>
        <p className="il-intro">
          {t('A list is read on a schedule, and the movies and shows on it are added to your library with the quality, folder and tags you choose. Nothing is ever removed because a title left a list, and a title is added only once, even if you remove it later.')}
        </p>
        <div className="il-actions">
          <button className="primary btn-with-icon" onClick={() => setAdding(true)} disabled={adding || !!editing}>
            <Icon name="plus" size={15} /> {t('Add a list')}
          </button>
        </div>
        {adding && !editing && (
          <div className="il-pick">
            <p className="il-sub">{t('Where does the list come from?')}</p>
            <div className="il-sources">
              {data.sources.map((s) => (
                <button
                  key={s.id}
                  className="il-source"
                  onClick={() => {
                    setEditing({ draft: blank(s), hasKey: false, hasCsv: false })
                    setAdding(false)
                  }}
                >
                  <strong>{s.label}</strong>
                  <small>{s.help}</small>
                </button>
              ))}
            </div>
            <button className="btn-sm" onClick={() => setAdding(false)}>
              {t('Cancel')}
            </button>
          </div>
        )}
      </section>

      {editing && (
        <ListForm
          key={editing.id ?? 'new'}
          initial={editing}
          sources={data.sources}
          profiles={profiles}
          defaultProfile={defaultProfile}
          defaultTvProfile={defaultTvProfile}
          folders={folders}
          knownTags={knownTags}
          onCancel={() => setEditing(null)}
          onSaved={(l, created) => {
            setEditing(null)
            toast.success(created ? t('List added. Mediarium is reading it now.') : t('List saved.'))
            load()
            if (l.running) setData((d) => d && { ...d, lists: [...d.lists.filter((x) => x.id !== l.id), l] })
          }}
        />
      )}

      {data.lists.length === 0 && !editing && !adding && data.plexWatchlists.length === 0 && (
        <p className="il-empty span-all">{t('No lists yet. Add one and Mediarium will keep your library up to date from it.')}</p>
      )}

      {data.lists.map((l) => (
        <section className="card il-list" key={l.id}>
          <h2>
            {l.name} <span className="badge">{sourceName(l.source)}</span>
          </h2>
          <div className="il-head">
            <Switch
              checked={l.enabled}
              disabled={busy === l.id}
              onChange={(v) => void act(l.id, () => api.setImportListEnabled(l.id, v), v ? t('List switched on.') : t('List switched off.'))}
              label={l.enabled ? t('On') : t('Off')}
              description={
                l.enabled
                  ? [intervalLabel(l.intervalHours), l.running ? t('Reading it now…') : l.nextRunAt ? t('next read {when}', { when: inTime(l.nextRunAt) || t('soon') }) : ''].filter(Boolean).join(' · ')
                  : t('Not read while it is off.')
              }
            />
            <div className="il-buttons">
              <button className="btn-sm btn-with-icon" disabled={busy === l.id || l.running} onClick={() => void act(l.id, () => api.runImportList(l.id))}>
                <Icon name="refresh" size={14} /> {l.running ? t('Reading…') : t('Run now')}
              </button>
              <button className="btn-sm" disabled={!!editing} onClick={() => setEditing({ id: l.id, draft: toInput(l), hasKey: l.hasKey, hasCsv: l.hasCsv })}>
                {t('Edit')}
              </button>
              <button className="btn-sm danger-ghost" disabled={busy === l.id} onClick={() => void remove(l)}>
                {t('Remove')}
              </button>
            </div>
          </div>
          <p className={`il-result${l.lastStatus === 'error' ? ' bad' : ''}`}>
            {l.lastRunAt ? (
              <>
                <strong>{l.lastStatus === 'error' ? t('Last read failed') : t('Last read')}</strong> {timeAgo(l.lastRunAt)}. {l.lastMessage}
              </>
            ) : (
              l.running ? t('Reading it for the first time…') : t('Not read yet.')
            )}
          </p>
          <p className="il-meta">
            {[
              l.media === 'both' ? t('Movies and shows') : l.media === 'movie' ? t('Movies') : t('Shows'),
              l.minRating > 0 ? t('rated {n} or more', { n: l.minRating }) : '',
              l.minYear > 0 ? t('from {year}', { year: String(l.minYear) }) : '',
              l.tags.length ? t('tagged {tags}', { tags: l.tags.join(', ') }) : '',
              l.totalAdded > 0 ? plural(l.totalAdded, '{n} title added so far', '{n} titles added so far') : '',
            ]
              .filter(Boolean)
              .join(' · ')}
          </p>
          {l.lastRunAt && (
            <button
              className="link-btn il-forget"
              disabled={busy === l.id || l.running}
              onClick={async () => {
                const ok = await confirm({
                  title: t('Forget what this list has seen?'),
                  body: <p>{t('The next read treats every title on the list as new. Titles already in your library are skipped; the rest are added again, even ones you removed.')}</p>,
                  confirmLabel: t('Forget'),
                })
                if (ok) void act(l.id, () => api.forgetImportList(l.id), t('Forgotten. The next read starts over.'))
              }}
            >
              {t('Start this list over')}
            </button>
          )}
        </section>
      ))}

      {data.plexWatchlists.map((p) => (
        <PlexRow key={p.userId} row={p} onChanged={load} />
      ))}
    </div>
  )
}

function PlexRow({ row, onChanged }: { row: PlexWatchlistRow; onChanged: () => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  return (
    <section className="card il-list">
      <h2>
        {t('Plex watchlist of {name}', { name: row.account || row.plexName || 'Plex' })} <span className="badge">{t('Plex')}</span>
      </h2>
      <div className="il-head">
        <p className="il-plex">
          <MediaServerMark kind="plex" size={16} />{' '}
          {row.autoAdd
            ? t('Checked every 20 minutes. What comes onto the watchlist is added, or asked for if the account has to ask.')
            : t('This account switched it off, so nothing is added from it.')}
        </p>
        <div className="il-buttons">
          <button
            className="btn-sm btn-with-icon"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              try {
                const r = await api.runPlexWatchlistFor(row.userId)
                toast.success(r.added + r.requested > 0 ? t('{added} added, {requested} asked for.', { added: r.added, requested: r.requested }) : t('Nothing new on that watchlist.'))
                onChanged()
              } catch (e) {
                toast.error(e instanceof Error ? e.message : String(e))
              } finally {
                setBusy(false)
              }
            }}
          >
            <Icon name="refresh" size={14} /> {t('Check now')}
          </button>
        </div>
      </div>
      <p className={`il-result${row.lastError ? ' bad' : ''}`}>
        {row.lastError ? row.lastError : row.checkedAt ? t('Last checked {when}.', { when: timeAgo(row.checkedAt) }) : t('Not checked yet.')}{' '}
        {row.added > 0 && t('{n} added so far.', { n: row.added })} {row.requested > 0 && t('{n} asked for so far.', { n: row.requested })}
      </p>
      <p className="il-meta">{tx('The account connects and disconnects its own watchlist under {link}.', { link: <Link to="/settings/services">{t('Settings > Account & Access > Linked services')}</Link> })}</p>
    </section>
  )
}

function ListForm({
  initial,
  sources,
  profiles,
  defaultProfile,
  defaultTvProfile,
  folders,
  knownTags,
  onCancel,
  onSaved,
}: {
  initial: { id?: number; draft: ImportListInput; hasKey: boolean; hasCsv: boolean }
  sources: ImportListSource[]
  profiles: QualityProfile[]
  defaultProfile: number
  defaultTvProfile: number
  folders: LibraryFolders
  knownTags: string[]
  onCancel: () => void
  onSaved: (l: ImportList, created: boolean) => void
}) {
  const [d, setD] = useState<ImportListInput>(initial.draft)
  const [fileName, setFileName] = useState('')
  const [hasCsv, setHasCsv] = useState(initial.hasCsv)
  const [genres, setGenres] = useState<{ id: number; name: string }[]>([])
  const [preview, setPreview] = useState<ImportListPreview | null>(null)
  const [simkl, setSimkl] = useState<SimklStatus | null>(null)
  useEffect(() => {
    if (d.source === 'simkl') api.simklStatus().then(setSimkl).catch(() => undefined)
  }, [d.source])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<'' | 'save' | 'check'>('')
  const fileInput = useRef<HTMLInputElement>(null)
  const source = sources.find((s) => s.id === d.source) as ImportListSource
  const editing = initial.id !== undefined
  const set = (patch: Partial<ImportListInput>) => {
    setD((cur) => ({ ...cur, ...patch }))
    setPreview(null)
  }
  const setCfg = (patch: Partial<ImportListInput['config']>) => set({ config: { ...d.config, ...patch } })

  const movieDefault = profiles.find((p) => p.id === defaultProfile)?.name
  const tvDefault = profiles.find((p) => p.id === defaultTvProfile)?.name
  // A list of shows follows the default for TV; a list of both says which each gets.
  const defaultLabel =
    d.media === 'tv'
      ? tvDefault
        ? t('Default ({name})', { name: tvDefault })
        : t('Default')
      : d.media !== 'movie' && movieDefault && tvDefault && movieDefault !== tvDefault
        ? t('Default ({movies} for movies, {tv} for TV)', { movies: movieDefault, tv: tvDefault })
        : movieDefault
          ? t('Default ({name})', { name: movieDefault })
          : t('Default')

  const singleMedia = d.media === 'movie' ? 'movie' : d.media === 'tv' ? 'tv' : null
  useEffect(() => {
    if (d.source !== 'tmdb-chart' || !singleMedia) return
    api.discoverGenres(singleMedia).then(setGenres).catch(() => setGenres([]))
  }, [d.source, singleMedia])

  function pickFile(f: File | undefined) {
    if (!f) return
    if (f.size > 4 * 1024 * 1024) {
      setError(t('That file is too big to be a list.'))
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      setFileName(f.name)
      setHasCsv(true)
      setError('')
      setCfg({ csv: String(reader.result ?? '') })
    }
    reader.onerror = () => setError(t("That file couldn't be read."))
    reader.readAsText(f)
  }

  const body = (): ImportListInput => ({ ...d, name: d.name.trim() })

  async function check() {
    setBusy('check')
    setError('')
    try {
      setPreview(await api.previewImportList({ ...body(), id: initial.id }))
    } catch (e) {
      setPreview(null)
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  async function save() {
    setBusy('save')
    setError('')
    try {
      const saved = editing ? await api.updateImportList(initial.id as number, body()) : await api.createImportList(body())
      onSaved(saved, !editing)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  const isChart = d.source.endsWith('-chart')
  const isFile = d.source === 'imdb' || d.source === 'letterboxd'
  const movieOnly = source.media === 'movie'
  const unmonitoredNote = !d.monitored

  return (
    <section className="card il-form span-all">
      <h2>{editing ? t('Edit the list') : t('Add a list')}</h2>
      <p className="il-sub">
        <strong>{source.label}.</strong> {source.help}
      </p>
      <div className="grid-form">
        <div className="form-cols">
          <label>
            {t('Name')}
            <input value={d.name} onChange={(e) => set({ name: e.target.value })} placeholder={source.label} maxLength={80} />
          </label>

          {(d.source === 'trakt-list' || d.source === 'tmdb-list' || d.source === 'mdblist' || d.source === 'trakt-watchlist') && (
            <label className="il-wide">
              {d.source === 'trakt-watchlist' ? t('Trakt user name or watchlist address') : t('Address of the list')}
              <input
                value={d.config.url ?? ''}
                onChange={(e) => setCfg({ url: e.target.value })}
                placeholder={
                  d.source === 'trakt-list'
                    ? 'https://trakt.tv/users/name/lists/list-name'
                    : d.source === 'trakt-watchlist'
                      ? 'https://trakt.tv/users/name/watchlist'
                      : d.source === 'tmdb-list'
                        ? 'https://www.themoviedb.org/list/8504994'
                        : 'https://mdblist.com/lists/name/list-name'
                }
              />
              {d.source === 'trakt-watchlist' && <small className="field-hint">{t('Only a public watchlist can be read. Set the profile to public on Trakt first.')}</small>}
            </label>
          )}

          {d.source === 'rss' && (
            <>
              <label className="il-wide">
                {t('Address of the feed')}
                <input value={d.config.url ?? ''} onChange={(e) => setCfg({ url: e.target.value })} placeholder="https://indexer.example/rss?t=2000&apikey=…" spellCheck={false} autoComplete="off" />
                <small className="field-hint">{t('If the feed needs your key, keep it in the address. The address is kept on your server and only sent to that site.')}</small>
              </label>
              <label className="check-row il-check il-wide">
                <input type="checkbox" checked={!!d.config.grab} onChange={(e) => setCfg({ grab: e.target.checked })} />
                <TipLabel text={t('Releases in the feed that have a download link are checked like a new release from an indexer: one that fits the quality profile of a title you have, missing or as a better version than its file, is downloaded. The usual limit of 10 automatic downloads an hour applies.')} learnMore="adding#rss-feeds">
                  {t('Also download wanted releases from this feed')}
                </TipLabel>
              </label>
            </>
          )}

          {d.source === 'simkl' && (
            <label className="il-wide">
              {t('Which Simkl list')}
              <select value={d.config.status ?? 'plantowatch'} onChange={(e) => setCfg({ status: e.target.value })}>
                <option value="plantowatch">{t('Plan to watch')}</option>
                <option value="watching">{t('Watching')}</option>
                <option value="completed">{t('Completed')}</option>
                <option value="hold">{t('On hold')}</option>
                <option value="dropped">{t('Dropped')}</option>
              </select>
              <small className="field-hint">{t('Movies, shows and anime on that list are added. An anime film counts as a movie and other anime as a show. Nothing is removed when you take a title off the list.')}</small>
              {simkl && !simkl.connected && <small className="field-hint error-text">{t('Connect your Simkl account first, in the Simkl card below the lists.')}</small>}
            </label>
          )}

          {d.source === 'mdblist' && (
            <label>
              {t('Your MDBList API key')}
              <input
                type="password"
                autoComplete="off"
                value={d.key ?? ''}
                onChange={(e) => set({ key: e.target.value })}
                placeholder={initial.hasKey ? t('leave blank to keep the saved key') : ''}
              />
              <small className="field-hint">{t('You make one at mdblist.com/preferences. It is kept encrypted and only sent to MDBList.')}</small>
            </label>
          )}

          {isChart && (
            <>
              <label>
                {t('Chart')}
                <select value={d.config.chart ?? ''} onChange={(e) => setCfg({ chart: e.target.value, ...(e.target.value === 'trending' || e.target.value === 'upcoming' ? { genre: 0, language: '' } : {}) })}>
                  <option value="trending">{t('Trending')}</option>
                  <option value="popular">{t('Popular')}</option>
                  {d.source === 'trakt-chart' && <option value="anticipated">{t('Anticipated')}</option>}
                  {d.source === 'tmdb-chart' && <option value="top-rated">{t('Top rated')}</option>}
                  {d.source === 'tmdb-chart' && <option value="upcoming">{t('Upcoming')}</option>}
                </select>
              </label>
              <label>
                {t('How many titles to look at')}
                <input
                  type="number"
                  min={1}
                  max={d.source === 'trakt-chart' ? 100 : 200}
                  value={d.config.count ?? ''}
                  onChange={(e) => setCfg({ count: Number(e.target.value) || undefined })}
                />
              </label>
              {d.source === 'tmdb-chart' && d.config.chart !== 'trending' && d.config.chart !== 'upcoming' && (
                <>
                  <label>
                    {t('Genre')}
                    <select value={d.config.genre ?? 0} onChange={(e) => setCfg({ genre: Number(e.target.value) })} disabled={!singleMedia}>
                      <option value={0}>{singleMedia ? t('Any genre') : t('Choose movies or shows to pick a genre')}</option>
                      {genres.map((g) => (
                        <option key={g.id} value={g.id}>
                          {g.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    {t('Original language')}
                    <select value={d.config.language ?? ''} onChange={(e) => setCfg({ language: e.target.value })}>
                      {(LANGUAGES.some(([c]) => c === (d.config.language ?? '')) ? LANGUAGES : [...LANGUAGES, [d.config.language ?? '', d.config.language ?? ''] as [string, string]]).map(([code, name]) => (
                        <option key={code} value={code}>
                          {name}
                        </option>
                      ))}
                    </select>
                  </label>
                </>
              )}
            </>
          )}

          <label>
            {t('Add')}
            <select value={d.media} onChange={(e) => set({ media: e.target.value as ImportListInput['media'] })} disabled={movieOnly}>
              <option value="both">{t('Movies and shows')}</option>
              <option value="movie">{t('Movies only')}</option>
              <option value="tv">{t('Shows only')}</option>
            </select>
            {movieOnly && <small className="field-hint">{t('This source only has movies.')}</small>}
          </label>
        </div>

        {isFile && (
          <div className="il-file">
            <span className="cell-label">{d.source === 'imdb' ? t('Your IMDb export (CSV file)') : t('Your Letterboxd export (CSV file)')}</span>
            <p className="field-hint">
              {d.source === 'imdb'
                ? t('IMDb blocks programs from downloading lists. Open the list on imdb.com in your browser, choose Export, and upload the file here. To bring in new titles later, upload a newer export.')
                : t('Letterboxd blocks programs from reading its pages. Export your watchlist (Settings > Import & Export) or a list, and upload the CSV here. Titles are matched by name and year, so a few may not be found. To bring in new titles later, upload a newer export.')}
            </p>
            <div className="il-actions">
              <input ref={fileInput} type="file" accept=".csv,text/csv,text/plain" hidden onChange={(e) => pickFile(e.target.files?.[0])} />
              <button className="btn-with-icon" onClick={() => fileInput.current?.click()}>
                <Icon name="plus" size={14} /> {hasCsv ? t('Upload another file') : t('Upload the file')}
              </button>
              {hasCsv && <span className="hint">{fileName ? t('{name} is ready.', { name: fileName }) : t('A file is saved with this list.')}</span>}
            </div>
            <label className="il-wide">
              {t('Or an address that serves the CSV (optional)')}
              <input value={d.config.url ?? ''} onChange={(e) => setCfg({ url: e.target.value })} placeholder={d.source === 'imdb' ? 'https://www.imdb.com/list/ls055592025/' : 'https://example.com/my-list.csv'} />
              <small className="field-hint">{t('Used when no file is uploaded. A site that checks for a browser is refused.')}</small>
            </label>
          </div>
        )}

        <h3 className="il-h3">{t('Only add titles that are')}</h3>
        <div className="form-cols">
          <label>
            {t('Rated at least (TMDB, 0 to 10)')}
            <input type="number" min={0} max={10} step={0.5} value={d.minRating || ''} placeholder={t('Any')} onChange={(e) => set({ minRating: Number(e.target.value) || 0 })} />
            <small className="field-hint">{t("A title nobody has rated yet doesn't pass.")}</small>
          </label>
          <label>
            {t('From this year on')}
            <input type="number" min={1870} max={2200} value={d.minYear || ''} placeholder={t('Any')} onChange={(e) => set({ minYear: Number(e.target.value) || 0 })} />
          </label>
        </div>

        <h3 className="il-h3">{t('How they are added')}</h3>
        <div className="form-cols">
          <label>
            <TipLabel text={t('Decides which releases are good enough to download. "Default" uses the profile that is set as your default.')}>{t('Quality profile')}</TipLabel>
            <select value={d.profileId} onChange={(e) => set({ profileId: Number(e.target.value) })}>
              <option value={0}>{defaultLabel}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          {folders.movies.length > 1 && d.media !== 'tv' && (
            <label>
              {t('Movies go in')}
              <select value={d.movieRoot} onChange={(e) => set({ movieRoot: e.target.value })}>
                {laterChoices(folders.movies, folders.newTitles.movies).map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          {folders.tv.length > 1 && d.media !== 'movie' && (
            <label>
              {t('Shows go in')}
              <select value={d.tvRoot} onChange={(e) => set({ tvRoot: e.target.value })}>
                {laterChoices(folders.tv, folders.newTitles.tv).map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label>
            <TipLabel text={t('Monitored titles are searched for and downloaded. Not monitored ones are only added to the library; you can switch monitoring on later.')}>{t('Monitoring')}</TipLabel>
            <select value={d.monitored ? 'yes' : 'no'} onChange={(e) => set({ monitored: e.target.value === 'yes' })}>
              <option value="yes">{t('Monitored: search for them and download them')}</option>
              <option value="no">{t('Not monitored: just add them to the library')}</option>
            </select>
          </label>
          <label className="check-row il-check">
            <input type="checkbox" checked={d.searchNow && !unmonitoredNote} disabled={unmonitoredNote} onChange={(e) => set({ searchNow: e.target.checked })} />
            <TipLabel text={t('Searches for each new title right after it is added. The usual limit of 10 automatic downloads an hour still applies.')}>{t('Start searching for each one as it is added')}</TipLabel>
          </label>
          <div className="add-tags il-wide">
            <TipLabel text={t('Labels put on every title this list adds, so you can find and filter them later.')}>{t('Tags (optional)')}</TipLabel>
            <TagChips tags={d.tags} onRemove={(x) => set({ tags: d.tags.filter((y) => y !== x) })} />
            <TagInput
              suggestions={knownTags.filter((k) => !d.tags.some((x) => x.toLowerCase() === k.toLowerCase()))}
              onAdd={(x) => set({ tags: d.tags.some((y) => y.toLowerCase() === x.toLowerCase()) ? d.tags : [...d.tags, x] })}
              placeholder={t('Like Kids or From Trakt')}
            />
          </div>
        </div>

        <h3 className="il-h3">{t('When')}</h3>
        <div className="form-cols">
          <label>
            <TipLabel text={t('How often Mediarium looks at the list for new titles. Titles that leave the list are never removed from your library.')}>{t('Read the list')}</TipLabel>
            <select value={d.intervalHours} onChange={(e) => set({ intervalHours: Number(e.target.value) })}>
              {INTERVALS.map((i) => (
                <option key={i.hours} value={i.hours}>
                  {i.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t('Add at most this many at a time')}
            <select value={d.maxPerRun} onChange={(e) => set({ maxPerRun: Number(e.target.value) })}>
              {[5, 10, 25, 50, 100].map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            <small className="field-hint">{t('A long list is added over several reads, so your indexers are not hit all at once.')}</small>
          </label>
          {!editing && (
            <label className="check-row il-check il-wide">
              <input type="checkbox" checked={d.startFresh} onChange={(e) => set({ startFresh: e.target.checked })} />
              <span>
                {t('Leave what is on the list now alone')}
                <small className="field-hint" style={{ display: 'block' }}>{t('Only titles added to the list after this are added to your library.')}</small>
              </span>
            </label>
          )}
        </div>
      </div>

      {preview && (
        <div className="test-result ok">
          <Icon name="check" size={15} />{' '}
          {preview.found === 0
            ? t('The list is empty.')
            : t('Found {found}: {movies} and {shows}.', {
                found: plural(preview.found, '{n} title', '{n} titles'),
                movies: plural(preview.movies, '{n} movie', '{n} movies'),
                shows: plural(preview.shows, '{n} show', '{n} shows'),
              })}{' '}
          {preview.sample.length > 0 && t('For example: {titles}.', { titles: preview.sample.join(', ') })}
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
      <div className="form-actions">
        <div className="fa-left">
          <button onClick={onCancel} disabled={!!busy}>
            {t('Cancel')}
          </button>
        </div>
        <div className="fa-mid">
          <button className="btn-with-icon" onClick={() => void check()} disabled={!!busy}>
            <Icon name="search" size={15} /> {busy === 'check' ? t('Checking…') : t('Check the list')}
          </button>
          <button className="primary btn-with-icon" onClick={() => void save()} disabled={!!busy}>
            <Icon name="check" size={15} /> {busy === 'save' ? t('Saving…') : editing ? t('Save') : t('Add the list')}
          </button>
        </div>
        <div className="fa-right" />
      </div>
    </section>
  )
}
