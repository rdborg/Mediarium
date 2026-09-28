import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type ActivityEntry, type BlocklistEntry, type QueueItem } from '../api'
import Icon, { type IconName } from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { useToast } from '../components/Toast'
import { formatBytes, timeAgo } from '../format'

type Tab = 'queue' | 'history' | 'blocklist'

const ACTIVE = new Set(['queued', 'downloading', 'importing'])

const EVENT: Record<string, { icon: IconName; tone: string; group: string }> = {
  grabbed: { icon: 'download', tone: 'info', group: 'Downloads' },
  imported: { icon: 'check', tone: 'success', group: 'Downloads' },
  failed: { icon: 'warning', tone: 'danger', group: 'Problems' },
  blocklisted: { icon: 'ban', tone: 'danger', group: 'Problems' },
  conflict: { icon: 'warning', tone: 'warning', group: 'Problems' },
  subtitle: { icon: 'chat', tone: 'info', group: 'Subtitles' },
  added: { icon: 'plus', tone: 'success', group: 'Library' },
  removed: { icon: 'trash', tone: 'muted', group: 'Library' },
}

// Activity: what is downloading right now (with the actions you actually want
// on each entry), a readable history of what Mediarium has done, and the
// blocklist of releases it will not grab again.
export default function Queue() {
  const toast = useToast()
  const [tab, setTab] = useState<Tab>('queue')
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [activity, setActivity] = useState<ActivityEntry[]>([])
  const [blocklist, setBlocklist] = useState<BlocklistEntry[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<number | null>(null)
  const [group, setGroup] = useState('All')

  const load = useCallback(async () => {
    try {
      const [q, a, b] = await Promise.all([api.listQueue(), api.listActivity(), api.listBlocklist()])
      setQueue(q)
      setActivity(a)
      setBlocklist(b)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    void load()
    const id = setInterval(load, 3000)
    return () => clearInterval(id)
  }, [load])

  async function act(item: QueueItem, fn: () => Promise<unknown>, done: string) {
    setBusy(item.id)
    try {
      await fn()
      toast.success(done)
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  async function clearFinished() {
    try {
      const r = await api.clearFinishedQueue()
      toast.success(`Cleared ${r.removed} finished item${r.removed === 1 ? '' : 's'}.`)
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const active = queue.filter((q) => ACTIVE.has(q.status))
  const parked = queue.filter((q) => q.status === 'conflict')
  const finished = queue.filter((q) => q.status === 'completed' || q.status === 'failed')

  const groups = useMemo(() => ['All', ...Array.from(new Set(Object.values(EVENT).map((e) => e.group)))], [])
  const shownActivity = activity.filter((a) => group === 'All' || (EVENT[a.eventType]?.group ?? 'Other') === group)

  const itemLink = (q: QueueItem) => (q.seriesId ? `/series/${q.seriesId}` : q.tmdbId ? `/title/${q.tmdbId}` : undefined)

  function Row({ q }: { q: QueueItem }) {
    const link = itemLink(q)
    const isActive = ACTIVE.has(q.status)
    const barTone = q.status === 'failed' ? 'bad' : q.status === 'conflict' ? 'warn' : ''
    return (
      <div className={`qrow${q.status === 'failed' ? ' failed' : ''} kind-${q.seriesId ? 'tv' : 'movie'}`}>
        <div className="qthumb">{q.posterUrl ? <img src={q.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</div>
        <div className="qmain">
          <div className="qtitle">
            {link ? <Link to={link}>{q.title}</Link> : <span>{q.title}</span>}
            {q.subtitle && <span className="badge">{q.subtitle}</span>}
            <span className={`badge ${q.status === 'completed' ? 'downloaded' : q.status}`}>{q.status}</span>
            <span className="badge" title={q.protocol === 'torrent' ? 'Torrent' : 'Usenet'}>
              {q.protocol === 'torrent' ? '⇅ torrent' : '⇩ usenet'}
            </span>
          </div>
          <div className="qrelease">{q.releaseTitle}</div>
          {(isActive || q.status === 'conflict') && (
            <div className="qprogress">
              <div className={`bar ${isActive ? 'active' : ''} ${barTone}`}>
                <span style={{ width: `${Math.max(2, q.progressPct)}%` }} />
              </div>
              <small>
                {q.progressPct.toFixed(0)}%{q.sizeBytes ? ` of ${formatBytes(q.sizeBytes)}` : ''}
              </small>
            </div>
          )}
          {q.error && <div className="error-text qerror">{q.error}</div>}
          {q.status === 'conflict' && q.destPath && <div className="qrelease">A file already exists at {q.destPath}</div>}
          {!isActive && q.status !== 'conflict' && (
            <small className="qwhen">
              {q.status === 'completed' ? 'Imported' : 'Failed'} {timeAgo(q.completedAt || q.addedAt)}
              {q.sizeBytes ? ` · ${formatBytes(q.sizeBytes)}` : ''}
            </small>
          )}
        </div>
        <div className="row-actions qactions">
          {q.status === 'conflict' && (
            <>
              <button className="primary btn-sm" disabled={busy === q.id} onClick={() => void act(q, () => api.resolveConflict(q.id, true), 'File replaced.')}>
                Overwrite
              </button>
              <button className="btn-sm" disabled={busy === q.id} onClick={() => void act(q, () => api.resolveConflict(q.id, false), 'Kept the existing file.')}>
                Skip
              </button>
            </>
          )}
          {q.status === 'failed' && (
            <>
              <button className="btn-sm btn-with-icon" disabled={busy === q.id} onClick={() => void act(q, () => api.retryQueueItem(q.id), 'Trying that release again.')}>
                <Icon name="refresh" size={15} /> Retry
              </button>
              <button className="btn-sm btn-with-icon" disabled={busy === q.id} title="Never grab this release again and look for another" onClick={() => void act(q, () => api.blocklistQueueItem(q.id), 'Blocklisted. Looking for another release.')}>
                <Icon name="ban" size={15} /> Blocklist &amp; search again
              </button>
            </>
          )}
          {link && (
            <Link className="icon-btn" to={link} title="Open" aria-label="Open">
              <Icon name="open" size={17} />
            </Link>
          )}
          {!isActive && q.status !== 'conflict' && (
            <button className="icon-btn danger" title="Remove from this list" aria-label="Remove from this list" disabled={busy === q.id} onClick={() => void act(q, () => api.deleteQueueItem(q.id), 'Removed from the list.')}>
              <Icon name="trash" size={17} />
            </button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div>
      <div className="page-header">
        <h1>Activity</h1>
        <div className="seg">
          <button className={tab === 'queue' ? 'active' : ''} onClick={() => setTab('queue')}>
            Queue <small>({active.length + parked.length + finished.length})</small>
          </button>
          <button className={tab === 'history' ? 'active' : ''} onClick={() => setTab('history')}>
            History
          </button>
          <button className={tab === 'blocklist' ? 'active' : ''} onClick={() => setTab('blocklist')}>
            Blocklist <small>({blocklist.length})</small>
          </button>
        </div>
      </div>
      {error && <p className="error-text">{error}</p>}

      {tab === 'queue' && (
        <>
          {!loaded && <div className="skeleton" style={{ height: 96 }} />}
          {loaded && active.length + parked.length + finished.length === 0 && (
            <div className="empty-state">
              <Icon name="download" size={44} />
              <p>Nothing downloading right now. Add something and it will show up here.</p>
            </div>
          )}
          {parked.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <h2>Needs your decision</h2>
              {parked.map((q) => (
                <Row key={q.id} q={q} />
              ))}
            </section>
          )}
          {active.length > 0 && (
            <section style={{ marginBottom: 24 }}>
              <h2>In progress</h2>
              {active.map((q) => (
                <Row key={q.id} q={q} />
              ))}
            </section>
          )}
          {finished.length > 0 && (
            <section>
              <div className="rail-head">
                <h2>Finished</h2>
                <button className="btn-sm btn-with-icon" onClick={() => void clearFinished()}>
                  <Icon name="trash" size={14} /> Clear finished
                </button>
              </div>
              {finished.map((q) => (
                <Row key={q.id} q={q} />
              ))}
            </section>
          )}
        </>
      )}

      {tab === 'history' && (
        <>
          <div className="chip-row" style={{ marginBottom: 16 }}>
            {groups.map((g) => (
              <button key={g} className={`chip${group === g ? ' active' : ''}`} onClick={() => setGroup(g)}>
                {g}
              </button>
            ))}
          </div>
          {shownActivity.length === 0 ? (
            <div className="empty-state">
              <Icon name="activity" size={44} />
              <p>No activity to show yet.</p>
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
                    <span className="tl-msg">{a.message}</span>
                    <time className="tl-time" title={new Date(a.createdAt).toLocaleString()}>
                      {timeAgo(a.createdAt)}
                    </time>
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}

      {tab === 'blocklist' && (
        <>
          <p style={{ color: 'var(--text-dim)' }}>
            Releases that failed because the release itself was bad. Automation won't grab these again; you can still pick one by hand.
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
                    if (!window.confirm('Remove every release from the blocklist?')) return
                    await api.clearBlocklist()
                    toast.success('Blocklist cleared.')
                    void load()
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
                            await api.removeBlocklistEntry(b.id)
                            toast.success('Removed from the blocklist.')
                            void load()
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
