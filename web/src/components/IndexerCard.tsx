import { useState } from 'react'
import { api, type IndexerConfig } from '../api'
import { timeAgo } from '../format'
import Icon from './Icon'
import { friendlyIndexerError } from './IndexerForm'
import { useToast } from './Toast'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { apiKey as apiKeyCheck, firstError, maxLength, required, url as urlCheck } from '../validate'

// One saved indexer: name, type and address on top, actions underneath, the
// last test result on its own line, and an inline form to edit it.
export default function IndexerCard({ indexer: i, onChanged, onToggle, onRemove }: { indexer: IndexerConfig; onChanged: () => void; onToggle: () => void; onRemove: () => void }) {
  const toast = useToast()
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(i.name)
  const [baseUrl, setBaseUrl] = useState(i.baseUrl)
  const [apiKey, setApiKey] = useState('')
  const [saving, setSaving] = useState(false)
  const site = i.kind === 'cardigann'
  const v = useValidation({
    name: firstError(required(name, 'Give this indexer a name.'), maxLength(name, 80, 'The name')),
    ...(site
      ? {}
      : {
          baseUrl: firstError(required(baseUrl, 'Add the address of the indexer, for example https://api.example.com.'), urlCheck(baseUrl, { example: 'https://api.example.com' })),
          apiKey: apiKeyCheck(apiKey),
        }),
  })

  async function test() {
    setTesting(true)
    setResult(null)
    try {
      const r = await api.testIndexer(i.id)
      setResult({ ok: r.ok, message: r.ok ? r.message : friendlyIndexerError(r.message) })
    } catch (e) {
      setResult({ ok: false, message: e instanceof Error ? e.message : String(e) })
    } finally {
      setTesting(false)
    }
  }

  async function setPriority(priority: number) {
    try {
      await api.setIndexerPriority(i.id, priority)
      toast.success(priority === 1 ? `${i.name} is preferred now.` : priority === 3 ? `${i.name} is only used as a last resort now.` : `${i.name} is back to normal.`)
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function save() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      await api.updateIndexer(i.id, { name: name.trim(), ...(site ? {} : { baseUrl: baseUrl.trim(), apiKey: apiKey.trim() }) })
      toast.success(`${name.trim() || i.name} saved.`)
      setEditing(false)
      setApiKey('')
      v.reset()
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className={`indexer-card${i.enabled ? '' : ' is-off'}`}>
      <div className="indexer-top">
        <span className={`tile-ico proto-${i.protocol}`}>
          <Icon name={i.protocol === 'torrent' ? 'magnet' : 'server'} size={18} />
        </span>
        <div className="indexer-info">
          <div className="indexer-title">
            <strong title={i.name}>{i.name}</strong>
            <span className="badge">{i.protocol === 'torrent' ? 'Torrent' : 'Usenet'}</span>
            {!i.enabled && <span className="badge">Off</span>}
          </div>
          <small className="indexer-url" title={i.baseUrl}>
            {i.baseUrl}
          </small>
          {!result && i.lastTestAt && (
            <small className={`indexer-health ${i.lastTestError ? 'bad' : 'ok'}`}>
              {i.lastTestError ? `Last test failed ${timeAgo(i.lastTestAt)}: ${friendlyIndexerError(i.lastTestError)}` : `Last test passed ${timeAgo(i.lastTestAt)}`}
            </small>
          )}
        </div>
        <label className="indexer-priority" title="Between two equally good releases, the one from a preferred indexer wins.">
          <span>Priority</span>
          <select value={i.priority ?? 2} onChange={(e) => void setPriority(Number(e.target.value))}>
            <option value={1}>Preferred</option>
            <option value={2}>Normal</option>
            <option value={3}>Last resort</option>
          </select>
        </label>
      </div>

      <div className="indexer-actions">
        <button className="btn-sm" onClick={() => void test()} disabled={testing}>
          {testing ? 'Testing…' : 'Test'}
        </button>
        <button className="btn-sm" onClick={() => setEditing((v) => !v)} aria-expanded={editing}>
          {editing ? 'Cancel' : 'Edit'}
        </button>
        <button className="btn-sm" onClick={onToggle}>
          {i.enabled ? 'Disable' : 'Enable'}
        </button>
        <button className="btn-sm btn-danger" onClick={onRemove}>
          Remove
        </button>
      </div>

      {result && (
        <div className={`indexer-result ${result.ok ? 'ok' : 'bad'}`}>
          <Icon name={result.ok ? 'check' : 'warning'} size={14} /> {result.message}
        </div>
      )}

      {editing && (
        <div className="indexer-edit grid-form">
          <label>
            Name
            <input value={name} onChange={(e) => setName(e.target.value)} {...v.bind('name', name, setName)} />
            <FieldError v={v} name="name" />
          </label>
          {site ? (
            <small style={{ color: 'var(--text-dim)' }}>This site was added from the list. To change its sign-in details, remove it and add it again.</small>
          ) : (
            <>
              <label>
                Address
                <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} {...v.bind('baseUrl', baseUrl, setBaseUrl)} />
                <FieldError v={v} name="baseUrl" />
              </label>
              <label>
                API key
                <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="Leave empty to keep the saved key" autoComplete="off" {...v.bind('apiKey', apiKey, setApiKey)} />
                <FieldError v={v} name="apiKey" />
              </label>
            </>
          )}
          <div>
            <button className="primary btn-sm" onClick={() => void save()} disabled={saving}>
              {saving ? 'Saving…' : 'Save changes'}
            </button>
            <FormProblem v={v} />
          </div>
        </div>
      )}
    </div>
  )
}
