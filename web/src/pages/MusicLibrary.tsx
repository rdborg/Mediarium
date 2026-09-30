import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type BulkResult, type MusicArtist, type MusicProfile } from '../api'
import ActionMenu from '../components/ActionMenu'
import BulkBar from '../components/BulkBar'
import { useConfirm } from '../components/ConfirmProvider'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import PosterCard, { type CardAction } from '../components/PosterCard'
import { describeState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { chosenOf, count, failureLines, nameList, selectAllKeys, selectionText, selectRange, toggleKey } from '../librarySelection'
import { removeOption } from '../diskUsage'

type Sort = 'added' | 'name' | 'missing'

// Where an artist stands: everything downloaded, some of it, nothing yet
// (searching) or not monitored.
export function artistState(a: Pick<MusicArtist, 'albumCount' | 'downloadedCount' | 'monitored' | 'monitoredCount'>): ItemState {
  if (a.albumCount > 0 && a.downloadedCount >= a.albumCount) return describeState('downloaded')
  if (a.downloadedCount > 0) return describeState('partial', `${a.downloadedCount}/${a.albumCount}`)
  // Not following an artist only stops new releases being picked up; albums
  // that are monitored are still looked for.
  if (!a.monitored && a.monitoredCount === 0) return describeState('added')
  return describeState('searching')
}

const missingOf = (a: MusicArtist) => Math.max(0, a.monitoredCount - a.downloadedCount)

const FILTERS: { id: string; label: string; test: (a: MusicArtist) => boolean }[] = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'downloaded', label: 'Complete', test: (a) => artistState(a).key === 'downloaded' },
  { id: 'partial', label: 'Partial', test: (a) => artistState(a).key === 'partial' },
  { id: 'searching', label: 'Waiting for a release', test: (a) => artistState(a).key === 'searching' },
  { id: 'added', label: 'Nothing monitored', test: (a) => artistState(a).key === 'added' },
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
  const navigate = useNavigate()
  const confirm = useConfirm()
  const toast = useToast()
  const [text, setText] = useState('')
  const [filter, setFilter] = useState('all')
  const [monitored, setMonitored] = useState('')
  const [genre, setGenre] = useState('')
  const [sort, setSort] = useState<Sort>('added')
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
      title: `Remove ${a.name} from your library?`,
      body: <p>Mediarium stops tracking this artist and cancels anything still downloading for them. You can add them back any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: removeOption('artist', usage),
    })
    if (!answer) return
    setBusy(true)
    try {
      await api.deleteArtist(a.id, answer.checked)
      toast.success(`${a.name} removed${answer.checked ? ' and their files' : ''}.`)
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
    const what = count(n, 'artist')
    const answer = await confirm({
      title: `Remove ${what} from your library?`,
      body: (
        <>
          <p>
            <strong>{nameList(chosen.map((a) => a.name))}</strong>
          </p>
          <p>
            Mediarium stops tracking {n === 1 ? 'them' : `all ${n}`} and cancels anything still downloading for them. You can add them back any time.
          </p>
          <p>
            <strong>Your music files stay where they are</strong> unless you tick the box below.
          </p>
        </>
      ),
      confirmLabel: `Remove ${what}`,
      danger: true,
      option: {
        label: n === 1 ? 'Also delete their albums from the disk' : `Also delete the albums of all ${n} from the disk`,
        hint: 'This deletes their album folders in your music library too. It cannot be undone.',
        defaultChecked: false,
        warning: `The albums of ${n === 1 ? 'this artist' : `all ${n} artists`} will be deleted from the disk for good.`,
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
    { id: '0', label: `Default (${profiles.find((p) => p.default)?.name ?? 'the default profile'})` },
    ...profiles.map((p) => ({ id: String(p.id), label: p.name })),
  ]

  const actionsFor = (a: MusicArtist): CardAction[] => [
    { icon: 'open', label: 'Open', onClick: () => navigate(`/music/artist/${a.id}`) },
    ...(admin ? [{ icon: 'trash', label: 'Remove from library', onClick: () => void remove(a), danger: true, disabled: busy } satisfies CardAction] : []),
  ]

  return (
    <div>
      <div className="toolbar">
        {switcher}
        <input type="search" placeholder="Find in your artists…" value={text} onChange={(e) => setText(e.target.value)} aria-label="Find in your artists" />
        {genreOptions.length > 0 && <Dropdown label="Genre" value={genre} onChange={setGenre} options={[{ value: '', label: 'All genres' }, ...genreOptions.map((g) => ({ value: g, label: g }))]} />}
        <Dropdown
          label="Following"
          value={monitored}
          onChange={setMonitored}
          options={[
            { value: '', label: 'Followed or not' },
            { value: 'yes', label: 'Followed' },
            { value: 'no', label: 'Not followed' },
          ]}
        />
        <Dropdown
          label="Sort"
          value={sort}
          onChange={(v) => setSort(v as Sort)}
          options={[
            { value: 'added', label: 'Recently added' },
            { value: 'name', label: 'Name A–Z' },
            { value: 'missing', label: 'Most albums missing' },
          ]}
        />
        <span className="spacer" />
        {admin && (
          <button className={`btn-with-icon${selecting ? ' primary' : ''}`} aria-pressed={selecting} onClick={() => (selecting ? stopSelecting() : setSelecting(true))}>
            <Icon name="check" size={16} /> Select
          </button>
        )}
        {admin && (
          <button className="btn-with-icon" onClick={() => navigate('/music/import')}>
            <Icon name="folder" size={16} /> Import existing
          </button>
        )}
        <button className="primary btn-with-icon" onClick={() => navigate('/discover?kind=music')}>
          <Icon name="plus" size={16} /> Add new
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
            Clear filters
          </button>
        )}
      </div>

      {selecting && admin && (
        <BulkBar text={selText} onSelectAll={() => setSelected(selectAllKeys(keyed))} onSelectNone={() => setSelected(new Set())} onDone={stopSelecting} busy={busy}>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkArtistFollow(ids(), true))}>
            <Icon name="eye" size={15} /> Follow
          </button>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkArtistFollow(ids(), false))}>
            <Icon name="eye-off" size={15} /> Stop following
          </button>
          <ActionMenu label="Quality profile" disabled={chosen.length === 0 || busy || profiles.length === 0} choices={profileChoices} onPick={(id) => void runBulk(() => api.bulkArtistProfile(ids(), Number(id)))} />
          <button className="btn-sm btn-danger btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void removeChosen()}>
            <Icon name="trash" size={15} /> Remove from library
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
            Dismiss
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
              <p>Your music library is empty.</p>
              <div className="row-actions">
                <button className="primary btn-with-icon" onClick={() => navigate('/discover?kind=music')}>
                  <Icon name="search" size={16} /> Search to add an artist
                </button>
                {admin && (
                  <button className="btn-with-icon" onClick={() => navigate('/music/import')}>
                    <Icon name="folder" size={16} /> Import the music you already have
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
              <p>Nothing matches these filters.</p>
              <button className="btn-with-icon" onClick={clearFilters}>
                Clear filters
              </button>
            </>
          )}
        </div>
      ) : (
        <div className="poster-grid music-grid">
          {shown.map((a) => {
            const missing = missingOf(a)
            return (
              <PosterCard
                key={a.id}
                to={`/music/artist/${a.id}`}
                poster={a.imageUrl}
                title={a.name}
                meta={[a.disambiguation || null, `${a.downloadedCount} of ${a.albumCount} albums`, missing > 0 ? `${missing} missing` : null, a.monitored ? null : 'not followed'].filter(Boolean).join(' · ')}
                state={artistState(a)}
                kind="music"
                dim={artistState(a).key === 'added'}
                actions={actionsFor(a)}
                selection={selecting ? { selected: selected.has(`artist-${a.id}`), onToggle: (e) => pick(a, e.shiftKey) } : undefined}
                footer={
                  a.albumCount > 0 ? (
                    <div className="bar" style={{ width: '100%', marginTop: 6 }} title={`${a.downloadedCount} of ${a.albumCount} albums`}>
                      <span style={{ width: `${(a.downloadedCount / a.albumCount) * 100}%` }} />
                    </div>
                  ) : undefined
                }
              />
            )
          })}
        </div>
      )}
    </div>
  )
}
