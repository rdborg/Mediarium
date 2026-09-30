import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../api'
import { copyText } from '../../components/CopyBox'
import Icon from '../../components/Icon'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import {
  agoText,
  countText,
  emptyText,
  exactTime,
  filtersActive,
  happenedText,
  NO_FILTERS,
  problemQuery,
  problemText,
  RANGE_LABELS,
  showingText,
  type ProblemFilters,
  type ProblemList,
  type ProblemRow,
  type RangePreset,
} from '../../problemsView'
import { formatSupportReport } from '../../supportReport'
import { problemsChanged } from '../../useProblems'
import { useLive } from '../../useLive'

const PAGE = 50

// Settings > System > Logs and errors: the things that went wrong (a download
// that failed, a provider that refused a login, a full disk), each with what it
// means and what to try. The list keeps itself up to date.
export default function LogsSettings() {
  const toast = useToast()
  const [filters, setFilters] = useState<ProblemFilters>(NO_FILTERS)
  const [typed, setTyped] = useState('')
  const [limit, setLimit] = useState(PAGE)
  const [data, setData] = useState<ProblemList | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState<Set<number>>(new Set())
  const [copying, setCopying] = useState(false)
  const asked = useRef(0)

  // Wait for a pause in typing before asking again.
  useEffect(() => {
    const t = setTimeout(() => setFilters((f) => (f.text === typed ? f : { ...f, text: typed })), 300)
    return () => clearTimeout(t)
  }, [typed])

  const query = useMemo(() => problemQuery(filters, { limit }), [filters, limit])

  const load = useCallback(() => {
    const n = ++asked.current
    return api
      .problems(query)
      .then((d) => {
        if (n !== asked.current) return // a newer question was asked meanwhile
        setData(d)
        setError('')
      })
      .catch((e) => {
        if (n === asked.current) setError(e instanceof Error ? e.message : String(e))
      })
  }, [query])
  useEffect(() => {
    void load()
  }, [load])
  useLive(load, 5000)

  const change = (patch: Partial<ProblemFilters>) => {
    setFilters((f) => ({ ...f, ...patch }))
    setLimit(PAGE)
  }
  const clearFilters = () => {
    setFilters(NO_FILTERS)
    setTyped('')
    setLimit(PAGE)
  }

  const toggle = (id: number) =>
    setOpen((s) => {
      const next = new Set(s)
      if (!next.delete(id)) next.add(id)
      return next
    })

  async function markRead(req: { ids?: number[]; all?: boolean }) {
    try {
      const res = await api.markProblemsRead(req)
      setData((d) =>
        d
          ? {
              ...d,
              counts: res.counts,
              items: d.items
                .map((p) => (req.all || req.ids?.includes(p.id) ? { ...p, read: true } : p))
                .filter((p) => !(filters.unreadOnly && p.read)),
            }
          : d,
      )
      problemsChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function copyForSupport() {
    setCopying(true)
    try {
      const ok = await copyText(formatSupportReport(await api.diagnostics()))
      if (ok) toast.success('Copied. Paste it into your support request.')
      else toast.error('Your browser blocked copying. Use Download log file instead.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setCopying(false)
    }
  }

  async function setNotify(on: boolean) {
    try {
      await api.setNotifyOnProblems(on)
      setData((d) => (d ? { ...d, notifyOnProblems: on } : d))
      toast.success(on ? 'Error messages are on.' : 'Error messages are off.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const filtered = filtersActive(filters)
  const counts = data?.counts

  return (
    <div className="pl-page">
      <p className="pl-intro">
        Things that went wrong, with what to try. Repeats within a few minutes count on one line, and problems are kept for {data?.keepDays ?? 30} days.
      </p>

      <div className="pl-cards">
        <StatCard tone="bad" icon="warning" label="Errors today" value={counts?.errorsToday} word="error" />
        <StatCard tone="warn" icon="info" label="Warnings today" value={counts?.warningsToday} word="warning" />
        <StatCard tone="bad" icon="calendar" label="Errors this week" value={counts?.errorsWeek} word="error" />
        <StatCard tone="warn" icon="calendar" label="Warnings this week" value={counts?.warningsWeek} word="warning" />
      </div>

      <div className="pl-toolbar">
        <div className="pl-filters">
          <div className="seg" role="group" aria-label="Show">
            {(
              [
                ['all', 'All'],
                ['error', 'Errors'],
                ['warning', 'Warnings'],
              ] as const
            ).map(([value, label]) => (
              <button key={value} type="button" className={filters.level === value ? 'active' : ''} aria-pressed={filters.level === value} onClick={() => change({ level: value })}>
                {label}
              </button>
            ))}
          </div>
          <select aria-label="Area" value={filters.area} onChange={(e) => change({ area: e.target.value })}>
            <option value="">All areas</option>
            {(data?.areas ?? []).map((a) => (
              <option key={a.id} value={a.id}>
                {a.label}
              </option>
            ))}
          </select>
          <select aria-label="When" value={filters.range} onChange={(e) => change({ range: e.target.value as RangePreset })}>
            {(Object.keys(RANGE_LABELS) as RangePreset[]).map((r) => (
              <option key={r} value={r}>
                {RANGE_LABELS[r]}
              </option>
            ))}
          </select>
          {filters.range === 'custom' && (
            <>
              <label className="pl-date">
                <span>From</span>
                <input type="date" value={filters.from} max={filters.to || undefined} onChange={(e) => change({ from: e.target.value })} />
              </label>
              <label className="pl-date">
                <span>To</span>
                <input type="date" value={filters.to} min={filters.from || undefined} onChange={(e) => change({ to: e.target.value })} />
              </label>
            </>
          )}
          <input className="pl-search" type="search" placeholder="Search the log" aria-label="Search the log" value={typed} onChange={(e) => setTyped(e.target.value)} />
          <label className="pl-unread">
            <input type="checkbox" checked={filters.unreadOnly} onChange={(e) => change({ unreadOnly: e.target.checked })} /> Only unread
          </label>
          {filtered && (
            <button type="button" className="pl-clear" onClick={clearFilters}>
              <Icon name="x" size={13} /> Clear filters
            </button>
          )}
        </div>
      </div>

      <div className="pl-bar">
        <span className="pl-bar-text">
          {data && <span>{showingText(data.items.length, data.total)}</span>}
          {counts && counts.unread > 0 && <span className="pl-unread-count">{counts.unread} unread</span>}
        </span>
        <div className="pl-actions">
          <button type="button" className="btn-with-icon" onClick={() => void copyForSupport()} disabled={copying}>
            <Icon name="chat" size={15} /> {copying ? 'Copying…' : 'Copy for support'}
          </button>
          <a className="btn-link" href={`/api/system/problems/export${problemQuery(filters)}`} download>
            <Icon name="download" size={15} /> Download log file
          </a>
          <button type="button" className="btn-with-icon" onClick={() => void markRead({ all: true })} disabled={!counts || counts.unread === 0}>
            <Icon name="check" size={15} /> Mark all as read
          </button>
        </div>
      </div>

      {error && <p className="error-text">{error}</p>}

      {!data && !error && (
        <div className="pl-list" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <div key={i} className="skeleton" style={{ height: 64, borderRadius: 14 }} />
          ))}
        </div>
      )}

      {data && data.items.length === 0 && (
        <div className="pl-empty">
          <Icon name={filtered ? 'search' : 'check'} size={34} />
          <strong>{emptyText(filtered, data.keepDays)}</strong>
          <span>
            {filtered
              ? 'Try clearing a filter.'
              : 'When something goes wrong, like a failed download or a refused login, it shows up here with what to try.'}
          </span>
        </div>
      )}

      {data && data.items.length > 0 && (
        <>
          <ul className="pl-list">
            {data.items.map((p) => (
              <ProblemItem key={p.id} p={p} open={open.has(p.id)} onToggle={() => toggle(p.id)} onRead={() => void markRead({ ids: [p.id] })} />
            ))}
          </ul>
          {data.items.length < data.total && (
            <div className="pl-more">
              <button type="button" onClick={() => setLimit((n) => n + PAGE)}>
                Show more
              </button>
            </div>
          )}
        </>
      )}

      <fieldset className="group alerts pl-notify">
        <legend>
          <Icon name="mail" size={14} /> Messages about errors
        </legend>
        <Switch
          checked={data?.notifyOnProblems ?? false}
          disabled={!data}
          onChange={(on) => void setNotify(on)}
          label="Tell me when an error happens"
          showState
          description={
            <>
              Goes to the places under <Link to="/settings/notifications">Notifications</Link> that have &quot;Something is wrong&quot; ticked. Errors only, and a repeat is sent once.
            </>
          }
        />
      </fieldset>
    </div>
  )
}

function StatCard({ tone, icon, label, value, word }: { tone: 'bad' | 'warn'; icon: 'warning' | 'info' | 'calendar'; label: string; value?: number; word: 'error' | 'warning' }) {
  return (
    <div className={`pl-card ${tone}${value ? ' has' : ''}`}>
      <span className="pl-card-icon">
        <Icon name={icon} size={18} />
      </span>
      <span className="pl-card-label">{label}</span>
      <span className="pl-card-value">{value ?? '…'}</span>
      <span className="pl-card-sub">{value === undefined ? '' : countText(value, word)}</span>
    </div>
  )
}

function ProblemItem({ p, open, onToggle, onRead }: { p: ProblemRow; open: boolean; onToggle: () => void; onRead: () => void }) {
  const toast = useToast()
  const bodyId = `pl-body-${p.id}`
  const other = p.message && p.message !== p.title

  async function copy(text: string) {
    if (await copyText(text)) toast.success('Copied.')
    else toast.error('Your browser blocked copying. Select the text and copy it yourself.')
  }

  return (
    <li className={`pl-row ${p.level}${p.read ? ' read' : ' unread'}${open ? ' open' : ''}`}>
      <button type="button" className="pl-head" aria-expanded={open} aria-controls={bodyId} onClick={onToggle}>
        <span className={`pl-level ${p.level}`}>
          <Icon name={p.level === 'error' ? 'warning' : 'info'} size={13} /> {p.level === 'error' ? 'Error' : 'Warning'}
        </span>
        <span className="pl-title">{p.title}</span>
        {!p.read && <span className="pl-new">New</span>}
        <span className="pl-meta">
          <span className="pl-area">{p.areaLabel}</span>
          {p.count > 1 && <span className="pl-times">{p.count} times</span>}
          <span className="pl-when" title={exactTime(p.lastAt)}>
            {agoText(p.lastAt)}
          </span>
          <Icon name="chevron-down" size={16} className="pl-chev" />
        </span>
      </button>

      <div className="pl-sub">
        <span className="pl-sub-text">
          {other && <span>{p.message}</span>}
          {p.forTitle &&
            (p.forLink ? (
              <span>
                For <Link to={p.forLink}>{p.forTitle}</Link>
              </span>
            ) : (
              <span>For {p.forTitle}</span>
            ))}
          {p.downloadId ? (
            <span>
              <Link to="/queue">Download {p.downloadId}</Link>
            </span>
          ) : null}
        </span>
        {!p.read && (
          <button type="button" className="pl-read" onClick={onRead}>
            <Icon name="check" size={13} /> Mark as read
          </button>
        )}
      </div>

      {open && (
        <div className="pl-body" id={bodyId}>
          <section>
            <h4>What happened</h4>
            <p>{p.explain || p.message}</p>
            <p className="pl-dim">{happenedText(p)}</p>
            <p className="pl-dim">
              First seen {exactTime(p.firstAt)}. <code>{p.code}</code>
            </p>
          </section>
          <section>
            <h4>What to try</h4>
            <p>{p.try || 'Nothing specific to try. If it keeps happening, send the technical detail to support.'}</p>
            {p.linkPath && (
              <Link className="btn-link primary-look" to={p.linkPath}>
                {p.linkLabel || 'Open settings'} <Icon name="open" size={14} />
              </Link>
            )}
          </section>
          <section>
            <div className="pl-detail-head">
              <h4>Technical detail</h4>
              <button type="button" className="pl-copy" onClick={() => void copy(problemText(p))}>
                <Icon name="list" size={13} /> Copy
              </button>
            </div>
            {p.detail ? <pre className="pl-detail">{p.detail}</pre> : <p className="pl-dim">No more detail for this one.</p>}
          </section>
        </div>
      )}
    </li>
  )
}
