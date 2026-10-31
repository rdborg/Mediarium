import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, can, isAdmin, type ActivityEntry, type BlocklistEntry, type QueueItem, type TitleEvent, type WantedRow } from '../api'
import { useAuth } from '../AuthContext'
import Cover from '../components/Cover'
import Icon, { type IconName } from '../components/Icon'
import Loading from '../components/Loading'
import InfoTip from '../components/InfoTip'
import HelpLink from '../components/HelpLink'
import { PosterFallback } from '../components/PosterCard'
import { useToast } from '../components/Toast'
import TorrentsOffNote from '../components/TorrentsOffNote'
import { useTorrentsSwitch } from '../useTorrentsSwitch'
import { isTorrentsOffText } from '../torrentsOff'
import { formatBytes, timeAgo } from '../format'
import { useConfirm } from '../components/ConfirmProvider'
import AddDownloadDialog from '../components/AddDownloadDialog'
import BuiltInClients from '../components/BuiltInClients'
import TorrentControls from '../components/TorrentControls'
import DownloadClients, { UsenetClients } from '../components/DownloadClients'
import RecycleBin from '../components/RecycleBin'
import RequestsList from '../components/RequestsList'
import IssuesList from '../components/IssuesList'
import { failureHelp } from '../failureHelp'
import RootMarkerFix from '../components/RootMarkerFix'
import { lostMarkerFolder } from '../rootMarker'
import { useModules } from '../ModulesContext'
import { bulkState, groupQueue, RUNNING, stateNote, statusLabel, stillWaiting, unpackLine } from '../queueView'
import { useLive } from '../useLive'
import { anyMoving, pollEvery } from '../pollPlan'
import { etaText, nextSample, type RateSample } from '../transferRate'
import { locale, plural, t, tx, st } from '../i18n'
import { useUrlState } from '../useUrlState'
import { keptDataText, keptFileLine } from '../keptFile'
import '../activity.css'

type Tab = 'queue' | 'clients' | 'history' | 'requests' | 'reports' | 'blocklist' | 'bin'

const EVENT: Record<string, { icon: IconName; tone: string; group: string }> = {
  grabbed: { icon: 'download', tone: 'info', group: 'Downloads' },
  imported: { icon: 'check', tone: 'success', group: 'Downloads' },
  paused: { icon: 'pause', tone: 'info', group: 'Downloads' },
  stopped: { icon: 'stop', tone: 'muted', group: 'Downloads' },
  failed: { icon: 'warning', tone: 'danger', group: 'Problems' },
  blocklisted: { icon: 'ban', tone: 'danger', group: 'Problems' },
  skipped: { icon: 'stop', tone: 'muted', group: 'Downloads' },
  conflict: { icon: 'warning', tone: 'warning', group: 'Problems' },
  subtitle: { icon: 'chat', tone: 'info', group: 'Subtitles' },
  added: { icon: 'plus', tone: 'success', group: 'Library' },
  library: { icon: 'check', tone: 'info', group: 'Library' },
  removed: { icon: 'trash', tone: 'muted', group: 'Library' },
}

// What the history filter chips say; the group names above stay the keys.
const GROUP_LABEL: Record<string, string> = {
  All: t('All'),
  Downloads: t('Downloads'),
  Problems: t('Problems'),
  Subtitles: t('Subtitles'),
  Library: t('Library'),
  Other: t('Other'),
}

// A title (or album) still waiting for a release, with what the last search said.
interface Waiting {
  key: string
  kind: 'movie' | 'tv' | 'music'
  movieId?: number
  albumId?: number
  to: string
  title: string
  subtitle?: string
  lastSearch?: string
  lastSearchAt?: string
  posterUrl?: string
  year?: number
  episodes?: number // a show: how many of its episodes are waiting
}

// The titles automation is still looking for: a movie is one entry, and a show
// is one entry however many of its episodes are missing. The server already
// groups them like that (one row per show), a page at a time.
function groupWanted(wanted: WantedRow[]): Waiting[] {
  const out: Waiting[] = []
  const shows = new Map<number, Waiting>()
  for (const w of wanted) {
    if (w.kind === 'show') {
      out.push({
        key: `show-${w.seriesId}`,
        kind: 'tv',
        to: `/series/${w.seriesId}`,
        title: w.title,
        subtitle: w.subtitle,
        lastSearch: w.lastSearch,
        lastSearchAt: w.lastSearchAt,
        posterUrl: w.posterUrl,
        year: w.year,
        episodes: w.episodes ?? 1,
      })
      continue
    }
    if (w.kind === 'movie') {
      out.push({
        key: `movie-${w.id}`,
        kind: 'movie',
        movieId: w.movieId,
        to: w.tmdbId ? `/title/${w.tmdbId}` : '/wanted',
        title: w.title,
        subtitle: w.subtitle,
        lastSearch: w.lastSearch,
        lastSearchAt: w.lastSearchAt,
        posterUrl: w.posterUrl,
        year: w.year,
      })
      continue
    }
    const seen = w.seriesId ? shows.get(w.seriesId) : undefined
    if (seen) {
      seen.episodes = (seen.episodes ?? 1) + 1
      continue
    }
    const entry: Waiting = {
      key: `show-${w.seriesId}`,
      kind: 'tv',
      to: `/series/${w.seriesId}`,
      title: w.title,
      subtitle: w.subtitle,
      lastSearch: w.lastSearch,
      lastSearchAt: w.lastSearchAt,
      posterUrl: w.posterUrl,
      year: w.year,
      episodes: 1,
    }
    if (w.seriesId) shows.set(w.seriesId, entry)
    out.push(entry)
  }
  return out
}

// How many waiting titles Activity reads: enough for the 20 it lists, with
// room for the ones that are downloading already and so are left out.
const WAITING_READ = 60

// The most downloads one bulk action may name (bulkQueueMax on the server).
const BULK_QUEUE_MAX = 200

const itemLink = (q: QueueItem) => (q.bookId ? `/book/${q.bookId}` : q.albumId ? `/music/album/${q.albumId}` : q.seriesId ? `/series/${q.seriesId}` : q.tmdbId ? `/title/${q.tmdbId}` : undefined)

// What a button press is doing right now, so only that button says so.
type Doing = 'pause' | 'resume' | 'stop' | 'other'

interface RowProps {
  q: QueueItem
  admin: boolean
  busy: Doing | null
  act: (item: QueueItem, fn: () => Promise<unknown>, done: string, doing?: Doing) => Promise<void>
  stop: (item: QueueItem) => Promise<void>
  remove: (item: QueueItem) => Promise<void>
  // Picking several downloads at once (administrators): whether this one is
  // picked, and the way to change that. Left out where picking isn't offered.
  picked?: boolean
  onPick?: (id: number, on: boolean) => void
}

// One download. A separate component (not one made inside the page) so a
// refresh every few seconds updates the row in place instead of building it
// again, which is what made rows flicker and jump.
// Why a download failed and what to try, folded away until asked for.
function FailureTips({ q, link }: { q: QueueItem; link?: string | null }) {
  const help = failureHelp(q.error, q.protocol)
  if (!help) return null
  return (
    <details className="qhelp">
      <summary>{t('Why did this happen, and what can I do?')}</summary>
      <p>{help.why}</p>
      <ul>
        {help.tips.map((tip) => (
          <li key={tip}>{tip}</li>
        ))}
      </ul>
      {link && (
        <p>
          {tx('{open} to choose another release yourself or change its quality profile.', { open: <Link to={link}>{t('Open the title')}</Link> })}
        </p>
      )}
    </details>
  )
}

// How fast a running download goes and how long is left, from the progress
// seen on this page between refreshes.
function useTransferRate(q: QueueItem): { rate: number; etaSec: number } | null {
  const [sample, setSample] = useState<RateSample | null>(null)
  const last = useRef<RateSample | null>(null)
  // Unpacking is timed from the bytes of the archive read so far (one archive;
  // with several the figures start over for each).
  const unpacking = q.status === 'importing' && !!q.unpack && q.unpack.totalBytes > 0 && q.unpack.count === 1
  const running = (q.status === 'downloading' && q.sizeBytes > 0) || unpacking
  const size = unpacking ? q.unpack!.totalBytes : q.sizeBytes
  const done = unpacking ? q.unpack!.doneBytes : (q.sizeBytes * q.progressPct) / 100
  useEffect(() => {
    if (!running) {
      last.current = null
      setSample(null)
      return
    }
    last.current = nextSample(last.current, done, Date.now())
    setSample(last.current)
  }, [running, done])
  if (!running || !sample || sample.rate <= 0) return null
  return { rate: sample.rate, etaSec: (size - done) / sample.rate }
}

// "Details" under a download: where it came from, where it goes, and what
// happened to it, read from the title's own history when the box is opened.
function QueueDetails({ q, admin }: { q: QueueItem; admin: boolean }) {
  const [open, setOpen] = useState(false)
  const [events, setEvents] = useState<TitleEvent[] | null>(null)
  const load = () => {
    if (events !== null) return
    const ask = q.albumId ? api.albumEvents(q.albumId) : q.seriesId ? api.seriesEvents(q.seriesId) : q.movieId ? api.movieEvents(q.movieId) : Promise.resolve([] as TitleEvent[])
    ask.then((all) => setEvents(all.filter((e) => !q.addedAt || e.at >= q.addedAt).slice(0, 12))).catch(() => setEvents([]))
  }
  return (
    <details
      className="qdetails"
      onToggle={(e) => {
        setOpen(e.currentTarget.open)
        if (e.currentTarget.open) load()
      }}
    >
      <summary>{t('Details')}</summary>
      <dl>
        <dt>{t('Release')}</dt>
        <dd>{q.releaseTitle}</dd>
        {q.indexerName && (
          <>
            <dt>{t('Indexer')}</dt>
            <dd>{q.indexerName}</dd>
          </>
        )}
        <dt>{t('Source')}</dt>
        <dd>{q.protocol === 'torrent' ? 'Torrent' : 'Usenet'}</dd>
        {q.category && (
          <>
            <dt>{t('Category')}</dt>
            <dd>{q.category}</dd>
          </>
        )}
        {q.peers !== undefined && (
          <>
            <dt>{t('Connected to')}</dt>
            <dd>{t('{peers} peers, {seeds} of them seeds', { peers: q.peers, seeds: q.seeds ?? 0 })}</dd>
          </>
        )}
        {q.sizeBytes > 0 && (
          <>
            <dt>{t('Size')}</dt>
            <dd>{formatBytes(q.sizeBytes)}</dd>
          </>
        )}
        {q.addedAt && (
          <>
            <dt>{t('Added')}</dt>
            <dd>{new Date(q.addedAt).toLocaleString(locale())}</dd>
          </>
        )}
        {q.completedAt && (
          <>
            <dt>{t('Finished')}</dt>
            <dd>{new Date(q.completedAt).toLocaleString(locale())}</dd>
          </>
        )}
        {q.destPath && (
          <>
            <dt>{t('Goes to')}</dt>
            <dd className="mono">{q.destPath}</dd>
          </>
        )}
        {q.keptFile && (
          <>
            <dt>{t('Kept file')}</dt>
            <dd className="mono">{q.keptFile}</dd>
          </>
        )}
        {q.keptData && (
          <>
            <dt>{t('Downloaded files')}</dt>
            <dd>{keptDataText(q.keptData)}</dd>
          </>
        )}
      </dl>
      {admin && q.protocol === 'torrent' && ['queued', 'downloading', 'importing', 'paused', 'completed'].includes(q.status) && <TorrentControls q={q} open={open} />}
      {events !== null && events.length > 0 && (
        <ol className="qsteps">
          {[...events].reverse().map((e, i) => (
            <li key={i} className={e.level !== 'info' ? e.level : undefined}>
              <time>{new Date(e.at).toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' })}</time>
              <span>{e.message}</span>
            </li>
          ))}
        </ol>
      )}
    </details>
  )
}

function QueueRow({ q, admin, busy, act, stop, remove, picked, onPick }: RowProps) {
  const me = useAuth().user
  const transfer = useTransferRate(q)
  const link = itemLink(q)
  const running = RUNNING.has(q.status)
  const working = busy !== null || !!q.pending
  const animated = (q.status === 'downloading' || q.status === 'importing') && !q.waiting
  const showBar = (running && q.status !== 'queued') || q.status === 'conflict' || q.status === 'paused'
  const barTone = q.status === 'failed' ? 'bad' : q.status === 'conflict' ? 'warn' : q.status === 'paused' ? 'paused' : ''
  const note = stateNote(q)
  return (
    <div className={`qrow st-${q.status}${q.status === 'failed' ? ' failed' : ''} kind-${q.bookId ? 'book' : q.albumId ? 'music' : q.seriesId ? 'tv' : 'movie'}`}>
      <div className="qthumb">
        {q.albumId ? <Cover src={q.posterUrl} thumb /> : q.posterUrl ? <img src={q.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}
        {admin && onPick && (
          <label className="qpick" title={t('Pick this download')}>
            <input type="checkbox" checked={!!picked} onChange={(e) => onPick(q.id, e.target.checked)} aria-label={t('Pick {title}', { title: q.title })} />
          </label>
        )}
      </div>
      <div className="qmain">
        <div className="qtitle">
          {link ? <Link to={link}>{q.title}</Link> : <span>{q.title}</span>}
          {q.subtitle && <span className="badge">{q.subtitle}</span>}
          <span className={`badge ${q.status === 'completed' ? 'downloaded' : q.status}`}>{statusLabel(q)}</span>
          <span className="qproto" title={q.protocol === 'torrent' ? 'Torrent' : 'Usenet'}>
            {q.protocol === 'torrent' ? 'torrent' : 'usenet'}
          </span>
          {q.category && (
            <span className="badge qcat" title={t('Torrent category')}>
              {q.category}
            </span>
          )}
        </div>
        <div className="qrelease">{q.releaseTitle}</div>
        {(q.resolution || q.source || q.codec || q.language) && (
          <div className="qspecs">
            {[q.resolution, q.source, q.codec].filter(Boolean).map((x) => (
              <span key={x} className="badge qspec">
                {x}
              </span>
            ))}
            {q.language && (
              <span className="badge qspec" title={t('The audio language the release name gives')}>
                {q.language}
              </span>
            )}
          </div>
        )}
        {showBar && (
          <div className="qprogress">
            <div className={`bar ${animated && !q.pending ? 'active' : ''} ${barTone}`}>
              <span style={{ width: `${Math.max(2, q.progressPct)}%` }} />
            </div>
            <small className="sr-only">
              {q.sizeBytes ? t('{pct}% of {size}', { pct: q.progressPct.toFixed(0), size: formatBytes(q.sizeBytes) }) : `${q.progressPct.toFixed(0)}%`}
            </small>
          </div>
        )}
        {q.status === 'importing' && q.unpack && <div className="qspeed">{unpackLine(q.unpack)}</div>}
        {transfer && (
          <div className="qspeed">
            {formatBytes(transfer.rate)}/s{etaText(transfer.etaSec) ? ' · ' + t('about {time} left', { time: etaText(transfer.etaSec) }) : ''}
          </div>
        )}
        {note && <div className="qnote">{note}</div>}
        {q.note && <div className="qnote">{st(q.note)}</div>}
        {q.error && (q.status === 'completed' ? <div className="qnote">{st(q.error)}</div> : <div className="error-text qerror">{st(q.error)}</div>)}
        {q.status === 'completed' && q.keptFile && <div className="qnote mono">{keptFileLine(q.keptFile)}</div>}
        {q.status === 'completed' && q.keptData && <div className="qnote">{keptDataText(q.keptData)}</div>}
        {q.status === 'failed' && isTorrentsOffText(q.error) && <TorrentsOffNote failed />}
        {q.status === 'failed' && admin && lostMarkerFolder(q.error) && <RootMarkerFix path={lostMarkerFolder(q.error)!} />}
        {q.status === 'failed' && <FailureTips q={q} link={link} />}
        {q.status === 'conflict' && q.destPath && <div className="qrelease">{t('A file already exists at {path}', { path: q.destPath })}</div>}
        <QueueDetails q={q} admin={admin} />
        {(q.status === 'completed' || q.status === 'failed' || q.status === 'stopped') && (
          <small className="qwhen">
            {q.status === 'completed'
              ? q.error
                ? t('Finished {when}', { when: timeAgo(q.completedAt || q.addedAt) })
                : t('Imported {when}', { when: timeAgo(q.completedAt || q.addedAt) })
              : q.status === 'stopped'
                ? t('Stopped {when}', { when: timeAgo(q.completedAt || q.addedAt) })
                : t('Failed {when}', { when: timeAgo(q.completedAt || q.addedAt) })}
            {q.sizeBytes ? ` · ${formatBytes(q.sizeBytes)}` : ''}
          </small>
        )}
      </div>
      {showBar ? (
        <div className="qmeter" aria-hidden="true">
          <b>
            {q.progressPct.toFixed(0)}
            <small>%</small>
          </b>
          {q.sizeBytes > 0 && <span>{formatBytes(q.sizeBytes)}</span>}
        </div>
      ) : q.status === 'queued' && q.queuePosition ? (
        <div className="qmeter in-line" aria-hidden="true">
          <b>
            <small>#</small>
            {q.queuePosition}
          </b>
          {q.sizeBytes > 0 && <span>{formatBytes(q.sizeBytes)}</span>}
        </div>
      ) : null}
      <div className="row-actions qactions">
        {q.status === 'conflict' && admin && (
          <>
            <button className="primary btn-sm" disabled={working} onClick={() => void act(q, () => api.resolveConflict(q.id, true), t('File replaced.'))}>
              {t('Overwrite')}
            </button>
            <button className="btn-sm" disabled={working} onClick={() => void act(q, () => api.resolveConflict(q.id, false), t('Kept the existing file.'))}>
              {t('Skip')}
            </button>
          </>
        )}
        {q.status === 'queued' && (q.queuePosition ?? 0) > 1 && admin && (
          <button className="btn-sm btn-with-icon" disabled={working} title={t('Start this one before the others that are waiting')} onClick={() => void act(q, () => api.frontQueueItem(q.id), t('Moved to the front of the line.'))}>
            <Icon name="chevron-up" size={15} /> {t('To the front')}
          </button>
        )}
        {running && admin && (
          <>
            <button className="btn-sm btn-with-icon" disabled={working} title={t('Pause the transfer and keep what is saved')} onClick={() => void act(q, () => api.pauseQueueItem(q.id), t('Paused.'), 'pause')}>
              <Icon name="pause" size={15} /> {busy === 'pause' || q.pending === 'pausing' ? t('Pausing…') : t('Pause')}
            </button>
            <button className="btn-sm btn-with-icon" disabled={working} title={t('Cancel this download')} onClick={() => void stop(q)}>
              <Icon name="stop" size={15} /> {busy === 'stop' || q.pending === 'stopping' ? t('Stopping…') : t('Stop')}
            </button>
          </>
        )}
        {q.status === 'paused' && admin && (
          <>
            <button className="primary btn-sm btn-with-icon" disabled={working} title={t('Carry on from what is saved')} onClick={() => void act(q, () => api.resumeQueueItem(q.id), t('Resuming.'), 'resume')}>
              <Icon name="play" size={15} /> {busy === 'resume' ? t('Resuming…') : t('Resume')}
            </button>
            <button className="btn-sm btn-with-icon" disabled={working} title={t('Cancel this download')} onClick={() => void stop(q)}>
              <Icon name="stop" size={15} /> {busy === 'stop' || q.pending === 'stopping' ? t('Stopping…') : t('Stop')}
            </button>
          </>
        )}
        {q.status === 'importing' && q.waiting && can(me, 'retry') && (
          <button className="btn-sm btn-with-icon" disabled={working} title={t('Try the import again now, from the files already downloaded')} onClick={() => void act(q, () => api.retryQueueItem(q.id), t('Trying the import again.'))}>
            <Icon name="refresh" size={15} /> {t('Retry now')}
          </button>
        )}
        {(q.status === 'failed' || q.status === 'stopped') && can(me, 'retry') && (
          <button className="btn-sm btn-with-icon" disabled={working} onClick={() => void act(q, () => api.retryQueueItem(q.id), t('Trying that release again.'))}>
            <Icon name="refresh" size={15} /> {t('Retry')}
          </button>
        )}
        {q.status === 'failed' && admin && (
          <button className="btn-sm btn-with-icon" disabled={working} title={t('Never use this release again and look for another one')} onClick={() => void act(q, () => api.blocklistQueueItem(q.id), t('Blocklisted. Looking for another release.'))}>
            <Icon name="ban" size={15} /> {t('Blocklist & search again')}
          </button>
        )}
        {link && (
          <Link className="icon-btn" to={link} title={t('Open')} aria-label={t('Open')}>
            <Icon name="open" size={17} />
          </Link>
        )}
        {(q.status === 'completed' || q.status === 'failed' || q.status === 'stopped' || q.status === 'paused') && admin && (
          <button className="icon-btn danger" title={t('Remove from this list')} aria-label={t('Remove from this list')} disabled={working} onClick={() => void remove(q)}>
            <Icon name="trash" size={17} />
          </button>
        )}
      </div>
    </div>
  )
}

// Activity: what is downloading right now (with the actions you actually want
// on each entry), a readable history of what Mediarium has done, and the
// blocklist of releases it will not grab again.
export default function Queue() {
  const confirm = useConfirm()
  const toast = useToast()
  const { grabError } = useTorrentsSwitch()
  // Members can watch the queue and retry, but pausing, stopping, removing,
  // blocklisting and resolving conflicts are for administrators.
  const admin = isAdmin(useAuth().user)
  // The tab, the history group and the history search live in the address, so Back returns to them.
  const [ui, patchUi] = useUrlState({ tab: 'queue', group: 'All', q: '' })
  // A link to a tab this account cannot see (the administrator's tabs) opens the queue.
  const tabsHere = admin ? (['queue', 'clients', 'history', 'requests', 'reports', 'blocklist', 'bin'] as const) : (['queue', 'history', 'requests', 'reports'] as const)
  const tab: Tab = tabsHere.find((x) => x === ui.tab) ?? 'queue'
  const setTab = (v: Tab) => patchUi({ tab: v })
  const [pendingRequests, setPendingRequests] = useState(0)
  const [addingByHand, setAddingByHand] = useState(false)
  const [openReports, setOpenReports] = useState(0)
  const [reportsOn, setReportsOn] = useState(false)
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [activity, setActivity] = useState<ActivityEntry[]>([])
  // History: how many lines to show, and the words to look for.
  const [actLimit, setActLimit] = useState(100)
  const actQuery = ui.q
  const setActQuery = (v: string) => patchUi({ q: v })
  const [blocklist, setBlocklist] = useState<BlocklistEntry[]>([])
  const [blocklistTotal, setBlocklistTotal] = useState(0)
  const [blLimit, setBlLimit] = useState(100)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<{ id: number; doing: Doing } | null>(null)
  const [bulkBusy, setBulkBusy] = useState<'pause' | 'resume' | null>(null)
  // The downloads picked with the checkboxes, for doing one thing to all of them.
  const [picked, setPicked] = useState<ReadonlySet<number>>(new Set())
  const [pickBusy, setPickBusy] = useState(false)
  const group = ui.group
  const setGroup = (v: string) => patchUi({ group: v })
  // Titles still waiting for a release, with why, so an empty queue explains itself.
  const musicOn = useModules().on('music')
  const [allWaiting, setWaiting] = useState<Waiting[]>([])
  // Only the first titles are read (the list shows 20); waitingMore is how many more there are.
  const [waitingMore, setWaitingMore] = useState(0)
  const loadWaiting = useCallback(() => {
    Promise.all([
      api.getWantedPage('missing', { limit: WAITING_READ }).catch(() => null),
      musicOn ? api.musicWanted('missing').catch(() => []) : Promise.resolve([]),
    ]).then(([wanted, albums]) => {
      setWaitingMore(wanted ? wanted.total - wanted.rows.length : 0)
      setWaiting([
        ...groupWanted(wanted?.rows ?? []),
        ...albums.map((a) => ({
          key: `album-${a.id}`,
          kind: 'music' as const,
          albumId: a.id,
          to: `/music/artist/${a.artistId}#album-${a.id}`,
          title: `${a.artistName} – ${a.title}`,
          lastSearch: a.lastSearch,
          lastSearchAt: a.lastSearchAt,
          posterUrl: a.coverUrl,
          year: a.year || undefined,
        })),
      ])
    })
  }, [musicOn])
  useEffect(loadWaiting, [loadWaiting])
  // The Reports tab shows once reports are on (or the account already made some).
  useEffect(() => {
    api
      .listIssues()
      .then((r) => {
        setOpenReports(r.open)
        setReportsOn(r.enabled)
      })
      .catch(() => undefined)
  }, [])
  useLive(loadWaiting, 30000)

  // The history search waits for a pause in typing, and only the answer to the latest request is used.
  const [searchedQ, setSearchedQ] = useState(actQuery)
  useEffect(() => {
    const timer = window.setTimeout(() => setSearchedQ(actQuery), 300)
    return () => window.clearTimeout(timer)
  }, [actQuery])
  const latest = useRef(0)
  const load = useCallback(async () => {
    const mine = ++latest.current
    try {
      // Only the queue needs the fast clock; the history and the blocklist
      // are read while their own tab is open (the blocklist count is kept
      // fresh more slowly by loadBlocklistCount below).
      const [q, a, b] = await Promise.all([
        api.listQueue(),
        tab === 'history' ? api.listActivity({ limit: actLimit, q: searchedQ.trim() }) : Promise.resolve(null),
        admin && tab === 'blocklist' ? api.listBlocklist(blLimit) : Promise.resolve(null),
      ])
      if (mine !== latest.current) return
      setQueue(q)
      if (a) setActivity(a)
      if (b) {
        setBlocklist((cur) => (JSON.stringify(cur) === JSON.stringify(b.items) ? cur : b.items)) // an unchanged list is not drawn again
        setBlocklistTotal(b.total)
      }
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoaded(true)
    }
  }, [admin, tab, actLimit, blLimit, searchedQ])

  useEffect(() => {
    void load()
  }, [load])
  // Every 3 seconds while a download moves, every 10 when nothing does.
  useLive(load, pollEvery(anyMoving(queue), 3000, 10000))

  // The number next to the Blocklist tab.
  const loadBlocklistCount = useCallback(() => {
    if (!admin) return
    api.listBlocklist(1).then((b) => setBlocklistTotal(b.total)).catch(() => undefined)
  }, [admin])
  useEffect(loadBlocklistCount, [loadBlocklistCount])
  useLive(loadBlocklistCount, 30000)

  const act = useCallback(
    async (item: QueueItem, fn: () => Promise<unknown>, done: string, doing: Doing = 'other') => {
      setBusy({ id: item.id, doing })
      try {
        await fn()
        toast.success(done)
        await load()
      } catch (e) {
        grabError(e)
      } finally {
        setBusy(null)
      }
    },
    [load, toast, grabError],
  )

  // Stop asks first, and offers to delete the partly downloaded files (on by
  // default). Only files in the downloads folder are ever deleted.
  const stop = useCallback(
    async (item: QueueItem) => {
      const hasFiles = item.keptFiles || item.progressPct > 0
      const r = await confirm({
        title: t('Stop this download?'),
        body: (
          <p>
            {tx('{title} stops downloading and goes back to waiting for a release. You can try this release again from the list.', { title: <strong>{item.title}</strong> })}
          </p>
        ),
        confirmLabel: t('Stop download'),
        danger: true,
        option: hasFiles
          ? { label: t('Also delete the partly downloaded files'), hint: t('Your library is not touched.'), defaultChecked: true }
          : undefined,
      })
      if (!r) return
      const del = hasFiles && r.checked
      await act(item, () => api.stopQueueItem(item.id, del), del ? t('Stopped. The partial download was deleted.') : t('Stopped.'), 'stop')
    },
    [act, confirm],
  )

  const remove = useCallback(
    async (item: QueueItem) => {
      let del = false
      if (item.status === 'paused' || item.keptFiles) {
        const r = await confirm({
          title: t('Remove this download?'),
          body: <p>{item.status === 'paused' ? t('It will stop and leave the list.') : t('It will leave the list.')}</p>,
          confirmLabel: t('Remove'),
          danger: true,
          option: item.keptFiles
            ? { label: t('Also delete the partly downloaded files'), hint: t('Your library is not touched.'), defaultChecked: true }
            : undefined,
        })
        if (!r) return
        del = item.keptFiles === true && r.checked
      }
      await act(item, () => api.deleteQueueItem(item.id, del), t('Removed from the list.'))
    },
    [act, confirm],
  )

  // Clearing finished items and editing the blocklist: one at a time, so a second press can't send the same thing twice.
  const [tidying, setTidying] = useState(false)
  async function clearFinished() {
    if (tidying) return
    setTidying(true)
    try {
      const r = await api.clearFinishedQueue()
      toast.success(plural(r.removed, 'Cleared {n} finished item.', 'Cleared {n} finished items.'))
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setTidying(false)
    }
  }

  async function pauseAll() {
    setBulkBusy('pause')
    try {
      const r = await api.pauseAllQueue()
      toast.success(r.paused === 0 ? t('Nothing was running.') : plural(r.paused, 'Paused {n} download.', 'Paused {n} downloads.'))
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBulkBusy(null)
    }
  }

  async function resumeAll() {
    setBulkBusy('resume')
    try {
      const r = await api.resumeAllQueue()
      if (r.skipped > 0) toast.error(`${t('{resumed} resumed, {skipped} could not start.', { resumed: r.resumed, skipped: r.skipped })} ${r.problem ?? ''}`.trim())
      else toast.success(r.resumed === 0 ? t('Nothing was paused.') : plural(r.resumed, 'Resumed {n} download.', 'Resumed {n} downloads.'))
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBulkBusy(null)
    }
  }

  const sections = useMemo(() => groupQueue(queue), [queue])
  const waiting = useMemo(() => stillWaiting(allWaiting, queue), [allWaiting, queue])
  const waitingCount = waiting.length + waitingMore
  const bulk = useMemo(() => bulkState(queue), [queue])
  const { inProgress, failed, decision, stopped, finished: completed } = sections
  const needsAttention = inProgress.length + decision.length + failed.length + stopped.length
  const downloading = inProgress.filter((q) => q.status === 'downloading' || q.status === 'importing')
  const inLine = inProgress.filter((q) => q.status === 'queued').length
  const problems = failed.length + stopped.length + decision.length
  const toGo = downloading.reduce((sum, q) => sum + (q.sizeBytes * (100 - q.progressPct)) / 100, 0)

  const groups = useMemo(() => ['All', ...Array.from(new Set(Object.values(EVENT).map((e) => e.group)))], [])
  const shownActivity = activity.filter((a) => group === 'All' || (EVENT[a.eventType]?.group ?? 'Other') === group)

  // What was picked and has since finished or left the list is simply not counted.
  // Downloads that can be ticked: the ones in progress, failed and stopped.
  const pickable = useMemo(() => [...inProgress, ...failed, ...stopped], [inProgress, failed, stopped])
  const livePicked = useMemo(() => {
    if (picked.size === 0) return picked
    const live = new Set(pickable.map((q) => q.id))
    return new Set([...picked].filter((id) => live.has(id)))
  }, [picked, pickable])

  const pick = useCallback((id: number, on: boolean) => {
    setPicked((cur) => {
      const next = new Set(cur)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  }, [])
  async function pickedDo(action: 'pause' | 'resume' | 'stop' | 'retry' | 'front' | 'remove' | 'blocklist') {
    // In the order they are shown, so "to the front" keeps the line's own order.
    // Retry only goes to the failed and stopped ones, so "Pick all" then Retry
    // leaves what is running alone instead of reporting it as refused.
    const ids = pickable
      .filter((q) => livePicked.has(q.id) && ((action !== 'retry' && action !== 'blocklist') || q.status === 'failed' || q.status === 'stopped'))
      .map((q) => q.id)
    if (ids.length === 0 || pickBusy) return
    let deleteFiles = false
    if (action === 'blocklist') {
      const r = await confirm({
        title: plural(ids.length, 'Blocklist this release?', 'Blocklist these {n} releases?'),
        body: <p>{t('They are never grabbed again, and Mediarium looks for another release for each title.')}</p>,
        confirmLabel: t('Blocklist releases'),
        danger: true,
      })
      if (!r) return
    }
    if (action === 'stop' || action === 'remove') {
      const anyFiles = pickable.some((q) => livePicked.has(q.id) && (q.keptFiles || q.progressPct > 0))
      const removing = action === 'remove'
      const r = await confirm({
        title: removing ? plural(ids.length, 'Remove this download from the list?', 'Remove these {n} downloads from the list?') : plural(ids.length, 'Stop this download?', 'Stop these {n} downloads?'),
        body: <p>{removing ? t('They leave the list. A download that is still running or waiting in line is not removed: pause or stop it first.') : t('They stop downloading and go back to waiting for a release. You can try them again from the list.')}</p>,
        confirmLabel: removing ? t('Remove downloads') : t('Stop downloads'),
        danger: true,
        option: anyFiles ? { label: t('Also delete the partly downloaded files'), hint: t('Your library is not touched.'), defaultChecked: true } : undefined,
      })
      if (!r) return
      deleteFiles = anyFiles && r.checked
    }
    setPickBusy(true)
    try {
      // The server takes at most BULK_QUEUE_MAX at a time (bulkQueueMax in
      // internal/api/queue_control.go), so a bigger pick goes in batches. For
      // "to the front" the last batch goes first: each batch lands in front of
      // the one before, which keeps the order of the whole pick.
      const batches: number[][] = []
      for (let i = 0; i < ids.length; i += BULK_QUEUE_MAX) batches.push(ids.slice(i, i + BULK_QUEUE_MAX))
      if (action === 'front') batches.reverse()
      let done = 0
      const failed: { id: number; error: string }[] = []
      for (const batch of batches) {
        const r = await api.bulkQueue(batch, action, deleteFiles)
        done += r.done ?? 0
        failed.push(...(r.failed ?? []))
      }
      if (failed.length > 0) toast.error(`${t('{done} done.', { done })} ${t('{n} could not be changed: {why}', { n: failed.length, why: failed[0].error })}`)
      else toast.success(plural(done, '{n} download changed.', '{n} downloads changed.'))
      setPicked(new Set())
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setPickBusy(false)
    }
  }

  const rows = (list: QueueItem[], pickable = false) =>
    list.map((q) => (
      <QueueRow
        key={q.id}
        q={q}
        admin={admin}
        busy={busy?.id === q.id ? busy.doing : null}
        act={act}
        stop={stop}
        remove={remove}
        picked={pickable ? livePicked.has(q.id) : undefined}
        onPick={pickable ? pick : undefined}
      />
    ))

  return (
    <div className="activity-page">
      <div className="page-header">
        <h1>{t('Activity')}</h1>
        {admin && (
          <div className="queue-actions">
            <Link className="btn-link" to="/import/manual">
              <Icon name="folder" size={15} /> {t('Import a file by hand')}
            </Link>
            <button className="btn-add-download btn-with-icon" onClick={() => setAddingByHand(true)}>
              <Icon name="plus" size={15} /> {t('Add a download by hand')}
            </button>
          </div>
        )}
        <div className="seg">
          <button aria-pressed={tab === 'queue'} className={tab === 'queue' ? 'active' : ''} onClick={() => setTab('queue')}>
            {t('Queue')} <small>({needsAttention})</small>
          </button>
          {admin && (
            <button aria-pressed={tab === 'clients'} className={tab === 'clients' ? 'active' : ''} onClick={() => setTab('clients')} title={t('The built-in Usenet downloader and torrent client, and any download client or debrid account you connected')}>
              {t('Download clients')}
            </button>
          )}
          <button aria-pressed={tab === 'history'} className={tab === 'history' ? 'active' : ''} onClick={() => setTab('history')}>
            {t('History')} {completed.length > 0 && <small>({completed.length})</small>}
          </button>
          <button aria-pressed={tab === 'requests'} className={tab === 'requests' ? 'active' : ''} onClick={() => setTab('requests')}>
            {t('Requests')} {pendingRequests > 0 && <small>({pendingRequests})</small>}
          </button>
          {(admin || reportsOn || openReports > 0) && (
            <button aria-pressed={tab === 'reports'} className={tab === 'reports' ? 'active' : ''} onClick={() => setTab('reports')}>
              {t('Reports')} {openReports > 0 && <small>({openReports})</small>}
            </button>
          )}
          {admin && (
            <button aria-pressed={tab === 'blocklist'} className={tab === 'blocklist' ? 'active' : ''} onClick={() => setTab('blocklist')}>
              {t('Blocklist')} <small>({blocklistTotal})</small>
            </button>
          )}
          {admin && (
            <button aria-pressed={tab === 'bin'} className={tab === 'bin' ? 'active' : ''} onClick={() => setTab('bin')}>
              {t('Recycle bin')}
            </button>
          )}
        </div>
      </div>
      {error && (
        <p className="error-text">
          {error}{' '}
          <button type="button" className="link-btn" onClick={() => void load()}>
            {t('Try again')}
          </button>
        </p>
      )}

      {tab === 'queue' && (
        <>
          {!loaded && <Loading height={96} onRetry={() => void load()} />}
          {loaded && !error && (
            <div className="act-board">
              <div className="ab-cell">
                <a href="#act-progress" className={`ab-slot ab-live${downloading.length ? ' on' : ''}`}>
                  <b>{downloading.length}</b>
                  <span>{t('Downloading')}</span>
                  {toGo > 0 && <small>{t('{size} to go', { size: formatBytes(toGo) })}</small>}
                </a>
                <InfoTip className="ab-tip" text={t('Downloads running right now, including ones that are being unpacked and moved into your library. The small number is how much is still to come.')} />
              </div>
              <div className="ab-cell">
                <a href="#act-progress" className={`ab-slot ab-line${inLine ? ' on' : ''}`}>
                  <b>{inLine}</b>
                  <span>{t('Waiting in line')}</span>
                </a>
                <InfoTip className="ab-tip" text={t('Downloads that are ready but waiting their turn, because only a few can run at the same time.')} />
              </div>
              <div className="ab-cell">
                <a href="#act-problems" className={`ab-slot ab-bad${problems ? ' on' : ''}`}>
                  <b>{problems}</b>
                  <span>{t('Problems')}</span>
                </a>
                <InfoTip className="ab-tip" text={t('Downloads that failed, were stopped, or need you to decide something, such as a file that already exists. Open one to retry it or remove it.')} />
              </div>
              <div className="ab-cell">
                <a href="#act-waiting" className={`ab-slot ab-want${waitingCount ? ' on' : ''}`}>
                  <b>{waitingCount}</b>
                  <span>{t('Waiting for a release')}</span>
                </a>
                <InfoTip className="ab-tip" text={t('Titles you are monitoring that have no release yet. Mediarium keeps searching and starts a download when one turns up.')} />
              </div>
              <div className="ab-cell">
                <button type="button" className={`ab-slot ab-done${completed.length ? ' on' : ''}`} onClick={() => setTab('history')}>
                  <b>{completed.length}</b>
                  <span>{t('Downloaded')}</span>
                </button>
                <InfoTip className="ab-tip" text={t('Downloads that finished and were imported. Press the box to see them under History.')} />
              </div>
            </div>
          )}
          {loaded && !error && needsAttention === 0 && (
            <div className="act-quiet">
              <Icon name="download" size={20} />
              <p>
                {t('Nothing is downloading right now.')} {waitingCount > 0 ? t('The titles below are waiting for a release.') : t('Add something and it shows up here.')}
                {completed.length > 0 && (
                  <>
                    {' '}
                    <button className="link-btn" onClick={() => setTab('history')}>
                      {plural(completed.length, '{n} finished download is in History.', '{n} finished downloads are in History.')}
                    </button>
                  </>
                )}
              </p>
            </div>
          )}

          {admin && livePicked.size > 0 && (
            <div className="pick-bar" role="region" aria-label={t('Picked downloads')}>
              <strong>{plural(livePicked.size, '{n} picked', '{n} picked')}</strong>
              <button className="btn-sm" disabled={pickBusy} onClick={() => void pickedDo('front')}>
                {t('To the front')}
              </button>
              <button className="btn-sm btn-with-icon" disabled={pickBusy} onClick={() => void pickedDo('pause')}>
                <Icon name="pause" size={14} /> {t('Pause')}
              </button>
              <button className="btn-sm btn-with-icon" disabled={pickBusy} onClick={() => void pickedDo('resume')}>
                <Icon name="play" size={14} /> {t('Resume')}
              </button>
              <button className="btn-sm btn-with-icon" disabled={pickBusy} onClick={() => void pickedDo('stop')}>
                <Icon name="stop" size={14} /> {t('Stop')}
              </button>
              <button className="btn-sm btn-with-icon" disabled={pickBusy} onClick={() => void pickedDo('retry')}>
                <Icon name="refresh" size={14} /> {t('Retry')}
              </button>
              <button className="btn-sm btn-with-icon" disabled={pickBusy} onClick={() => void pickedDo('blocklist')}>
                <Icon name="ban" size={14} /> {t('Blocklist')}
              </button>
              <button className="btn-sm btn-with-icon danger-ghost" disabled={pickBusy} onClick={() => void pickedDo('remove')}>
                <Icon name="trash" size={14} /> {t('Remove')}
              </button>
              <button className="btn-sm" disabled={pickBusy} onClick={() => setPicked(new Set(pickable.map((q) => q.id)))}>
                {t('Pick all')}
              </button>
              <button className="btn-sm" disabled={pickBusy} onClick={() => setPicked(new Set(pickable.filter((q) => q.status === 'paused').map((q) => q.id)))}>
                {t('Pick paused')}
              </button>
              <button className="btn-sm" disabled={pickBusy} onClick={() => setPicked(new Set(pickable.filter((q) => q.status === 'failed' || q.status === 'stopped').map((q) => q.id)))}>
                {t('Pick problems')}
              </button>
              <button className="btn-sm" disabled={pickBusy} onClick={() => setPicked(new Set())}>
                {t('Clear')}
              </button>
            </div>
          )}
          {inProgress.length > 0 && (
            <section id="act-progress" style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>
                  {t('In progress')} <small className="count">({inProgress.length})</small>
                </h2>
                {admin && (
                  <span className="rail-actions">
                    <button className="btn-sm btn-with-icon" disabled={!bulk.canPause || bulkBusy !== null} onClick={() => void pauseAll()}>
                      <Icon name="pause" size={14} /> {bulkBusy === 'pause' ? t('Pausing…') : t('Pause all')}
                    </button>
                    <button className="btn-sm btn-with-icon" disabled={!bulk.canResume || bulkBusy !== null} onClick={() => void resumeAll()}>
                      <Icon name="play" size={14} /> {bulkBusy === 'resume' ? t('Resuming…') : t('Resume all')}
                    </button>
                  </span>
                )}
              </div>
              {rows(inProgress, true)}
            </section>
          )}

          {failed.length > 0 && (
            <section id="act-problems" style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>{t('Failed')}</h2>
                <span>{t('retry, pick another release, or remove')}</span>
                {admin && (
                  <button className="btn-sm" onClick={() => setPicked(new Set(pickable.filter((q) => q.status === 'failed' || q.status === 'stopped').map((q) => q.id)))}>
                    {t('Pick problems')}
                  </button>
                )}
              </div>
              {rows(failed, true)}
            </section>
          )}

          {decision.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <h2 className="has-tip">
                {admin ? t('Needs your decision') : t('Waiting for an admin')}
                <InfoTip text={t('A file with the same name is already in your library. Overwrite replaces it with the new download; Skip keeps the one you have.')} />
              </h2>
              {rows(decision)}
            </section>
          )}

          {stopped.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>{t('Stopped')}</h2>
                <span>{t('retry, or remove')}</span>
              </div>
              {rows(stopped, true)}
            </section>
          )}

          {waiting.length > 0 && (
            <section id="act-waiting" className="waiting-list">
              <h2>
                {t('Waiting for a release')} <small>({waitingCount})</small>
              </h2>
              <ul>
                {waiting.slice(0, 20).map((w) => {
                  const search = w.lastSearch ? `${st(w.lastSearch.replace(/^Searched: /, ''))}${w.lastSearchAt ? ` (${timeAgo(w.lastSearchAt)})` : ''}` : t('Not searched yet.')
                  const meta = [w.year ? String(w.year) : '', w.episodes && w.episodes > 1 ? plural(w.episodes, '{n} episode waiting', '{n} episodes waiting') : w.subtitle ?? ''].filter(Boolean).join(' · ')
                  return (
                    <li key={w.key} className={`kind-${w.kind}`}>
                      <Link to={w.to} className="wait-thumb" aria-hidden="true" tabIndex={-1}>
                        {w.kind === 'music' ? <Cover src={w.posterUrl} thumb /> : w.posterUrl ? <img src={w.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}
                      </Link>
                      <div className="wait-body">
                        <Link to={w.to} className="wait-title">
                          {w.title}
                        </Link>
                        {meta && <span className="wait-meta">{meta}</span>}
                        <small className="wait-search" title={search}>
                          {search}
                        </small>
                      </div>
                    </li>
                  )
                })}
              </ul>
              {waitingCount > 20 && (
                <Link to="/wanted" className="btn btn-sm">
                  {t('See all {n} in Upcoming → Wanted', { n: waitingCount })}
                </Link>
              )}
            </section>
          )}
        </>
      )}

      {tab === 'history' && (
        <>
          {completed.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>{t('Recently downloaded')}</h2>
                {admin && (
                  <button className="btn-sm btn-with-icon" disabled={tidying} onClick={() => void clearFinished()}>
                    <Icon name="trash" size={14} /> {t('Clear finished')}
                  </button>
                )}
              </div>
              {rows(completed)}
            </section>
          )}
          <div className="act-tools">
            <div className="seg">
              {groups.map((g) => (
                <button key={g} aria-pressed={group === g} className={group === g ? 'active' : ''} onClick={() => setGroup(g)}>
                  {GROUP_LABEL[g] ?? g}
                </button>
              ))}
            </div>
            <input type="search" className="wanted-search" placeholder={t('Find in the history')} aria-label={t('Find in the history')} value={actQuery} onChange={(e) => { setActQuery(e.target.value); setActLimit(100) }} />
          </div>
          {shownActivity.length === 0 ? (
            <div className="empty-state">
              <Icon name="activity" size={44} />
              <p>{actQuery.trim() ? t('Nothing in the history matches that.') : t('No activity yet.')}</p>
            </div>
          ) : (
            <ul className="timeline">
              {shownActivity.map((a) => {
                const e = EVENT[a.eventType] ?? { icon: 'info' as IconName, tone: 'muted', group: 'Other' }
                return (
                  <li key={a.id}>
                    <span className={`tl-icon tone-${e.tone}`}>
                      <Icon name={e.icon} size={16} />
                    </span>
                    <span className="tl-msg">{a.link ? <Link to={a.link}>{st(a.message)}</Link> : st(a.message)}</span>
                    <time className="tl-time" title={new Date(a.createdAt).toLocaleString(locale())}>
                      {timeAgo(a.createdAt)}
                    </time>
                  </li>
                )
              })}
            </ul>
          )}
          {activity.length >= actLimit && (
            <div className="rail-foot">
              <button className="btn-sm" onClick={() => setActLimit((n) => n + 200)}>
                {t('Show older lines')}
              </button>
            </div>
          )}
        </>
      )}

      {tab === 'clients' && admin && (
        <>
          {!loaded && <Loading height={96} onRetry={() => void load()} />}
          {loaded && (
            <BuiltInClients items={queue} renderRows={rows} onChanged={load} usenetChildren={<UsenetClients />}>
              <DownloadClients />
            </BuiltInClients>
          )}
        </>
      )}
      {tab === 'bin' && admin && <RecycleBin />}
      {tab === 'requests' && <RequestsList onCount={setPendingRequests} />}
      {tab === 'reports' && <IssuesList onCount={setOpenReports} />}

      {tab === 'blocklist' && admin && (
        <>
          <p style={{ color: 'var(--text-dim)' }}>
            {t("Releases that failed because they were bad. Mediarium won't pick them again, but you can still choose one by hand.")}
            <HelpLink topic="activity#when-a-release-turns-out-to-be-bad" text={t('Remove lets searches pick that release again. Clear all does the same for every release in the list.')} />
          </p>
          {blocklistTotal === 0 ? (
            <div className="empty-state">
              <Icon name="ban" size={44} />
              <p>{t('Nothing is blocklisted.')}</p>
            </div>
          ) : (
            <>
              <div style={{ marginBottom: 12 }}>
                <button
                  className="btn-sm btn-danger"
                  disabled={tidying}
                  onClick={async () => {
                    if (!(await confirm({ title: t('Clear the whole blocklist?'), body: <p>{t('Searches will be able to pick every blocked release again.')}</p>, confirmLabel: t('Clear blocklist'), danger: true }))) return
                    setTidying(true)
                    try {
                      await api.clearBlocklist()
                      toast.success(t('Blocklist cleared.'))
                      void load()
                    } catch (e) {
                      toast.error(e instanceof Error ? e.message : String(e))
                    } finally {
                      setTidying(false)
                    }
                  }}
                >
                  {t('Clear all')}
                </button>
              </div>
              <table className="act-table">
                <thead>
                  <tr>
                    <th>{t('Release')}</th>
                    <th>{t('Why')}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {blocklist.map((b) => (
                    <tr key={b.id}>
                      <td className="mono-cell">{b.releaseTitle}</td>
                      <td style={{ color: 'var(--text-dim)', fontSize: '0.85rem' }}>{st(b.reason)}</td>
                      <td>
                        <button
                          className="btn-sm"
                          disabled={tidying}
                          onClick={async () => {
                            setTidying(true)
                            try {
                              await api.removeBlocklistEntry(b.id)
                              toast.success(t('Removed from the blocklist.'))
                              void load()
                            } catch (e) {
                              toast.error(e instanceof Error ? e.message : String(e))
                            } finally {
                              setTidying(false)
                            }
                          }}
                        >
                          {t('Remove')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {blocklist.length < blocklistTotal && (
                <div className="rail-foot">
                  <span style={{ color: 'var(--text-dim)', marginRight: 12 }}>{t('Showing {shown} of {total}', { shown: blocklist.length, total: blocklistTotal })}</span>
                  <button className="btn-sm" onClick={() => setBlLimit((n) => n + 100)}>
                    {t('Show more')}
                  </button>
                </div>
              )}
            </>
          )}
        </>
      )}
      {addingByHand && <AddDownloadDialog onClose={() => setAddingByHand(false)} onAdded={() => void load()} />}
    </div>
  )
}

