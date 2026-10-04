import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, isAdmin, type ActivityEntry, type BlocklistEntry, type QueueItem } from '../api'
import { useAuth } from '../AuthContext'
import Cover from '../components/Cover'
import Icon, { type IconName } from '../components/Icon'
import Loading from '../components/Loading'
import { PosterFallback } from '../components/PosterCard'
import { useToast } from '../components/Toast'
import { formatBytes, timeAgo } from '../format'
import { useConfirm } from '../components/ConfirmProvider'
import RecycleBin from '../components/RecycleBin'
import { failureHelp } from '../failureHelp'
import { useModules } from '../ModulesContext'
import { bulkState, groupQueue, RUNNING, stateNote, statusLabel, stillWaiting } from '../queueView'
import { useLive } from '../useLive'

type Tab = 'queue' | 'history' | 'blocklist' | 'bin'

const EVENT: Record<string, { icon: IconName; tone: string; group: string }> = {
  grabbed: { icon: 'download', tone: 'info', group: 'Downloads' },
  imported: { icon: 'check', tone: 'success', group: 'Downloads' },
  paused: { icon: 'pause', tone: 'info', group: 'Downloads' },
  stopped: { icon: 'stop', tone: 'muted', group: 'Downloads' },
  failed: { icon: 'warning', tone: 'danger', group: 'Problems' },
  blocklisted: { icon: 'ban', tone: 'danger', group: 'Problems' },
  conflict: { icon: 'warning', tone: 'warning', group: 'Problems' },
  subtitle: { icon: 'chat', tone: 'info', group: 'Subtitles' },
  added: { icon: 'plus', tone: 'success', group: 'Library' },
  removed: { icon: 'trash', tone: 'muted', group: 'Library' },
}

// A title (or album) still waiting for a release, with what the last search said.
interface Waiting {
  key: string
  movieId?: number
  albumId?: number
  to: string
  title: string
  subtitle?: string
  lastSearch?: string
  lastSearchAt?: string
}

const itemLink = (q: QueueItem) => (q.albumId ? `/music/album/${q.albumId}` : q.seriesId ? `/series/${q.seriesId}` : q.tmdbId ? `/title/${q.tmdbId}` : undefined)

// What a button press is doing right now, so only that button says so.
type Doing = 'pause' | 'resume' | 'stop' | 'other'

interface RowProps {
  q: QueueItem
  admin: boolean
  busy: Doing | null
  act: (item: QueueItem, fn: () => Promise<unknown>, done: string, doing?: Doing) => Promise<void>
  stop: (item: QueueItem) => Promise<void>
  remove: (item: QueueItem) => Promise<void>
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
      <summary>Why did this happen, and what can I do?</summary>
      <p>{help.why}</p>
      <ul>
        {help.tips.map((t) => (
          <li key={t}>{t}</li>
        ))}
      </ul>
      {link && (
        <p>
          <Link to={link}>Open the title</Link> to choose another release yourself or change its quality profile.
        </p>
      )}
    </details>
  )
}

function QueueRow({ q, admin, busy, act, stop, remove }: RowProps) {
  const link = itemLink(q)
  const running = RUNNING.has(q.status)
  const working = busy !== null || !!q.pending
  const animated = q.status === 'downloading' || q.status === 'importing'
  const showBar = (running && q.status !== 'queued') || q.status === 'conflict' || q.status === 'paused'
  const barTone = q.status === 'failed' ? 'bad' : q.status === 'conflict' ? 'warn' : q.status === 'paused' ? 'paused' : ''
  const note = stateNote(q)
  return (
    <div className={`qrow st-${q.status}${q.status === 'failed' ? ' failed' : ''} kind-${q.albumId ? 'music' : q.seriesId ? 'tv' : 'movie'}`}>
      <div className="qthumb">{q.albumId ? <Cover src={q.posterUrl} /> : q.posterUrl ? <img src={q.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</div>
      <div className="qmain">
        <div className="qtitle">
          {link ? <Link to={link}>{q.title}</Link> : <span>{q.title}</span>}
          {q.subtitle && <span className="badge">{q.subtitle}</span>}
          <span className={`badge ${q.status === 'completed' ? 'downloaded' : q.status}`}>{statusLabel(q)}</span>
          <span className="badge" title={q.protocol === 'torrent' ? 'Torrent' : 'Usenet'}>
            {q.protocol === 'torrent' ? '⇅ torrent' : '⇩ usenet'}
          </span>
        </div>
        <div className="qrelease">{q.releaseTitle}</div>
        {showBar && (
          <div className="qprogress">
            <div className={`bar ${animated && !q.pending ? 'active' : ''} ${barTone}`}>
              <span style={{ width: `${Math.max(2, q.progressPct)}%` }} />
            </div>
            <small>
              {q.progressPct.toFixed(0)}%{q.sizeBytes ? ` of ${formatBytes(q.sizeBytes)}` : ''}
            </small>
          </div>
        )}
        {note && <div className="qnote">{note}</div>}
        {q.error && <div className="error-text qerror">{q.error}</div>}
        {q.status === 'failed' && <FailureTips q={q} link={link} />}
        {q.status === 'conflict' && q.destPath && <div className="qrelease">A file already exists at {q.destPath}</div>}
        {(q.status === 'completed' || q.status === 'failed' || q.status === 'stopped') && (
          <small className="qwhen">
            {q.status === 'completed' ? 'Imported' : q.status === 'stopped' ? 'Stopped' : 'Failed'} {timeAgo(q.completedAt || q.addedAt)}
            {q.sizeBytes ? ` · ${formatBytes(q.sizeBytes)}` : ''}
          </small>
        )}
      </div>
      <div className="row-actions qactions">
        {q.status === 'conflict' && admin && (
          <>
            <button className="primary btn-sm" disabled={working} onClick={() => void act(q, () => api.resolveConflict(q.id, true), 'File replaced.')}>
              Overwrite
            </button>
            <button className="btn-sm" disabled={working} onClick={() => void act(q, () => api.resolveConflict(q.id, false), 'Kept the existing file.')}>
              Skip
            </button>
          </>
        )}
        {running && admin && (
          <>
            <button className="btn-sm btn-with-icon" disabled={working} title="Pause the transfer and keep what is saved" onClick={() => void act(q, () => api.pauseQueueItem(q.id), 'Paused.', 'pause')}>
              <Icon name="pause" size={15} /> {busy === 'pause' || q.pending === 'pausing' ? 'Pausing…' : 'Pause'}
            </button>
            <button className="btn-sm btn-with-icon" disabled={working} title="Cancel this download" onClick={() => void stop(q)}>
              <Icon name="stop" size={15} /> {busy === 'stop' || q.pending === 'stopping' ? 'Stopping…' : 'Stop'}
            </button>
          </>
        )}
        {q.status === 'paused' && admin && (
          <>
            <button className="primary btn-sm btn-with-icon" disabled={working} title="Carry on from what is saved" onClick={() => void act(q, () => api.resumeQueueItem(q.id), 'Resuming.', 'resume')}>
              <Icon name="play" size={15} /> {busy === 'resume' ? 'Resuming…' : 'Resume'}
            </button>
            <button className="btn-sm btn-with-icon" disabled={working} title="Cancel this download" onClick={() => void stop(q)}>
              <Icon name="stop" size={15} /> {busy === 'stop' || q.pending === 'stopping' ? 'Stopping…' : 'Stop'}
            </button>
          </>
        )}
        {(q.status === 'failed' || q.status === 'stopped') && (
          <button className="btn-sm btn-with-icon" disabled={working} onClick={() => void act(q, () => api.retryQueueItem(q.id), 'Trying that release again.')}>
            <Icon name="refresh" size={15} /> Retry
          </button>
        )}
        {q.status === 'failed' && admin && (
          <button className="btn-sm btn-with-icon" disabled={working} title="Never use this release again and look for another one" onClick={() => void act(q, () => api.blocklistQueueItem(q.id), 'Blocklisted. Looking for another release.')}>
            <Icon name="ban" size={15} /> Blocklist &amp; search again
          </button>
        )}
        {link && (
          <Link className="icon-btn" to={link} title="Open" aria-label="Open">
            <Icon name="open" size={17} />
          </Link>
        )}
        {(q.status === 'completed' || q.status === 'failed' || q.status === 'stopped' || q.status === 'paused') && admin && (
          <button className="icon-btn danger" title="Remove from this list" aria-label="Remove from this list" disabled={working} onClick={() => void remove(q)}>
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
  // Members can watch the queue and retry, but pausing, stopping, removing,
  // blocklisting and resolving conflicts are for administrators.
  const admin = isAdmin(useAuth().user)
  const [tab, setTab] = useState<Tab>('queue')
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [activity, setActivity] = useState<ActivityEntry[]>([])
  // History: how many lines to show, and the words to look for.
  const [actLimit, setActLimit] = useState(100)
  const [actQuery, setActQuery] = useState('')
  const [blocklist, setBlocklist] = useState<BlocklistEntry[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<{ id: number; doing: Doing } | null>(null)
  const [bulkBusy, setBulkBusy] = useState<'pause' | 'resume' | null>(null)
  const [group, setGroup] = useState('All')
  // Titles still waiting for a release, with why, so an empty queue explains itself.
  const musicOn = useModules().on('music')
  const [allWaiting, setWaiting] = useState<Waiting[]>([])
  const loadWaiting = useCallback(() => {
    Promise.all([
      api.getWanted('missing').catch(() => []),
      musicOn ? api.musicWanted('missing').catch(() => []) : Promise.resolve([]),
    ]).then(([wanted, albums]) =>
      setWaiting([
        ...wanted.map((w) => ({
          key: `${w.kind}-${w.id}`,
          movieId: w.movieId,
          to: w.kind === 'movie' && w.tmdbId ? `/title/${w.tmdbId}` : `/series/${w.seriesId}`,
          title: w.title,
          subtitle: w.subtitle,
          lastSearch: w.lastSearch,
          lastSearchAt: w.lastSearchAt,
        })),
        ...albums.map((a) => ({
          key: `album-${a.id}`,
          albumId: a.id,
          to: `/music/artist/${a.artistId}#album-${a.id}`,
          title: `${a.artistName} – ${a.title}`,
          lastSearch: a.lastSearch,
          lastSearchAt: a.lastSearchAt,
        })),
      ]),
    )
  }, [musicOn])
  useEffect(loadWaiting, [loadWaiting])
  useLive(loadWaiting, 30000)

  const load = useCallback(async () => {
    try {
      const [q, a, b] = await Promise.all([api.listQueue(), api.listActivity({ limit: actLimit, q: actQuery.trim() }), admin ? api.listBlocklist() : Promise.resolve([])])
      setQueue(q)
      setActivity(a)
      setBlocklist(b)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoaded(true)
    }
  }, [admin, actLimit, actQuery])

  useEffect(() => {
    void load()
  }, [load])
  useLive(load, 3000)

  const act = useCallback(
    async (item: QueueItem, fn: () => Promise<unknown>, done: string, doing: Doing = 'other') => {
      setBusy({ id: item.id, doing })
      try {
        await fn()
        toast.success(done)
        await load()
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      } finally {
        setBusy(null)
      }
    },
    [load, toast],
  )

  // Stop asks first, and offers to delete the partly downloaded files (on by
  // default). Only files in the downloads folder are ever deleted.
  const stop = useCallback(
    async (item: QueueItem) => {
      const hasFiles = item.keptFiles || item.progressPct > 0
      const r = await confirm({
        title: 'Stop this download?',
        body: (
          <p>
            <strong>{item.title}</strong> stops downloading and goes back to waiting for a release. You can try this release again from the list.
          </p>
        ),
        confirmLabel: 'Stop download',
        danger: true,
        option: hasFiles
          ? { label: 'Also delete the partly downloaded files', hint: 'Your library is not touched.', defaultChecked: true }
          : undefined,
      })
      if (!r) return
      const del = hasFiles && r.checked
      await act(item, () => api.stopQueueItem(item.id, del), del ? 'Stopped. The partial download was deleted.' : 'Stopped.', 'stop')
    },
    [act, confirm],
  )

  const remove = useCallback(
    async (item: QueueItem) => {
      let del = false
      if (item.status === 'paused' || item.keptFiles) {
        const r = await confirm({
          title: 'Remove this download?',
          body: <p>{item.status === 'paused' ? 'It will stop and leave the list.' : 'It will leave the list.'}</p>,
          confirmLabel: 'Remove',
          danger: true,
          option: item.keptFiles
            ? { label: 'Also delete the partly downloaded files', hint: 'Your library is not touched.', defaultChecked: true }
            : undefined,
        })
        if (!r) return
        del = item.keptFiles === true && r.checked
      }
      await act(item, () => api.deleteQueueItem(item.id, del), 'Removed from the list.')
    },
    [act, confirm],
  )

  async function clearFinished() {
    try {
      const r = await api.clearFinishedQueue()
      toast.success(`Cleared ${r.removed} finished item${r.removed === 1 ? '' : 's'}.`)
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function pauseAll() {
    setBulkBusy('pause')
    try {
      const r = await api.pauseAllQueue()
      toast.success(r.paused === 0 ? 'Nothing was running.' : `Paused ${r.paused} download${r.paused === 1 ? '' : 's'}.`)
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
      if (r.skipped > 0) toast.error(`${r.resumed} resumed, ${r.skipped} could not start. ${r.problem ?? ''}`.trim())
      else toast.success(r.resumed === 0 ? 'Nothing was paused.' : `Resumed ${r.resumed} download${r.resumed === 1 ? '' : 's'}.`)
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBulkBusy(null)
    }
  }

  const sections = useMemo(() => groupQueue(queue), [queue])
  const waiting = useMemo(() => stillWaiting(allWaiting, queue), [allWaiting, queue])
  const bulk = useMemo(() => bulkState(queue), [queue])
  const { inProgress, failed, decision, stopped, finished: completed } = sections
  const needsAttention = inProgress.length + decision.length + failed.length + stopped.length

  const groups = useMemo(() => ['All', ...Array.from(new Set(Object.values(EVENT).map((e) => e.group)))], [])
  const shownActivity = activity.filter((a) => group === 'All' || (EVENT[a.eventType]?.group ?? 'Other') === group)

  const rows = (list: QueueItem[]) =>
    list.map((q) => <QueueRow key={q.id} q={q} admin={admin} busy={busy?.id === q.id ? busy.doing : null} act={act} stop={stop} remove={remove} />)

  return (
    <div>
      <div className="page-header">
        <h1>Activity</h1>
        {admin && (
          <Link className="btn-link" to="/import/manual">
            <Icon name="folder" size={15} /> Import a file by hand
          </Link>
        )}
        <div className="seg">
          <button className={tab === 'queue' ? 'active' : ''} onClick={() => setTab('queue')}>
            Queue <small>({needsAttention})</small>
          </button>
          <button className={tab === 'history' ? 'active' : ''} onClick={() => setTab('history')}>
            History {completed.length > 0 && <small>({completed.length})</small>}
          </button>
          {admin && (
            <button className={tab === 'blocklist' ? 'active' : ''} onClick={() => setTab('blocklist')}>
              Blocklist <small>({blocklist.length})</small>
            </button>
          )}
          {admin && (
            <button className={tab === 'bin' ? 'active' : ''} onClick={() => setTab('bin')}>
              Recycle bin
            </button>
          )}
        </div>
      </div>
      {error && <p className="error-text">{error}</p>}

      {tab === 'queue' && (
        <>
          {!loaded && <Loading height={96} onRetry={() => void load()} />}
          {loaded && needsAttention === 0 && (
            <div className="empty-state">
              <Icon name="download" size={44} />
              <p>
                Nothing is downloading right now.{waiting.length > 0 ? ' The titles below are waiting for a release.' : ' Add something and it shows up here.'}
                {completed.length > 0 && (
                  <>
                    {' '}
                    <button className="link-btn" onClick={() => setTab('history')}>
                      {completed.length} finished {completed.length === 1 ? 'download is' : 'downloads are'} in History.
                    </button>
                  </>
                )}
              </p>
            </div>
          )}

          {inProgress.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>
                  In progress <small className="count">({inProgress.length})</small>
                </h2>
                {admin && (
                  <span className="rail-actions">
                    <button className="btn-sm btn-with-icon" disabled={!bulk.canPause || bulkBusy !== null} onClick={() => void pauseAll()}>
                      <Icon name="pause" size={14} /> {bulkBusy === 'pause' ? 'Pausing…' : 'Pause all'}
                    </button>
                    <button className="btn-sm btn-with-icon" disabled={!bulk.canResume || bulkBusy !== null} onClick={() => void resumeAll()}>
                      <Icon name="play" size={14} /> {bulkBusy === 'resume' ? 'Resuming…' : 'Resume all'}
                    </button>
                  </span>
                )}
              </div>
              {rows(inProgress)}
            </section>
          )}

          {failed.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>Failed</h2>
                <span>retry, pick another release, or remove</span>
              </div>
              {rows(failed)}
            </section>
          )}

          {decision.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <h2>{admin ? 'Needs your decision' : 'Waiting for an admin'}</h2>
              {rows(decision)}
            </section>
          )}

          {stopped.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <div className="rail-head">
                <h2>Stopped</h2>
                <span>retry, or remove</span>
              </div>
              {rows(stopped)}
            </section>
          )}

          {waiting.length > 0 && (
            <section className="waiting-list">
              <h2>
                Waiting for a release <small>({waiting.length})</small>
              </h2>
              <ul>
                {waiting.slice(0, 20).map((w) => (
                  <li key={w.key}>
                    <Link to={w.to}>
                      <strong>{w.title}</strong>
                      {w.subtitle && <span> · {w.subtitle}</span>}
                    </Link>
                    <small>{w.lastSearch ? `${w.lastSearch.replace(/^Searched: /, '')}${w.lastSearchAt ? ` (${timeAgo(w.lastSearchAt)})` : ''}` : 'Not searched yet.'}</small>
                  </li>
                ))}
              </ul>
              {waiting.length > 20 && (
                <Link to="/wanted" className="btn btn-sm">
                  See all {waiting.length} in Upcoming → Wanted
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
                <h2>Recently downloaded</h2>
                {admin && (
                  <button className="btn-sm btn-with-icon" onClick={() => void clearFinished()}>
                    <Icon name="trash" size={14} /> Clear finished
                  </button>
                )}
              </div>
              {rows(completed)}
            </section>
          )}
          <div className="chip-row" style={{ marginBottom: 16 }}>
            {groups.map((g) => (
              <button key={g} className={`chip${group === g ? ' active' : ''}`} onClick={() => setGroup(g)}>
                {g}
              </button>
            ))}
          </div>
          <div className="toolbar">
            <input type="search" className="wanted-search" placeholder="Find in the history" aria-label="Find in the history" value={actQuery} onChange={(e) => { setActQuery(e.target.value); setActLimit(100) }} />
          </div>
          {shownActivity.length === 0 ? (
            <div className="empty-state">
              <Icon name="activity" size={44} />
              <p>{actQuery.trim() ? 'Nothing in the history matches that.' : 'No activity yet.'}</p>
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
                    <span className="tl-msg">{a.link ? <Link to={a.link}>{a.message}</Link> : a.message}</span>
                    <time className="tl-time" title={new Date(a.createdAt).toLocaleString()}>
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
                Show older lines
              </button>
            </div>
          )}
        </>
      )}

      {tab === 'bin' && admin && <RecycleBin />}

      {tab === 'blocklist' && admin && (
        <>
          <p style={{ color: 'var(--text-dim)' }}>
            Releases that failed because they were bad. Mediarium won&apos;t pick them again, but you can still choose one by hand.
          </p>
          {blocklist.length === 0 ? (
            <div className="empty-state">
              <Icon name="ban" size={44} />
              <p>Nothing is blocklisted.</p>
            </div>
          ) : (
            <>
              <div style={{ marginBottom: 12 }}>
                <button
                  className="btn-sm btn-danger"
                  onClick={async () => {
                    if (!(await confirm({ title: 'Clear the whole blocklist?', body: <p>Searches will be able to pick every blocked release again.</p>, confirmLabel: 'Clear blocklist', danger: true }))) return
                    try {
                      await api.clearBlocklist()
                      toast.success('Blocklist cleared.')
                      void load()
                    } catch (e) {
                      toast.error(e instanceof Error ? e.message : String(e))
                    }
                  }}
                >
                  Clear all
                </button>
              </div>
              <table>
                <thead>
                  <tr>
                    <th>Release</th>
                    <th>Why</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {blocklist.map((b) => (
                    <tr key={b.id}>
                      <td style={{ wordBreak: 'break-all' }}>{b.releaseTitle}</td>
                      <td style={{ color: 'var(--text-dim)', fontSize: '0.85rem' }}>{b.reason}</td>
                      <td>
                        <button
                          className="btn-sm"
                          onClick={async () => {
                            try {
                              await api.removeBlocklistEntry(b.id)
                              toast.success('Removed from the blocklist.')
                              void load()
                            } catch (e) {
                              toast.error(e instanceof Error ? e.message : String(e))
                            }
                          }}
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </>
      )}
    </div>
  )
}
