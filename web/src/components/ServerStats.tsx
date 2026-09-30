import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError, type SystemStats } from '../api'
import { formatBytes } from '../format'
import { useLive } from '../useLive'
import Icon from './Icon'

const pct = (used: number, total: number) => (total > 0 ? Math.round((used / total) * 100) : 0)

function uptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`
}

// The same disk often holds several folders; show each disk once, naming
// the folders on it.
export function uniqueDisks(stats: SystemStats) {
  const out: { labels: string[]; freeBytes: number; totalBytes: number; usedBytes: number }[] = []
  for (const d of stats.disks) {
    const same = out.find((o) => o.totalBytes === d.totalBytes && Math.abs(o.freeBytes - d.freeBytes) < 64 * 1024 * 1024)
    if (same) same.labels.push(d.label)
    else out.push({ labels: [d.label], freeBytes: d.freeBytes, totalBytes: d.totalBytes, usedBytes: d.usedBytes })
  }
  return out
}

export function useServerStats(ms = 10000) {
  const [stats, setStats] = useState<SystemStats | null>(null)
  const [denied, setDenied] = useState(false)
  const load = useCallback(() => {
    api
      .systemStats()
      .then((s) => {
        setStats(s)
        setDenied(false)
      })
      .catch((e) => {
        // Only an answer of "not for you" hides the card. A refresh that fails for another reason keeps what is showing.
        if (e instanceof ApiError && (e.status === 401 || e.status === 403 || e.status === 404)) setDenied(true)
      })
  }, [])
  useEffect(load, [load])
  useLive(load, ms)
  return { stats, denied }
}

export type Level = 'ok' | 'warn' | 'bad'
const levelOf = (p: number): Level => (p >= 90 ? 'bad' : p >= 75 ? 'warn' : 'ok')
const LEVEL_TEXT: Record<Level, string> = { ok: 'Everything looks fine', warn: 'Getting busy or full', bad: 'Running low on something' }

// Everything the two Server cards show, worked out once: how busy the
// processor and memory are, the storage in use, and one overall status
// (the worst of the three).
export function summarize(stats: SystemStats) {
  // Under 10% show one decimal, so a lightly used server reads 0.4% instead of a flat 0%.
  const roundPct = (n: number) => (n < 10 ? Math.round(n * 10) / 10 : Math.round(n))
  const cpu = stats.cpu ? roundPct(stats.cpu.percent) : undefined
  const appCpu = stats.app.cpuPercent !== undefined ? roundPct(stats.app.cpuPercent) : undefined
  const mem = stats.memory ? { ...stats.memory, pct: pct(stats.memory.usedBytes, stats.memory.totalBytes) } : undefined
  let storage = stats.storage
  if (!storage) {
    const disks = uniqueDisks(stats)
    if (disks.length > 0) {
      storage = {
        usedBytes: disks.reduce((n, d) => n + d.usedBytes, 0),
        freeBytes: disks.reduce((n, d) => n + d.freeBytes, 0),
        totalBytes: disks.reduce((n, d) => n + d.totalBytes, 0),
      }
    }
  }
  const store = storage && storage.totalBytes > 0 ? { ...storage, pct: pct(storage.usedBytes, storage.totalBytes) } : undefined
  const levels: Level[] = [cpu !== undefined ? levelOf(cpu) : 'ok', mem ? levelOf(mem.pct) : 'ok', store ? levelOf(store.pct) : 'ok']
  const level: Level = levels.includes('bad') ? 'bad' : levels.includes('warn') ? 'warn' : 'ok'
  return { cpu, appCpu, mem, store, level }
}

function StatusDot({ level }: { level: Level }) {
  return <span className={`srv-dot ${level}`} role="img" aria-label={LEVEL_TEXT[level]} title={LEVEL_TEXT[level]} />
}

function Bar({ used, total, label, detail, sub }: { used: number; total: number; label: string; detail: string; sub?: string }) {
  const p = pct(used, total)
  return (
    <div className="srv-bar">
      <div className="srv-bar-top">
        <span>{label}</span>
        <b>{p}%</b>
      </div>
      <div className={`srv-track${p >= 90 ? ' hot' : p >= 75 ? ' warm' : ''}`}>
        <div style={{ width: `${p}%` }} />
      </div>
      <small className="srv-bar-detail">{detail}</small>
      {sub && <small className="srv-bar-detail">{sub}</small>}
    </div>
  )
}

// A thin bar that grows to its width once it has appeared, and again whenever
// the figure changes. Green until 75%, amber until 90%, then red.
function MiniBar({ value }: { value: number }) {
  const [shown, setShown] = useState(0)
  useEffect(() => {
    const id = requestAnimationFrame(() => setShown(Math.max(0, Math.min(100, value))))
    return () => cancelAnimationFrame(id)
  }, [value])
  return (
    <span className={`info-bar ${levelOf(value)}`} aria-hidden="true">
      <span style={{ width: `${shown}%` }} />
    </span>
  )
}

const NL = String.fromCharCode(10)
const exact = (n: number) => `${n.toLocaleString()} bytes`

// The compact "Server" card on the dashboard: a heading, then CPU, memory and
// storage each with its figures and a small coloured bar, and a status dot.
// Pressing it opens the details in place (load, uptimes, what Mediarium itself
// uses, one bar per folder); pressing again closes them. Nothing is shown for
// accounts that may not see the server (members).
export function ServerInfoCard({ stats, denied, to }: { stats: SystemStats | null; denied: boolean; to?: string }) {
  const [open, setOpen] = useState(false)
  if (denied) return null
  if (!stats) return <div className="skeleton info-card" style={{ minHeight: 150 }} />
  const s = summarize(stats)
  const tip = [
    s.cpu !== undefined ? `CPU: ${stats.cpu?.percent.toFixed(1)}% of ${stats.cpu?.cores} ${stats.cpu?.cores === 1 ? 'core' : 'cores'}${stats.app.cpuPercent !== undefined ? `, Mediarium ${stats.app.cpuPercent.toFixed(1)}%` : ''}` : '',
    s.mem ? `Memory: ${exact(s.mem.usedBytes)} in use, ${exact(s.mem.totalBytes - s.mem.usedBytes)} available, ${exact(s.mem.totalBytes)} in all` : '',
    s.store ? `Storage: ${exact(s.store.usedBytes)} in use, ${exact(s.store.freeBytes)} free, ${exact(s.store.totalBytes)} in all` : '',
  ]
    .filter(Boolean)
    .join(NL)
  return (
    <div className={`info-card${open ? ' open' : ''}`}>
      <div className="info-summary" onClick={() => setOpen((v) => !v)} title={open ? undefined : tip}>
        <h3 className="info-head">
          <button type="button" className="info-toggle" aria-expanded={open} aria-controls="server-details" title={open ? 'Hide the details' : 'Show more details'}>
            <span className="info-ico">
              <Icon name="monitor" size={18} />
            </span>
            <span className="info-name">Server</span>
            <StatusDot level={s.level} />
            <Icon name={open ? 'chevron-up' : 'chevron-down'} size={16} />
          </button>
        </h3>
        <div className="info-rows">
          <div className="info-row">
            <span className="info-label">CPU</span>
            <span className="info-value">{s.cpu !== undefined ? `${s.cpu}%${s.appCpu !== undefined ? ` (Mediarium ${s.appCpu}%)` : ''}` : 'Not available'}</span>
            {s.cpu !== undefined && <MiniBar value={s.cpu} />}
          </div>
          <div className="info-row">
            <span className="info-label">Memory</span>
            <span className="info-value">{s.mem ? `${formatBytes(s.mem.usedBytes)} in use · ${formatBytes(Math.max(0, s.mem.totalBytes - s.mem.usedBytes))} available` : 'Not available'}</span>
            {s.mem && <MiniBar value={s.mem.pct} />}
          </div>
          <div className="info-row">
            <span className="info-label">Storage</span>
            <span className="info-value">{s.store ? `${formatBytes(s.store.usedBytes)} in use · ${formatBytes(s.store.freeBytes)} free` : 'Not available'}</span>
            {s.store && <MiniBar value={s.store.pct} />}
          </div>
        </div>
      </div>
      {open && (
        <div className="info-details" id="server-details">
          <div className="info-block">
            <h4>Running for</h4>
            {stats.load && (
              <p>
                Load <b>{stats.load.map((l) => l.toFixed(2)).join(' · ')}</b>
              </p>
            )}
            {stats.uptimeSeconds ? (
              <p>
                Server up <b>{uptime(stats.uptimeSeconds)}</b>
              </p>
            ) : null}
            <p>
              Mediarium up <b>{uptime(stats.app.uptimeSeconds)}</b>
            </p>
          </div>
          <div className="info-block">
            <h4>Mediarium itself</h4>
            <p>
              Memory <b>{formatBytes(stats.app.memoryBytes)}</b>
              {stats.app.limitBytes ? ` of a ${formatBytes(stats.app.limitBytes)} limit` : ''}
            </p>
            {stats.app.cpuPercent !== undefined && (
              <p>
                CPU <b>{stats.app.cpuPercent.toFixed(1)}%</b>
              </p>
            )}
            {stats.cpu && (
              <p>
                Processor <b>{stats.cpu.cores} {stats.cpu.cores === 1 ? 'core' : 'cores'}</b>
              </p>
            )}
          </div>
          <div className="info-block info-disks">
            <h4>Folders</h4>
            {stats.disks.length === 0 && <p>No folder sizes available.</p>}
            {stats.disks.map((d) => {
              const p = pct(d.usedBytes, d.totalBytes)
              return (
                <div key={d.label + d.path} className="info-disk" title={`${d.path}${NL}${exact(d.freeBytes)} free of ${exact(d.totalBytes)}`}>
                  <span>{d.label}</span>
                  <small>
                    {formatBytes(d.freeBytes)} free of {formatBytes(d.totalBytes)}
                  </small>
                  <MiniBar value={p} />
                </div>
              )
            })}
          </div>
          {to && (
            <Link to={to} className="info-more">
              More details <Icon name="open" size={13} />
            </Link>
          )}
        </div>
      )}
    </div>
  )
}

// Settings, System → Server: CPU, memory and storage side by side, with the
// smaller facts (load, uptimes, what Mediarium itself uses, each folder's
// disk) in a row underneath. Updated live.
export default function ServerStatsCard() {
  const { stats, denied } = useServerStats(5000)
  if (denied) return null
  const s = stats ? summarize(stats) : null
  return (
    <fieldset className="group usenet span-all">
      <legend>
        <Icon name="monitor" size={14} /> Server {s && <StatusDot level={s.level} />}
      </legend>
      {!stats || !s ? (
        <div className="skeleton" style={{ height: 120 }} />
      ) : (
        <>
          <div className="srv-bars">
            {stats.cpu ? (
              <Bar
                used={stats.cpu.percent}
                total={100}
                label="CPU"
                detail={`${s.cpu}% of ${stats.cpu.cores} ${stats.cpu.cores === 1 ? 'core' : 'cores'}`}
                sub={s.appCpu !== undefined ? `Mediarium uses ${s.appCpu}%` : undefined}
              />
            ) : (
              <small>CPU use isn't available on this system.</small>
            )}
            {s.mem ? <Bar used={s.mem.usedBytes} total={s.mem.totalBytes} label="Memory" detail={`${formatBytes(s.mem.usedBytes)} in use`} sub={`${formatBytes(Math.max(0, s.mem.totalBytes - s.mem.usedBytes))} available of ${formatBytes(s.mem.totalBytes)}`} /> : <small>Memory figures aren't available on this system.</small>}
            {s.store ? (
              <Bar used={s.store.usedBytes} total={s.store.totalBytes} label="Storage" detail={`${formatBytes(s.store.usedBytes)} in use`} sub={`${formatBytes(s.store.freeBytes)} free of ${formatBytes(s.store.totalBytes)}`} />
            ) : (
              <small>No folder sizes available.</small>
            )}
          </div>
          <div className="srv-facts">
            {stats.load && (
              <span>
                Load <b>{stats.load.map((l) => l.toFixed(2)).join(' · ')}</b>
              </span>
            )}
            {stats.uptimeSeconds ? (
              <span>
                Server up <b>{uptime(stats.uptimeSeconds)}</b>
              </span>
            ) : null}
            <span>
              Mediarium up <b>{uptime(stats.app.uptimeSeconds)}</b>
            </span>
            <span>
              Mediarium memory <b>{formatBytes(stats.app.memoryBytes)}</b>
              {stats.app.limitBytes ? ` of a ${formatBytes(stats.app.limitBytes)} limit` : ''}
            </span>
            {uniqueDisks(stats).map((d) => (
              <span key={d.labels.join()}>
                {d.labels.join(', ')} <b>{formatBytes(d.freeBytes)}</b> free of {formatBytes(d.totalBytes)}
              </span>
            ))}
          </div>
        </>
      )}
    </fieldset>
  )
}
