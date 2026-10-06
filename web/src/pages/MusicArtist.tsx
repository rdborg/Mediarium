import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { api, can, isAdmin, type MusicAlbum, type MusicArtist as MusicArtistData, type MusicProfile, type MusicRelease, type QueueItem, type SearchResult } from '../api'
import { useAuth } from '../AuthContext'
import { useConfirm } from '../components/ConfirmProvider'
import Cover from '../components/Cover'
import FilesPanel from '../components/FilesPanel'
import Icon from '../components/Icon'
import ReleaseTable from '../components/ReleaseTable'
import SearchStatus from '../components/SearchStatus'
import { describeState, type ItemState } from '../components/state'
import Switch from '../components/Switch'
import TitleEvents from '../components/TitleEvents'
import { useToast } from '../components/Toast'
import { useModules } from '../ModulesContext'
import { useLive } from '../useLive'
import { artistState } from './MusicLibrary'
import { removeOption } from '../diskUsage'
import ReleaseNotice from '../components/ReleaseNotice'
import type { Unanswered } from '../releaseHint'
import { useDocumentTitle } from '../documentTitle'

type Tab = 'tracks' | 'releases' | 'events' | 'files'

const GROUPS: { type: string; title: string; hint: string }[] = [
  { type: 'album', title: 'Albums', hint: 'full studio albums' },
  { type: 'ep', title: 'EPs', hint: 'shorter releases' },
  { type: 'single', title: 'Singles', hint: 'one or two songs' },
]

const TYPE_NAME: Record<string, string> = { album: 'Album', ep: 'EP', single: 'Single' }

function trackLength(ms: number): string {
  if (!ms) return ''
  const s = Math.round(ms / 1000)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

// The plain-words label for what a release in the search says it is.
function releaseQuality(r: SearchResult): string | undefined {
  const m = r as MusicRelease
  return [m.quality, m.bitDepth ? `${m.bitDepth}-bit` : null, m.source].filter(Boolean).join(' · ') || undefined
}

function albumState(a: MusicAlbum, live?: QueueItem): ItemState {
  if (live) return describeState('downloading', `${Math.round(live.progressPct)}%`)
  if (a.status === 'downloaded') return describeState('downloaded')
  if (a.status === 'downloading') return describeState('downloading')
  if (!a.monitored) return describeState('added')
  return describeState('searching')
}

// One artist: their albums, EPs and singles as cards. Each album can be
// searched for now, chosen by hand from the releases found, and opened for its
// tracklist and its own activity log.
export default function MusicArtist() {
  // Another artist starts clean, so nothing of the last one stays on the page.
  return <ArtistPage key={useParams<{ id: string }>().id} />
}

function ArtistPage() {
  const confirm = useConfirm()
  const toast = useToast()
  const navigate = useNavigate()
  const location = useLocation()
  const admin = isAdmin(useAuth().user)
  const { on, loaded } = useModules()
  const { id } = useParams<{ id: string }>()
  const artistId = Number(id)

  const [artist, setArtist] = useState<MusicArtistData | null>(null)
  useDocumentTitle(artist?.name)
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [error, setError] = useState('')
  const [open, setOpen] = useState<{ id: number; tab: Tab } | null>(null)
  const [searchingAll, setSearchingAll] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)
  const jumped = useRef(false)
  const [profiles, setProfiles] = useState<MusicProfile[]>([])
  useEffect(() => {
    if (admin) api.musicProfiles().then(setProfiles).catch(() => undefined)
  }, [admin])

  const load = useCallback(() => {
    api
      .getArtist(artistId)
      .then((a) => {
        setArtist(a)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.listQueue().then(setQueue).catch(() => undefined)
  }, [artistId])
  useEffect(load, [load])
  useLive(load, 5000)

  // Arriving from Activity or Upcoming with #album-12 opens that album.
  useEffect(() => {
    if (jumped.current || !artist?.albums) return
    const m = /^#album-(\d+)$/.exec(location.hash)
    if (!m) return
    jumped.current = true
    const want = Number(m[1])
    if (!artist.albums.some((a) => a.id === want)) return
    setOpen({ id: want, tab: 'tracks' })
    setTimeout(() => document.getElementById(`album-${want}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 150)
  }, [artist, location.hash])

  const albums = useMemo(() => artist?.albums ?? [], [artist])
  const grouped = useMemo(() => {
    const byType = (t: string) => albums.filter((a) => (GROUPS.some((g) => g.type === a.type) ? a.type === t : t === 'other')).sort((a, b) => b.releaseDate.localeCompare(a.releaseDate))
    return [...GROUPS, { type: 'other', title: 'Other releases', hint: '' }].map((g) => ({ ...g, items: byType(g.type) })).filter((g) => g.items.length > 0)
  }, [albums])

  const liveFor = (albumId: number) => queue.find((q) => q.albumId === albumId && (q.status === 'downloading' || q.status === 'importing'))
  const cover = useMemo(() => {
    const sorted = [...albums].sort((a, b) => Number(b.status === 'downloaded') - Number(a.status === 'downloaded') || Number(b.type === 'album') - Number(a.type === 'album') || b.releaseDate.localeCompare(a.releaseDate))
    return sorted.find((a) => a.coverUrl)?.coverUrl
  }, [albums])

  function patchAlbum(albumId: number, change: Partial<MusicAlbum>) {
    setArtist((cur) => (cur ? { ...cur, albums: (cur.albums ?? []).map((a) => (a.id === albumId ? { ...a, ...change } : a)) } : cur))
  }

  // Following an artist means releases MusicBrainz lists later are picked up.
  async function setFollowed(next: boolean) {
    if (!artist) return
    const before = artist
    setArtist({ ...artist, monitored: next })
    try {
      setArtist(await api.updateArtist(artist.id, { monitored: next }))
      toast.success(next ? `Following ${artist.name}.` : `Stopped following ${artist.name}.`)
    } catch (e) {
      setArtist(before)
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function chooseProfile(profileId: number) {
    if (!artist) return
    const before = artist
    try {
      setArtist(await api.updateArtist(artist.id, { profileId }))
      toast.success('Quality profile saved.')
    } catch (e) {
      setArtist(before)
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggleMonitored(a: MusicAlbum) {
    const next = !a.monitored
    patchAlbum(a.id, { monitored: next })
    try {
      await api.setAlbumMonitored(a.id, next)
      toast.success(next ? `${a.title} is monitored.` : `${a.title} is not monitored.`)
    } catch (e) {
      patchAlbum(a.id, { monitored: !next })
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function searchNow(a: MusicAlbum) {
    setBusy(a.id)
    try {
      const r = await api.searchNowAlbum(a.id)
      toast.info(`${a.title}: ${r.message}`)
      setTimeout(load, 600)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  // Search for every monitored album that is still missing, one after another.
  async function searchMissing() {
    const todo = albums.filter((a) => a.monitored && a.status === 'missing')
    if (todo.length === 0) return
    setSearchingAll(true)
    let grabbed = 0
    try {
      for (const a of todo) grabbed += (await api.searchNowAlbum(a.id)).grabbed
      toast.success(grabbed > 0 ? `Started ${grabbed} download${grabbed === 1 ? '' : 's'}. Follow them in Activity.` : 'Nothing suitable found yet. Mediarium keeps looking.')
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSearchingAll(false)
    }
  }

  async function onRemove() {
    if (!artist) return
    const usage = await api.artistDiskUsage(artist.id).catch(() => null)
    const answer = await confirm({
      title: `Remove ${artist.name} from your library?`,
      body: <p>Mediarium stops tracking this artist and cancels anything still downloading for them. You can add them back any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: removeOption('artist', usage),
    })
    if (!answer) return
    try {
      await api.deleteArtist(artist.id, answer.checked)
      toast.success(`${artist.name} removed${answer.checked ? ' and their files' : ''}.`)
      navigate('/library?kind=music')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (loaded && !on('music')) {
    return (
      <div className="empty-state">
        <Icon name="music" size={44} />
        <p>
          Music is switched off. {admin ? <Link to="/settings/modules">Switch it on in Settings &gt; Media types.</Link> : 'Ask an administrator to switch it on.'}
        </p>
      </div>
    )
  }
  if (error && !artist) return <p className="error-text">{error}</p>
  if (!artist) return <div className="skeleton" style={{ height: 320, borderRadius: 22 }} />

  const state = artistState(artist)
  const missing = albums.filter((a) => a.monitored && a.status === 'missing').length

  return (
    <div>
      <section className="title-hero kind-music">
        <div className="title-poster">
          <Cover src={artist.imageUrl || artist.coverUrl || cover} alt={artist.name} />
        </div>
        <div className="title-info">
          <div className="title-kicker">
            <span className="kind-tag kind-music">
              <Icon name="music" size={13} /> Artist
            </span>
            <span className={`state-tag st-${state.key}`} title={state.hint}>
              <Icon name={state.icon} size={13} /> {state.label}
            </span>
          </div>
          <h1>{artist.name}</h1>
          {artist.disambiguation && <p className="title-tagline">{artist.disambiguation}</p>}
          <div className="title-chips">
            <span className="chip-plain">
              <Icon name="disc" size={13} /> {artist.albumCount} release{artist.albumCount === 1 ? '' : 's'}
            </span>
            <span className="chip-plain">
              <Icon name="check" size={13} /> {artist.downloadedCount} downloaded
            </span>
            {missing > 0 && (
              <span className="chip-plain">
                <Icon name="search" size={13} /> {missing} missing
              </span>
            )}
            {!admin && (
              <>
                <span className="chip-genre" title="Albums are only downloaded in qualities this profile accepts">
                  Quality: {artist.profileName || 'Default'}
                </span>
                <span className="chip-genre" title="Following means new releases are picked up">
                  {artist.monitored ? 'Following' : 'Not following'}
                </span>
              </>
            )}
          </div>
          <div className="title-links">
            <a className="link-pill" href={`https://musicbrainz.org/artist/${artist.mbid}`} target="_blank" rel="noreferrer">
              <Icon name="external" size={13} /> More about this artist
            </a>
          </div>
          {admin && (
            <div className="title-controls">
              <Switch checked={artist.monitored} onChange={(v) => void setFollowed(v)} label="Follow this artist" description="Checks for new releases every 12 hours and monitors them." />
              {profiles.length > 0 && (
                <div className="picker-row">
                  <label>
                    Quality profile
                    <select value={artist.profileId} onChange={(e) => void chooseProfile(Number(e.target.value))}>
                      <option value={0}>Default ({profiles.find((p) => p.default)?.name ?? 'default'})</option>
                      {profiles.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
              )}
            </div>
          )}
          <div className="title-actions">
            <button className="primary btn-with-icon" onClick={() => void searchMissing()} disabled={searchingAll || missing === 0}>
              <Icon name="search" size={16} /> {searchingAll ? 'Searching…' : missing > 0 ? `Search for ${missing} missing album${missing === 1 ? '' : 's'}` : 'Nothing missing'}
            </button>
            {admin && (
              <button className="btn-with-icon danger-ghost" onClick={() => void onRemove()}>
                <Icon name="trash" size={16} /> Remove
              </button>
            )}
          </div>
        </div>
      </section>

      {grouped.length === 0 && (
        <div className="empty-state">
          <Icon name="disc" size={44} />
          <p>No albums, EPs or singles found for this artist yet.</p>
        </div>
      )}

      {grouped.map((g) => (
        <section key={g.type} className="album-section">
          <div className="rail-head">
            <h2>
              {g.title} <small>({g.items.length})</small>
            </h2>
            <span>
              {g.items.filter((a) => a.status === 'downloaded').length} downloaded{g.hint ? ` · ${g.hint}` : ''}
            </span>
          </div>
          <div className="album-grid">
            {g.items.map((a) => (
              <AlbumCard
                key={a.id}
                album={a}
                live={liveFor(a.id)}
                open={open?.id === a.id ? open.tab : null}
                busy={busy === a.id}
                onToggleMonitored={() => void toggleMonitored(a)}
                onSearchNow={() => void searchNow(a)}
                onOpen={(tab) => setOpen(open?.id === a.id && open.tab === tab ? null : { id: a.id, tab })}
                onChanged={load}
              />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function AlbumCard({
  album,
  live,
  open,
  busy,
  onToggleMonitored,
  onSearchNow,
  onOpen,
  onChanged,
}: {
  album: MusicAlbum
  live?: QueueItem
  open: Tab | null
  busy: boolean
  onToggleMonitored: () => void
  onSearchNow: () => void
  onOpen: (tab: Tab) => void
  onChanged: () => void
}) {
  const me = useAuth().user
  const state = albumState(album, live)
  return (
    <article id={`album-${album.id}`} className={`album-card${open ? ' open' : ''}${album.monitored ? '' : ' dim'}`}>
      <div className="album-top">
        <Cover src={album.coverUrl} alt={`${album.title} cover`} className="album-cover" />
        <div className="album-info">
          <h3 title={album.title}>{album.title}</h3>
          <p className="album-sub">
            {[album.year || null, TYPE_NAME[album.type] ?? album.type, album.quality || null].filter(Boolean).join(' · ')}
          </p>
          <div className="album-tags">
            <span className={`state-tag st-${state.key}`} title={state.hint}>
              <Icon name={state.icon} size={12} /> {state.label}
            </span>
            {!album.monitored && <span className="badge">not monitored</span>}
          </div>
        </div>
        <button
          className={`icon-btn${album.monitored ? ' on' : ''}`}
          onClick={onToggleMonitored}
          aria-pressed={album.monitored}
          title={album.monitored ? 'Monitored (click to stop)' : 'Not monitored (click to monitor)'}
          aria-label={album.monitored ? `Stop monitoring ${album.title}` : `Monitor ${album.title}`}
        >
          <Icon name={album.monitored ? 'eye' : 'eye-off'} size={17} />
        </button>
      </div>
      {live && (
        <div className="album-progress" title={`${Math.round(live.progressPct)}%`}>
          <div className="bar active">
            <span style={{ width: `${Math.max(3, live.progressPct)}%` }} />
          </div>
        </div>
      )}
      <div className="album-actions">
        {can(me, 'manage') && (
          <button className="btn-sm btn-with-icon" onClick={onSearchNow} disabled={busy || album.status === 'downloading' || !!live}>
            <Icon name="search" size={14} /> {busy ? 'Searching…' : album.status === 'downloaded' ? 'Look for better' : 'Search now'}
          </button>
        )}
        {can(me, 'releases') && (
          <button className={`btn-sm btn-with-icon${open === 'releases' ? ' primary' : ''}`} onClick={() => onOpen('releases')}>
            <Icon name="list" size={14} /> Choose a release
          </button>
        )}
        <button className={`btn-sm btn-with-icon${open === 'tracks' || open === 'events' || open === 'files' ? ' primary' : ''}`} onClick={() => onOpen(open === 'events' || open === 'files' ? open : 'tracks')} aria-expanded={!!open}>
          <Icon name={open ? 'chevron-up' : 'chevron-down'} size={14} /> Details
        </button>
      </div>
      {open && <AlbumPanel album={album} tab={open} onTab={onOpen} onChanged={onChanged} />}
    </article>
  )
}

function AlbumPanel({ album, tab, onTab, onChanged }: { album: MusicAlbum; tab: Tab; onTab: (t: Tab) => void; onChanged: () => void }) {
  return (
    <div className="album-panel">
      <div className="seg">
        <button className={tab === 'tracks' ? 'active' : ''} onClick={() => onTab('tracks')}>
          Tracks
        </button>
        <button className={tab === 'releases' ? 'active' : ''} onClick={() => onTab('releases')}>
          Releases
        </button>
        <button className={tab === 'events' ? 'active' : ''} onClick={() => onTab('events')}>
          What happened
        </button>
        <button className={tab === 'files' ? 'active' : ''} onClick={() => onTab('files')}>
          Files
        </button>
      </div>
      {tab === 'tracks' && <TracksPanel album={album} />}
      {tab === 'releases' && <ReleasesPanel album={album} onChanged={onChanged} />}
      {tab === 'files' && <FilesPanel kind="album" id={album.id} />}
      {tab === 'events' && (
        <div className="album-events">
          {album.status === 'missing' && album.monitored && <SearchStatus kind="album" id={album.id} />}
          <TitleEvents kind="album" id={album.id} />
        </div>
      )}
    </div>
  )
}

function TracksPanel({ album }: { album: MusicAlbum }) {
  const [full, setFull] = useState<MusicAlbum | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    api
      .getAlbum(album.id)
      .then((a) => {
        setFull(a)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [album.id])
  // Reload when the album changes (a download finished, an upgrade landed).
  useEffect(load, [load, album.status, album.quality])
  useLive(load, 10000)

  if (error) return <p className="error-text">{error}</p>
  if (!full) return <div className="skeleton" style={{ height: 90 }} />
  const tracks = full.tracks ?? []
  const discs = new Set(tracks.map((t) => t.disc)).size
  const have = tracks.filter((t) => t.hasFile).length
  return (
    <div>
      {full.path && <code className="filepath">{full.path}</code>}
      {tracks.length === 0 ? (
        <p className="album-empty">The tracklist appears after the first search or download.</p>
      ) : (
        <>
          <p className="album-empty">
            {have} of {tracks.length} tracks in your library.
          </p>
          <table className="track-table">
            <tbody>
              {tracks.map((t, i) => (
                <tr key={t.id} className={t.hasFile ? 'have' : 'lack'}>
                  <td className="track-no">
                    {discs > 1 && (i === 0 || tracks[i - 1].disc !== t.disc) ? <span className="track-disc">Disc {t.disc}</span> : null}
                    {t.position}
                  </td>
                  <td>{t.title}</td>
                  <td className="track-len">{trackLength(t.lengthMs)}</td>
                  <td className="track-file" title={t.filePath}>
                    {t.hasFile ? (
                      <span className="state-tag st-downloaded" title="In your library">
                        <Icon name="check" size={12} /> <span className="tf-text">in library</span>
                      </span>
                    ) : (
                      <span className="state-tag st-added" title="Missing">
                        <Icon name="x" size={12} /> <span className="tf-text">missing</span>
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  )
}

function ReleasesPanel({ album, onChanged }: { album: MusicAlbum; onChanged: () => void }) {
  const toast = useToast()
  const [results, setResults] = useState<MusicRelease[] | null>(null)
  const [answered, setAnswered] = useState<{ sources: number; unanswered: Unanswered[] }>({ sources: 0, unanswered: [] })
  const [error, setError] = useState('')
  const [grabbing, setGrabbing] = useState<string | null>(null)

  const search = useCallback(() => {
    setResults(null)
    setError('')
    api
      .albumSearch(album.id)
      .then((a) => {
        setAnswered({ sources: a.sources, unanswered: a.unanswered })
        setResults(a.results)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [album.id])
  useEffect(search, [search])

  async function grab(r: SearchResult) {
    setGrabbing(r.downloadUrl)
    try {
      await api.grabAlbum(album.id, { releaseTitle: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol })
      toast.success(`Started downloading "${r.title}". Follow it in Activity.`)
      setTimeout(onChanged, 500)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setGrabbing(null)
    }
  }

  if (error) return <p className="error-text">{error}</p>
  if (results === null) return <div className="skeleton" style={{ height: 120 }} />
  if (results.length === 0) {
    return <ReleaseNotice className="album-empty" sources={answered.sources} unanswered={answered.unanswered} results={0} />
  }
  return (
    <div className="album-releases">
      <p className="album-empty">
        {results.length} release{results.length === 1 ? '' : 's'} found. Releases Mediarium would skip are marked with the reason, and you can still pick any of them.
      </p>
      <ReleaseNotice className="album-empty" sources={answered.sources} unanswered={answered.unanswered} results={results.length} />
      <div className="table-scroll">
        <ReleaseTable results={results} grabbing={grabbing} onGrab={grab} qualityLabel={releaseQuality} />
      </div>
    </div>
  )
}
