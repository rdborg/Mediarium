import { useState, type ReactNode } from 'react'
import { api } from '../api'
import { USENET_PROVIDERS } from '../usenetProviders'
import Icon from './Icon'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { firstError, hostOrIP, maxLength, numberRange, port as portCheck, required } from '../validate'

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
  connections: 10,
  priority,
  enabled: true,
})

// Add/edit form for a Usenet server: the news-server account your Usenet
// provider gave you. Mediarium's built-in downloader connects to it directly,
// so this is the only thing needed to download from Usenet. The save button
// unlocks once a connection test with exactly these details has passed.
export default function UsenetServerForm({
  initial,
  submitLabel,
  onSubmit,
  onCancel,
  showPriority = true,
  left,
  right,
}: {
  initial: UsenetServerDraft
  submitLabel: string
  onSubmit: (draft: UsenetServerDraft) => void | Promise<void>
  onCancel?: () => void
  showPriority?: boolean
  left?: ReactNode
  right?: ReactNode
}) {
  const [d, setD] = useState<UsenetServerDraft>(initial)
  const [preset, setPreset] = useState('')
  const [busy, setBusy] = useState(false)
  const [test, setTest] = useState<{ state: 'idle' | 'running' | 'ok' | 'fail'; message: string; forKey: string }>({ state: 'idle', message: '', forKey: '' })
  const editing = d.id !== undefined
  const detailsKey = `${d.host.trim()}|${d.port}|${d.useSsl}|${d.username}|${d.password}`
  const tested = test.state === 'ok' && test.forKey === detailsKey
  const v = useValidation({
    name: maxLength(d.name, 80, 'The name'),
    host: firstError(required(d.host, 'Add the address of your Usenet server, for example news.example.com.'), hostOrIP(d.host, 'news.example.com')),
    port: firstError(required(d.port || '', 'Port must be a number between 1 and 65535.'), portCheck(d.port)),
    username: d.password.trim() ? required(d.username, 'Add the username that goes with this password.') : null,
    connections: numberRange(d.connections, 'Connections', { min: 1, max: 100, allowBlank: false }),
    ...(showPriority ? { priority: numberRange(d.priority, 'Priority', { min: 0, max: 99, allowBlank: false }) } : {}),
  })

  function applyPreset(name: string) {
    setPreset(name)
    const p = USENET_PROVIDERS.find((x) => x.name === name)
    if (!p) return
    setD((cur) => ({ ...cur, name: p.name, host: p.host, port: p.port, useSsl: p.useSsl, connections: p.connections }))
  }

  async function runTest() {
    if (!v.attempt()) return
    setTest({ state: 'running', message: '', forKey: detailsKey })
    try {
      const r = await api.testUsenetServerConfig({ id: d.id, host: d.host.trim(), port: d.port, useSsl: d.useSsl, username: d.username, password: d.password })
      setTest({ state: r.ok ? 'ok' : 'fail', message: r.message, forKey: detailsKey })
    } catch (e) {
      setTest({ state: 'fail', message: e instanceof Error ? e.message : String(e), forKey: detailsKey })
    }
  }

  async function submit() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      await onSubmit(d)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid-form">
      <div className="form-cols">
        {!editing && (
          <label>
            Provider
            <select value={preset} onChange={(e) => applyPreset(e.target.value)}>
              <option value="">Other (type the details)</option>
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
          <input value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} placeholder="e.g. My provider" {...v.bind('name', d.name, (t) => setD((c) => ({ ...c, name: t })))} />
          <FieldError v={v} name="name" />
        </label>
        <label>
          Server host
          <input value={d.host} onChange={(e) => setD({ ...d, host: e.target.value })} placeholder="news.example.com" {...v.bind('host', d.host, (t) => setD((c) => ({ ...c, host: t })))} />
          <FieldError v={v} name="host" />
        </label>
        <label>
          Port
          <input type="number" value={d.port} onChange={(e) => setD({ ...d, port: Number(e.target.value) })} {...v.bind('port')} />
          <FieldError v={v} name="port" />
        </label>
        <label>
          Username
          <input value={d.username} onChange={(e) => setD({ ...d, username: e.target.value })} autoComplete="off" {...v.bind('username', d.username, (t) => setD((c) => ({ ...c, username: t })))} />
          <FieldError v={v} name="username" />
        </label>
        <label>
          Password
          <input
            type="password"
            value={d.password}
            onChange={(e) => setD({ ...d, password: e.target.value })}
            placeholder={editing && d.hasPassword ? 'leave blank to keep the saved password' : ''}
            autoComplete="new-password"
            {...v.bind('password')}
          />
        </label>
        <label>
          Connections
          <input type="number" min={1} max={100} value={d.connections} onChange={(e) => setD({ ...d, connections: Number(e.target.value) })} {...v.bind('connections')} />
          <FieldError v={v} name="connections" />
          <small className="field-hint">Start with 10. Stay within your plan's limit, and count any other program using the same login.</small>
        </label>
        {showPriority && (
          <label>
            Priority
            <input type="number" min={0} max={99} value={d.priority} onChange={(e) => setD({ ...d, priority: Number(e.target.value) })} {...v.bind('priority')} />
            <FieldError v={v} name="priority" />
            <small className="field-hint">0 is the main provider. Higher numbers are backups.</small>
          </label>
        )}
        <div className="cell-check">
          <span className="cell-label">Security</span>
          <label className="check-row">
            <input type="checkbox" checked={d.useSsl} onChange={(e) => setD({ ...d, useSsl: e.target.checked, port: e.target.checked && d.port === 119 ? 563 : !e.target.checked && d.port === 563 ? 119 : d.port })} />
            Use SSL (recommended)
          </label>
        </div>
        {editing && (
          <div className="cell-check">
            <span className="cell-label">Status</span>
            <label className="check-row">
              <input type="checkbox" checked={d.enabled} onChange={(e) => setD({ ...d, enabled: e.target.checked })} />
              Enabled
            </label>
          </div>
        )}
      </div>
      <div className="form-actions">
        <div className="fa-left">
          {left}
          {onCancel && <button onClick={onCancel}>Cancel</button>}
        </div>
        <div className="fa-mid">
          <button className="btn-with-icon" onClick={() => void runTest()} disabled={test.state === 'running'}>
            <Icon name="refresh" size={15} /> {test.state === 'running' ? 'Testing…' : 'Test connection'}
          </button>
          <button className="primary btn-with-icon" onClick={() => void submit()} disabled={!tested || busy} title={tested ? '' : 'Test the connection first'}>
            <Icon name="plus" size={15} /> {submitLabel}
          </button>
        </div>
        <div className="fa-right">{right}</div>
      </div>
      {!tested && test.state !== 'fail' && <small className="test-hint">Test the connection to unlock {submitLabel}.</small>}
      <FormProblem v={v} verb="test the connection" />
      {test.forKey === detailsKey && test.state === 'ok' && (
        <div className="test-result ok">
          <Icon name="check" size={15} /> {test.message || 'Connected.'}
        </div>
      )}
      {test.forKey === detailsKey && test.state === 'fail' && (
        <div className="test-result fail">
          <Icon name="warning" size={15} /> {test.message}
        </div>
      )}
    </div>
  )
}
