import { useState } from 'react'
import { api, type IndexerConfig } from '../api'
import Icon from './Icon'
import { friendlyIndexerError } from './IndexerForm'
import { useToast } from './Toast'

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

  async function save() {
    setSaving(true)
    try {
      await api.updateIndexer(i.id, { name: name.trim(), ...(site ? {} : { baseUrl: baseUrl.trim(), apiKey: apiKey.trim() }) })
      toast.success(`${name.trim() || i.name} saved.`)
      setEditing(false)
      setApiKey('')
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
        </div>
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
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {site ? (
            <small style={{ color: 'var(--text-dim)' }}>This site was added from the list. To change its sign-in details, remove it and add it again.</small>
          ) : (
            <>
              <label>
                Address
                <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
              </label>
              <label>
                API key
                <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="Leave empty to keep the saved key" autoComplete="off" />
              </label>
            </>
          )}
          <div>
            <button className="primary btn-sm" onClick={() => void save()} disabled={saving || !name.trim()}>
              {saving ? 'Saving…' : 'Save changes'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
