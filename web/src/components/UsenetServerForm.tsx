import { useState, type ReactNode } from 'react'
import { api } from '../api'
import { USENET_PROVIDERS } from '../usenetProviders'
import TestButton from './TestButton'

export interface UsenetServerDraft {
  id?: number
  name: string
  host: string
  port: number
  useSsl: boolean
  username: string
  password: string
  connections: number
  priority: number
  enabled: boolean
  hasPassword?: boolean
}

export const blankServer = (priority = 0): UsenetServerDraft => ({
  name: '',
  host: '',
  port: 563,
  useSsl: true,
  username: '',
  password: '',
  connections: 20,
  priority,
  enabled: true,
})

// Add/edit form for a Usenet server: the news-server account your Usenet
// provider gave you. Mediarium's built-in downloader connects to it directly,
// so this is the only thing needed to download from Usenet.
export default function UsenetServerForm({
  initial,
  submitLabel,
  onSubmit,
  onCancel,
  showPriority = true,
  extra,
}: {
  initial: UsenetServerDraft
  submitLabel: string
  onSubmit: (draft: UsenetServerDraft) => void | Promise<void>
  onCancel?: () => void
  showPriority?: boolean
  extra?: ReactNode
}) {
  const [d, setD] = useState<UsenetServerDraft>(initial)
  const [busy, setBusy] = useState(false)
  const editing = d.id !== undefined

  function applyPreset(name: string) {
    const p = USENET_PROVIDERS.find((x) => x.name === name)
    if (!p) return
    setD((cur) => ({ ...cur, name: p.name, host: p.host, port: p.port, useSsl: p.useSsl, connections: p.connections }))
  }

  async function submit() {
    setBusy(true)
    try {
      await onSubmit(d)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid-form">
      {!editing && (
        <label>
          Provider (optional shortcut)
          <select value="" onChange={(e) => applyPreset(e.target.value)}>
            <option value="">— pick your provider to fill in the host, or type it below —</option>
            {USENET_PROVIDERS.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <label>
        Name
        <input value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} placeholder="e.g. My provider" />
      </label>
      <label>
        Server host
        <input value={d.host} onChange={(e) => setD({ ...d, host: e.target.value })} placeholder="news.example.com" />
      </label>
      <div style={{ display: 'flex', gap: 12, alignItems: 'end', flexWrap: 'wrap' }}>
        <label style={{ width: 110 }}>
          Port
          <input type="number" value={d.port} onChange={(e) => setD({ ...d, port: Number(e.target.value) })} />
        </label>
        <label style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <input type="checkbox" checked={d.useSsl} onChange={(e) => setD({ ...d, useSsl: e.target.checked, port: e.target.checked && d.port === 119 ? 563 : !e.target.checked && d.port === 563 ? 119 : d.port })} />
          Use SSL (recommended)
        </label>
      </div>
      <label>
        Username
        <input value={d.username} onChange={(e) => setD({ ...d, username: e.target.value })} autoComplete="off" />
      </label>
      <label>
        Password
        <input
          type="password"
          value={d.password}
          onChange={(e) => setD({ ...d, password: e.target.value })}
          placeholder={editing && d.hasPassword ? 'leave blank to keep the saved password' : ''}
          autoComplete="new-password"
        />
      </label>
      <label>
        Connections
        <input type="number" min={1} max={100} value={d.connections} onChange={(e) => setD({ ...d, connections: Number(e.target.value) })} />
        <span style={{ color: 'var(--text-dim)', fontSize: '0.8rem' }}>
          Simultaneous connections. Use no more than your plan allows.
        </span>
      </label>
      {showPriority && (
        <label>
          Priority
          <input type="number" min={0} max={99} value={d.priority} onChange={(e) => setD({ ...d, priority: Number(e.target.value) })} />
          <span style={{ color: 'var(--text-dim)', fontSize: '0.8rem' }}>
            0 is your main server. A higher number makes it a backup, used only for articles the lower numbers don't have.
          </span>
        </label>
      )}
      {editing && (
        <label style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <input type="checkbox" checked={d.enabled} onChange={(e) => setD({ ...d, enabled: e.target.checked })} />
          Enabled
        </label>
      )}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <button className="primary" onClick={submit} disabled={!d.host.trim() || busy}>
          {submitLabel}
        </button>
        <TestButton
          label="Test connection"
          disabled={!d.host.trim()}
          run={() => api.testUsenetServerConfig({ id: d.id, host: d.host.trim(), port: d.port, useSsl: d.useSsl, username: d.username, password: d.password })}
        />
        {onCancel && <button onClick={onCancel}>Cancel</button>}
        {extra}
      </div>
    </div>
  )
}
