import { useEffect, useMemo, useState } from 'react'
import { api, ApiError, type IndexerConfig, type IndexerDefinition } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'

type Filter = 'all' | 'public' | 'semi-private' | 'private'

// Pick a site from the community list of search definitions (the same ones
// Prowlarr and Jackett use), fill in what that site asks for, and add it.
// The list is downloaded only when this opens; nothing is pre-selected.
export default function SitePicker({ protocol, onAdded }: { protocol: 'torrent' | 'usenet'; onAdded: (i: IndexerConfig) => void }) {
  const toast = useToast()
  const [defs, setDefs] = useState<IndexerDefinition[] | null>(null)
  const [meta, setMeta] = useState<{ updatedAt?: string; source?: string; licence?: string }>({})
  const [error, setError] = useState('')
  const [missing, setMissing] = useState(false)
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [picked, setPicked] = useState<IndexerDefinition | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [baseUrl, setBaseUrl] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [refreshing, setRefreshing] = useState(false)

  const load = (refresh = false) => {
    setError('')
    if (refresh) setRefreshing(true)
    api
      .indexerDefinitions(refresh)
      .then((r) => {
        setDefs(r.definitions)
        setMeta({ updatedAt: r.updatedAt, source: r.source, licence: r.licence })
      })
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) setMissing(true)
        else setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => setRefreshing(false))
  }
  useEffect(() => load(false), [])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (defs ?? [])
      .filter((d) => d.protocol === protocol)
      .filter((d) => filter === 'all' || d.type === filter)
      .filter((d) => !needle || d.name.toLowerCase().includes(needle) || (d.description ?? '').toLowerCase().includes(needle))
      .sort((a, b) => a.name.localeCompare(b.name))
  }, [defs, q, filter, protocol])

  function pick(d: IndexerDefinition) {
    setPicked(d)
    setName(d.name)
    setBaseUrl(d.links?.[0] ?? '')
    const init: Record<string, string> = {}
    for (const s of d.settings ?? []) {
      if (s.default !== undefined && s.default !== null) init[s.name] = String(s.default)
    }
    setValues(init)
  }

  async function add() {
    if (!picked) return
    setBusy(true)
    try {
      const created = await api.createIndexer({ name: name.trim() || picked.name, definitionId: picked.id, baseUrl, apiKey: '', protocol, settings: values })
      toast.success(`${created.name} added. Press Test to check it works.`)
      setPicked(null)
      onAdded(created)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (missing) {
    return (
      <div className="notice notice-info">
        Picking sites from a list is coming in the next update. For now you can use <strong>Paste a Torznab link</strong>, or skip torrents and add them later in Settings, Indexers.
      </div>
    )
  }
  if (error) {
    return (
      <div className="notice notice-warn">
        The site list could not be loaded: {error}.{' '}
        <button className="btn-sm" onClick={() => load(true)}>
          Try again
        </button>
      </div>
    )
  }
  if (!defs) return <div className="skeleton" style={{ height: 180 }} />

  if (picked) {
    const fields = (picked.settings ?? []).filter((s) => !s.type.startsWith('info'))
    const infos = (picked.settings ?? []).filter((s) => s.type.startsWith('info') && s.label)
    return (
      <div className="site-detail grid-form">
        <button className="btn-back btn-sm" onClick={() => setPicked(null)} style={{ justifySelf: 'start' }}>
          ← Back to the list
        </button>
        <div>
          <strong style={{ fontSize: '1.05rem' }}>{picked.name}</strong> <span className={`badge site-${picked.type}`}>{typeLabel(picked.type)}</span>
          {picked.language && <span className="badge">{picked.language}</span>}
          {picked.description && <p style={{ margin: '6px 0 0', color: 'var(--text-dim)' }}>{picked.description}</p>}
        </div>
        {infos.map((s) => (
          <div key={s.name} className="notice notice-info">
            {s.label}
          </div>
        ))}
        <div className="form-cols">
          <label>
            Name in Mediarium
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {picked.links && picked.links.length > 1 && (
            <label>
              Site address
              <select value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)}>
                {picked.links.map((l) => (
                  <option key={l} value={l}>
                    {l}
                  </option>
                ))}
              </select>
            </label>
          )}
          {fields.map((s) =>
            s.type === 'checkbox' ? (
              <label key={s.name} className="check-row">
                <input type="checkbox" checked={values[s.name] === 'true'} onChange={(e) => setValues({ ...values, [s.name]: e.target.checked ? 'true' : 'false' })} />
                {s.label || s.name}
              </label>
            ) : s.type === 'select' ? (
              <label key={s.name}>
                {s.label || s.name}
                <select value={values[s.name] ?? ''} onChange={(e) => setValues({ ...values, [s.name]: e.target.value })}>
                  {(s.options ?? []).map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <label key={s.name}>
                {s.label || s.name}
                <input
                  type={s.type === 'password' ? 'password' : 'text'}
                  value={values[s.name] ?? ''}
                  onChange={(e) => setValues({ ...values, [s.name]: e.target.value })}
                  autoComplete="off"
                  spellCheck={false}
                />
              </label>
            ),
          )}
        </div>
        {fields.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>This site needs nothing else. Press Add.</p>}
        <div>
          <button className="primary btn-with-icon" onClick={() => void add()} disabled={busy}>
            <Icon name="plus" size={15} /> Add {picked.name}
          </button>
        </div>
      </div>
    )
  }

  const counts = (t: Filter) => (defs ?? []).filter((d) => d.protocol === protocol && (t === 'all' || d.type === t)).length
  return (
    <div className="site-picker grid-form">
      <div className="toolbar" style={{ margin: 0 }}>
        <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search sites" aria-label="Search sites" style={{ flex: '1 1 220px', maxWidth: 'none' }} />
        <div className="chip-row">
          {(['all', 'public', 'semi-private', 'private'] as Filter[]).map((f) => (
            <button key={f} className={`chip${filter === f ? ' active' : ''}`} onClick={() => setFilter(f)}>
              {f === 'all' ? 'All' : typeLabel(f)} <small>{counts(f)}</small>
            </button>
          ))}
        </div>
      </div>
      <div className="site-list" role="list">
        {shown.length === 0 && <p style={{ color: 'var(--text-dim)', margin: 8 }}>No site matches.</p>}
        {shown.map((d) => (
          <button
            key={d.id}
            role="listitem"
            className={`site-row${d.supported === false ? ' unsupported' : ''}`}
            onClick={() => d.supported !== false && pick(d)}
            disabled={d.supported === false}
            title={d.supported === false ? d.problem || 'This site cannot be used yet.' : undefined}
          >
            <span className="site-name">{d.name}</span>
            <span className={`badge site-${d.type}`}>{typeLabel(d.type)}</span>
            {d.language && <span className="site-lang">{d.language}</span>}
            <span className="site-desc">{d.supported === false ? `Not supported yet: ${d.problem || 'unknown reason'}` : d.description}</span>
          </button>
        ))}
      </div>
      <p className="site-source">
        {shown.length} sites. List from the community {meta.source ? <a href={meta.source} target="_blank" rel="noreferrer">site definitions</a> : 'site definitions'}
        {meta.licence ? ` (${meta.licence})` : ''}
        {meta.updatedAt ? `, updated ${new Date(meta.updatedAt).toLocaleDateString()}` : ''}.{' '}
        <button className="btn-sm" onClick={() => load(true)} disabled={refreshing}>
          {refreshing ? 'Updating…' : 'Update list'}
        </button>{' '}
        You choose which sites to use and are responsible for following the law where you live.
      </p>
    </div>
  )
}

function typeLabel(t: string): string {
  return t === 'public' ? 'Public' : t === 'private' ? 'Private' : t === 'semi-private' ? 'Semi-private' : t
}
