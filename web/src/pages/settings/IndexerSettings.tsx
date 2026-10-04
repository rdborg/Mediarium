import { useCallback, useEffect, useState } from 'react'
import { api, type FlareSolverrStatus, type IndexerConfig } from '../../api'
import Icon from '../../components/Icon'
import IndexerCard from '../../components/IndexerCard'
import IndexerForm from '../../components/IndexerForm'
import { useToast } from '../../components/Toast'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { url as urlCheck } from '../../validate'

// Indexers: search services for releases. One form adds either kind (Usenet or
// torrent); everything you have added is listed together on the right.
export default function IndexerSettings() {
  const confirm = useConfirm()
  const toast = useToast()
  const [testingAll, setTestingAll] = useState(false)

  // Tests every enabled indexer, one after another, then says how many passed.
  async function testAll() {
    if (!list) return
    setTestingAll(true)
    const on = list.filter((i) => i.enabled)
    const failed: string[] = []
    for (const i of on) {
      try {
        const r = await api.testIndexer(i.id)
        if (!r.ok) failed.push(i.name)
      } catch {
        failed.push(i.name)
      }
    }
    setTestingAll(false)
    reload()
    if (failed.length === 0) toast.success(on.length === 1 ? 'The indexer answered.' : `All ${on.length} indexers answered.`)
    else toast.error(`${failed.length} of ${on.length} did not answer: ${failed.join(', ')}.`)
  }
  const [list, setList] = useState<IndexerConfig[] | null>(null)
  const [torrentOn, setTorrentOn] = useState(true)
  const [flare, setFlare] = useState('')
  const [flareSaved, setFlareSaved] = useState('')
  const [bundled, setBundled] = useState(false)
  const [flareState, setFlareState] = useState<FlareSolverrStatus | null>(null)
  // Blank is fine: it turns the helper off (or falls back to the built-in one).
  const v = useValidation({ flare: urlCheck(flare, { requireScheme: true, example: 'http://flaresolverr:8191' }) })

  const reload = useCallback(() => {
    // A refresh that fails keeps what is showing instead of claiming there are none.
    api.listIndexers().then(setList).catch(() => setList((cur) => cur ?? []))
  }, [])
  useEffect(reload, [reload])
  useLive(reload, 15000)
  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setTorrentOn(s.torrentEnabled !== false)
        setFlare(s.flareSolverrUrl ?? '')
        setFlareSaved(s.flareSolverrUrl ?? '')
        setBundled(s.flareSolverrBundled === true)
      })
      .catch(() => undefined)
  }, [])
  const loadFlareState = useCallback(() => {
    api.flareSolverrStatus().then(setFlareState).catch(() => undefined)
  }, [])
  useEffect(loadFlareState, [loadFlareState, flareSaved])
  useLive(loadFlareState, 20000)

  async function saveFlare() {
    if (!v.attempt()) return
    try {
      const s = await api.putSettings({ flareSolverrUrl: flare.trim() })
      setFlareSaved(s.flareSolverrUrl ?? '')
      toast.success(flare.trim() ? 'FlareSolverr address saved.' : 'FlareSolverr turned off.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(i: IndexerConfig) {
    if (!(await confirm({ title: `Remove ${i.name}?`, body: <p>Searches stop using it. You can add it again later.</p>, confirmLabel: 'Remove indexer', danger: true }))) return
    try {
      await api.deleteIndexer(i.id)
      toast.success(`${i.name} removed.`)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggle(i: IndexerConfig) {
    try {
      await api.setIndexerEnabled(i.id, !i.enabled)
      toast.success(i.enabled ? `${i.name} disabled.` : `${i.name} enabled.`)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ color: 'var(--text-dim)', margin: 0 }}>
        <strong>Indexers</strong> are the sites Mediarium searches for downloads. Sign up on one, then add its address and API key here.
      </p>
      <div className="indexer-cols span-all">
      <fieldset className="group usenet">
        <legend>
          <Icon name="plus" size={14} /> Add an indexer
        </legend>
        <IndexerForm onAdded={reload} torrentDisabled={!torrentOn} />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="list" size={14} /> Your indexers {list && <small>({list.length})</small>}
        </legend>
        {list && list.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>None yet. Pick a site from the list on the left, or paste a link.</p>}
        {list && list.length > 1 && (
          <p style={{ margin: '0 0 12px' }}>
            <button className="btn-sm" disabled={testingAll} onClick={() => void testAll()}>
              {testingAll ? 'Testing…' : 'Test all'}
            </button>
          </p>
        )}
        {list && list.length > 0 && (
          <div className="indexer-cards">
            {list.map((i) => (
              <IndexerCard key={i.id} indexer={i} onChanged={reload} onToggle={() => void toggle(i)} onRemove={() => void remove(i)} />
            ))}
          </div>
        )}
      </fieldset>
      <fieldset className="group torrent flare-card">
        <legend>
          <Icon name="shield" size={14} /> Cloudflare sites (optional)
        </legend>
        {flareState?.configured && (
          <div className={`flare-status ${flareState.running ? 'ok' : 'down'}`} role="status">
            <span className="flare-dot" aria-hidden="true" />
            <strong>{flareState.running ? (bundled && !flareSaved ? 'Built in and running' : 'Connected and running') : 'Not answering right now'}</strong>
            {flareState.running && flareState.version && <small>version {flareState.version}</small>}
          </div>
        )}
        {bundled ? (
          <p style={{ marginTop: 0 }}>
            A Cloudflare helper is built in, so sites with a &quot;checking your browser&quot; page just work. Use the box below only if you'd rather run your own.
          </p>
        ) : (
          <>
            <p style={{ marginTop: 0 }}>
              Some sites show a &quot;checking your browser&quot; page that only a real browser can pass. Run the free <strong>FlareSolverr</strong> helper next to Mediarium and enter its address here. The <code>-full</code> version of Mediarium has it built in.
            </p>
            <details className="how-details" style={{ marginBottom: 12 }}>
              <summary>How to add FlareSolverr to Docker</summary>
              <pre className="code-block">{`  flaresolverr:
    image: ghcr.io/flaresolverr/flaresolverr:latest
    container_name: flaresolverr
    restart: unless-stopped
    environment:
      - TZ=Etc/UTC`}</pre>
              <p>Add this under <code>services:</code> in your compose file, run <code>docker compose up -d</code>, then enter <code>http://flaresolverr:8191</code> below.</p>
            </details>
          </>
        )}
        <label className="flare-field">
          {bundled ? 'Use my own helper instead (optional)' : 'FlareSolverr address'}
          <input value={flare} onChange={(e) => setFlare(e.target.value)} placeholder="http://flaresolverr:8191" {...v.bind('flare', flare, setFlare)} />
          <FieldError v={v} name="flare" />
        </label>
        <button className="primary" onClick={() => void saveFlare()} disabled={flare.trim() === flareSaved}>
          Save
        </button>
        <FormProblem v={v} />
      </fieldset>
      </div>
    </div>
  )
}
