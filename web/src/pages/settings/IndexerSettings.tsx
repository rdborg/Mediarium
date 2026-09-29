import { useCallback, useEffect, useState } from 'react'
import { api, type IndexerConfig } from '../../api'
import Icon from '../../components/Icon'
import IndexerCard from '../../components/IndexerCard'
import IndexerForm from '../../components/IndexerForm'
import { useToast } from '../../components/Toast'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'

// Indexers: search services for releases. One form adds either kind (Usenet or
// torrent); everything you have added is listed together on the right.
export default function IndexerSettings() {
  const confirm = useConfirm()
  const toast = useToast()
  const [list, setList] = useState<IndexerConfig[] | null>(null)
  const [torrentOn, setTorrentOn] = useState(true)
  const [flare, setFlare] = useState('')
  const [flareSaved, setFlareSaved] = useState('')

  const reload = useCallback(() => {
    api.listIndexers().then(setList).catch(() => setList([]))
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
      })
      .catch(() => undefined)
  }, [])

  async function saveFlare() {
    try {
      const s = await api.putSettings({ flareSolverrUrl: flare.trim() })
      setFlareSaved(s.flareSolverrUrl ?? '')
      toast.success(flare.trim() ? 'FlareSolverr address saved.' : 'FlareSolverr turned off.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(i: IndexerConfig) {
    if (!(await confirm({ title: `Remove ${i.name}?`, body: <p>Searches stop using this indexer. You can add it again later.</p>, confirmLabel: 'Remove indexer', danger: true }))) return
    await api.deleteIndexer(i.id)
    toast.success(`${i.name} removed.`)
    reload()
  }

  async function toggle(i: IndexerConfig) {
    try {
      await api.setIndexerEnabled(i.id, !i.enabled)
      toast.success(i.enabled ? `${i.name} disabled: it is skipped in searches.` : `${i.name} enabled.`)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ color: 'var(--text-dim)', margin: 0 }}>
        An <strong>indexer</strong> is a search service for releases. Mediarium asks it whether a movie or episode is available, then downloads the best match with its built-in downloader. You sign up on the indexer's own website, then add its address and API key here. Add as many as you like.
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
        {list && list.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>None yet. Add your first with the form on the left: pick a site from the list, or paste a link.</p>}
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
        <p style={{ marginTop: 0 }}>
          Some public sites show a &quot;checking your browser&quot; page that only a real browser can pass. For those, run the free <strong>FlareSolverr</strong> helper next to Mediarium and enter its address here. Sites that need it tell you when you test them; everything else works without it.
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
        <label className="flare-field">
          FlareSolverr address
          <input value={flare} onChange={(e) => setFlare(e.target.value)} placeholder="http://flaresolverr:8191" />
        </label>
        <button className="primary" onClick={() => void saveFlare()} disabled={flare.trim() === flareSaved}>
          Save
        </button>
      </fieldset>
      </div>
    </div>
  )
}
