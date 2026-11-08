import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useOpenFromList } from '../useTwoPane'
import { api, type BulkResult, type MusicArtist, type MusicProfile } from '../api'
import ActionMenu from '../components/ActionMenu'
import BulkBar from '../components/BulkBar'
import { useConfirm } from '../components/ConfirmProvider'
import { useMusicPlayer } from '../components/MusicPlayer'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import PosterCard, { type CardAction } from '../components/PosterCard'
import { WindowedGrid } from '../components/Windowed'
import { describeState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { chosenOf, failureLines, nameList, selectAllKeys, selectionText, selectRange, toggleKey } from '../librarySelection'
import { removeOption } from '../diskUsage'
import { artistQueue } from '../musicPlay'
import FilterToggle from '../components/FilterToggle'
import { plural, t, tx } from '../i18n'
import { useUrlState } from '../useUrlState'

type Sort = 'added' | 'name' | 'missing'

// Where an artist stands: everything downloaded, some of it, nothing yet
// (searching) or not monitored.
export function artistState(a: Pick<MusicArtist, 'albumCount' | 'downloadedCount' | 'monitored' | 'monitoredCount'>): ItemState {
  // What counts is what you asked for: one album added from an artist with 48
  // releases is complete when that album is in, not "1 of 48".
  const wanted = a.monitoredCount > 0 ? a.monitoredCount : a.albumCount
  if (wanted > 0 && a.downloadedCount >= wanted) return describeState('downloaded', undefined, t('Everything you asked for from this artist is downloaded.'))
  if (a.downloadedCount > 0) return describeState('partial', `${a.downloadedCount}/${wanted}`, t('Some of the albums you want are downloaded.'))
  // Not following an artist only stops new releases being picked up; albums
  // that are monitored are still looked for.
  if (!a.monitored && a.monitoredCount === 0) return describeState('added')
  return describeState('searching')
}

const missingOf = (a: MusicArtist) => Math.max(0, a.monitoredCount - a.downloadedCount)

const FILTERS: { id: string; label: string; test: (a: MusicArtist) => boolean }[] = [
  { id: 'all', label: t('All'), test: () => true },
  { id: 'downloaded', label: t('Complete'), test: (a) => artistState(a).key === 'downloaded' },
  { id: 'partial', label: t('Partial'), test: (a) => artistState(a).key === 'partial' },
  { id: 'searching', label: t('Waiting for a release'), test: (a) => artistState(a).key === 'searching' },
  { id: 'added', label: t('Nothing monitored'), test: (a) => artistState(a).key === 'added' },
]

// The Music tab of the library: your artists as cards with how many of their
// albums you have, the same finding and filtering as movies and shows.
export default function MusicLibrary({
  artists,
  error,
  switcher,
  admin,
  reload,
}: {
  artists: MusicArtist[] | null
  error: string
  switcher: ReactNode
  admin: boolean
  reload: () => void
}) {
  const [filtersOpen, setFiltersOpen] = useState(false)
  const navigate = useNavigate()
  const openFromList = useOpenFromList()
  const confirm = useConfirm()
  const toast = useToast()
  const player = useMusicPlayer()
  // The search and filters live in the address, so Back from an artist returns to the same list.
  const [ui, patchUi] = useUrlState({ mq: '', mf: 'all', mm: '', mg: '', ms: 'added' })
  const text = ui.mq
  const setText = (v: string) => patchUi({ mq: v })
  const filter = ui.mf
  const setFilter = (v: string) => patchUi({ mf: v })
  const monitored = ui.mm
  const setMonitored = (v: string) => patchUi({ mm: v })
  const genre = ui.mg
  const setGenre = (v: string) => patchUi({ mg: v })
  const sort = ui.ms as Sort
  const setSort = (v: Sort) => patchUi({ ms: v })
  const [busy, setBusy] = useState(false)
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const anchor = useRef<string | null>(null) // the last one ticked, for shift-click
  const [note, setNote] = useState<{ message: string; lines: string[] } | null>(null)
  const [profiles, setProfiles] = useState<MusicProfile[]>([])
  useEffect(() => {
    if (admin) api.musicProfiles().then(setProfiles).catch(() => undefined)
  }, [admin])

  const genreOptions = useMemo(() => [...new Set((artists ?? []).flatMap((a) => a.genres ?? []))].sort(), [artists])
  const shown = useMemo(() => {
    if (!artists) return null
    const test = FILTERS.find((f) => f.id === filter)?.test ?? (() => true)
    const needle = text.trim().toLowerCase()
    const list = artists.filter(
      (a) =>
        test(a) &&
        (!needle || a.name.toLowerCase().includes(needle)) &&
        (!genre || (a.genres ?? []).includes(genre)) &&
        (monitored === '' || (monitored === 'yes') === a.monitored),
    )
    if (sort === 'name') list.sort((a, b) => a.sortName.localeCompare(b.sortName))
    else if (sort === 'missing') list.sort((a, b) => missingOf(b) - missingOf(a) || a.sortName.localeCompare(b.sortName))
    else list.sort((a, b) => b.addedAt.localeCompare(a.addedAt))
    return list
  }, [artists, filter, text, genre, monitored, sort])

  async function remove(a: MusicArtist) {
    const usage = await api.artistDiskUsage(a.id).catch(() => null)
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: a.name }),
      body: <p>{t('Mediarium stops tracking this artist and cancels anything still downloading for them. You can add them back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: removeOption('artist', usage),
    })
    if (!answer) return
    setBusy(true)
    try {
      await api.deleteArtist(a.id, answer.checked)
      toast.success(answer.checked ? t('{name} and their files removed.', { name: a.name }) : t('{name} removed.', { name: a.name }))
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const keyed = useMemo(() => (shown ?? []).map((a) => ({ key: `artist-${a.id}`, artist: a })), [shown])
  const chosen = useMemo(() => chosenOf(keyed, selected).map((k) => k.artist), [keyed, selected])
  const selText = selectionText({ selected: chosen.length, shown: keyed.length, total: artists?.length ?? 0, noun: 'artist' })
  // The bar counts, and every action reaches, only what is in the view: after a
  // filter or a search, nothing you can no longer see is touched.
  function pick(a: MusicArtist, shift: boolean) {
    const key = `artist-${a.id}`
    setSelected((cur) => (shift ? selectRange(cur, keyed, anchor.current, key) : toggleKey(cur, key)))
    anchor.current = key
  }
  function stopSelecting() {
    setSelecting(false)
    setSelected(new Set())
    setNote(null)
  }
  const ids = () => chosen.map((a) => a.id)

  async function runBulk(action: () => Promise<BulkResult>) {
    setBusy(true)
    setNote(null)
    try {
      const r = await action()
      if (r.failed.length === 0) toast.success(r.message)
      else {
        setNote({ message: r.message, lines: failureLines(r.failed, (_k, id) => artists?.find((a) => a.id === id)?.name) })
        toast.error(r.message)
      }
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function removeChosen() {
    const n = chosen.length
    if (n === 0) return
    const answer = await confirm({
      title: plural(n, 'Remove {n} artist from your library?', 'Remove {n} artists from your library?'),
      body: (
        <>
          <p>
            <strong>{nameList(chosen.map((a) => a.name))}</strong>
          </p>
          <p>{plural(n, 'Mediarium stops tracking them and cancels anything still downloading for them. You can add them back any time.', 'Mediarium stops tracking all {n} and cancels anything still downloading for them. You can add them back any time.')}</p>
          <p>{tx('{stay} unless you tick the box below.', { stay: <strong>{t('Your music files stay where they are')}</strong> })}</p>
        </>
      ),
      confirmLabel: plural(n, 'Remove {n} artist', 'Remove {n} artists'),
      danger: true,
      option: {
        label: plural(n, 'Also delete their albums from the disk', 'Also delete the albums of all {n} from the disk'),
        hint: t('This deletes their album folders in your music library too. It cannot be undone.'),
        defaultChecked: false,
        warning: plural(n, 'The albums of this artist will be deleted from the disk for good.', 'The albums of all {n} artists will be deleted from the disk for good.'),
      },
    })
    if (!answer) return
    await runBulk(() => api.bulkArtistRemove(ids(), answer.checked))
  }

  const filtersActive = text.trim() !== '' || filter !== 'all' || monitored !== '' || genre !== ''
  function clearFilters() {
    setText('')
    setFilter('all')
    setMonitored('')
    setGenre('')
  }

  const profileChoices = [
    { id: '0', label: t('Default ({name})', { name: profiles.find((p) => p.default)?.name ?? t('the default profile') }) },
    ...profiles.map((p) => ({ id: String(p.id), label: p.name })),
  ]

  async function playArtist(a: MusicArtist) {
    if (!player) return
    try {
      const list = await artistQueue(a.id)
      if (list.length === 0) {
        toast.error(t('None of this artist is in your library yet.'))
        return
      }
      player.setShuffle(false)
      player.playQueue(list, 0)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const actionsFor = (a: MusicArtist): CardAction[] => [
    ...(player && a.downloadedCount > 0 ? [{ icon: 'play', label: t('Play'), onClick: () => void playArtist(a) } satisfies CardAction] : []),
    { icon: 'open', label: t('Open'), onClick: () => navigate(openFromList(`/music/artist/${a.id}`)) },
    ...(admin ? [{ icon: 'trash', label: t('Remove from library'), onClick: () => void remove(a), danger: true, disabled: busy } satisfies CardAction] : []),
  ]

  return (
    <div>
      <div className={`toolbar lib-toolbar collapsible${filtersOpen ? ' filters-open' : ''}`}>
        {switcher}
        <input type="search" placeholder={t('Find in your artists…')} value={text} onChange={(e) => setText(e.target.value)} aria-label={t('Find in your artists')} />
        <FilterToggle open={filtersOpen} onToggle={() => setFiltersOpen((o) => !o)} active={filtersActive} />
        <div className="fold">
        {genreOptions.length > 0 && <Dropdown label={t('Genre')} value={genre} onChange={setGenre} options={[{ value: '', label: t('All genres') }, ...genreOptions.map((g) => ({ value: g, label: g }))]} />}
        <Dropdown
          label={t('Following')}
          value={monitored}
          onChange={setMonitored}
          options={[
            { value: '', label: t('Followed or not') },
            { value: 'yes', label: t('Followed') },
            { value: 'no', label: t('Not followed') },
          ]}
        />
        <Dropdown
          label={t('Sort')}
          value={sort}
          onChange={(v) => setSort(v as Sort)}
          options={[
            { value: 'added', label: t('Recently added') },
            { value: 'name', label: t('Name A–Z') },
            { value: 'missing', label: t('Most albums missing') },
          ]}
        />
        <span className="spacer" />
        {admin && (
          <button className={`btn-with-icon${selecting ? ' primary' : ''}`} aria-pressed={selecting} onClick={() => (selecting ? stopSelecting() : setSelecting(true))}>
            <Icon name="check" size={16} /> {t('Select')}
          </button>
        )}
        {admin && (
          <button className="btn-with-icon" onClick={() => navigate('/music/import')}>
            <Icon name="folder" size={16} /> {t('Import existing')}
          </button>
        )}
        </div>
        <button className="btn-with-icon toolbar-playlists" onClick={() => navigate('/music/playlists')}>
          <Icon name="list" size={16} /> {t('Playlists')}
        </button>
        <button className="primary btn-with-icon toolbar-add" onClick={() => navigate('/discover?kind=music')}>
          <Icon name="plus" size={16} /> {t('Add new')}
        </button>
      </div>

      <div className="chip-row" style={{ marginBottom: 18, alignItems: 'center' }}>
        {FILTERS.map((f) => (
          <button key={f.id} className={`chip${filter === f.id ? ' active' : ''}`} onClick={() => setFilter(f.id)}>
            {f.label} {artists ? <small>{artists.filter(f.test).length}</small> : null}
          </button>
        ))}
        {filtersActive && (
          <button className="btn-sm" onClick={clearFilters}>
            {t('Clear filters')}
          </button>
        )}
      </div>

      {selecting && admin && (
        <BulkBar text={selText} onSelectAll={() => setSelected(selectAllKeys(keyed))} onSelectNone={() => setSelected(new Set())} onDone={stopSelecting} busy={busy}>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkArtistFollow(ids(), true))}>
            <Icon name="eye" size={15} /> {t('Follow')}
          </button>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkArtistFollow(ids(), false))}>
            <Icon name="eye-off" size={15} /> {t('Stop following')}
          </button>
          <ActionMenu label={t('Quality profile')} disabled={chosen.length === 0 || busy || profiles.length === 0} choices={profileChoices} onPick={(id) => void runBulk(() => api.bulkArtistProfile(ids(), Number(id)))} />
          <button className="btn-sm btn-danger btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void removeChosen()}>
            <Icon name="trash" size={15} /> {t('Remove from library')}
          </button>
        </BulkBar>
      )}

      {note && (
        <div className="bulk-note" role="status">
          <div>
            <strong>{note.message}</strong>
            <ul>
              {note.lines.map((l, n) => (
                <li key={n}>{l}</li>
              ))}
            </ul>
          </div>
          <button className="btn-sm" onClick={() => setNote(null)}>
            {t('Dismiss')}
          </button>
        </div>
      )}

      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid music-grid">
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '1 / 1.55' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="empty-state">
          <Icon name="music" size={44} />
          {(artists?.length ?? 0) === 0 ? (
            <>
              <p>{t('Your music library is empty.')}</p>
              <div className="row-actions">
                <button className="primary btn-with-icon" onClick={() => navigate('/discover?kind=music')}>
                  <Icon name="search" size={16} /> {t('Search to add an artist')}
                </button>
                {admin && (
                  <button className="btn-with-icon" onClick={() => navigate('/music/import')}>
                    <Icon name="folder" size={16} /> {t('Import the music you already have')}
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
              <p>{t('Nothing matches these filters.')}</p>
              <button className="btn-with-icon" onClick={clearFilters}>
                {t('Clear filters')}
              </button>
            </>
          )}
        </div>
      ) : (
        <WindowedGrid
          className="poster-grid music-grid"
          items={shown}
          render={(a) => {
            const missing = missingOf(a)
            return (
              <PosterCard
                key={a.id}
                to={openFromList(`/music/artist/${a.id}`)}
                poster={a.imageUrl}
                title={a.name}
                meta={[a.disambiguation || null, plural(a.albumCount, '{have} of {n} album', '{have} of {n} albums', { have: a.downloadedCount }), missing > 0 ? t('{n} missing', { n: missing }) : null, a.monitored ? null : t('not followed')].filter(Boolean).join(' · ')}
                state={artistState(a)}
                kind="music"
                dim={artistState(a).key === 'added'}
                actions={actionsFor(a)}
                selection={selecting ? { selected: selected.has(`artist-${a.id}`), onToggle: (e) => pick(a, e.shiftKey) } : undefined}
                footer={
                  a.albumCount > 0 ? (
                    <div className="bar" style={{ width: '100%', marginTop: 6 }} title={plural(a.albumCount, '{have} of {n} album', '{have} of {n} albums', { have: a.downloadedCount })}>
                      <span style={{ width: `${(a.downloadedCount / a.albumCount) * 100}%` }} />
                    </div>
                  ) : undefined
                }
              />
            )
          }}
        />
      )}
    </div>
  )
}
