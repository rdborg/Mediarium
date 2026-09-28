import { useEffect, useState } from 'react'
import { useToast } from '../../components/Toast'
import { api, type IndexerConfig } from '../../api'
import Icon from '../../components/Icon'
import TestButton from '../../components/TestButton'

// Usenet indexers that are open to new members, offered as a quick fill for the
// address. You still need your own account and API key on each one.
const KNOWN_USENET_INDEXERS = [
  { name: 'NZBgeek', baseUrl: 'https://api.nzbgeek.info', note: 'registration opens periodically' },
  { name: 'NZBFinder', baseUrl: 'https://nzbfinder.ws', note: 'open registration' },
  { name: 'nzb.life', baseUrl: 'https://nzb.life', note: 'open registration' },
  { name: 'NZBPlanet', baseUrl: 'https://api.nzbplanet.org', note: 'free tier available' },
  { name: 'NZBIndex', baseUrl: 'https://nzbindex.com', note: 'no account needed' },
  { name: 'AnimeTosho', baseUrl: 'https://feed.animetosho.org', note: 'anime only, no API key needed' },
] as const

function IndexersSection() {
  const toast = useToast()
  const [list, setList] = useState<IndexerConfig[]>([])
  const [error, setError] = useState('')

  function reload() {
    api.listIndexers().then(setList).catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }
  useEffect(reload, [])

  async function remove(id: number) {
    await api.deleteIndexer(id)
    reload()
  }

  async function toggle(id: number, enabled: boolean) {
    try {
      await api.setIndexerEnabled(id, enabled)
      toast.success(enabled ? 'Indexer enabled.' : 'Indexer disabled: it will be skipped in searches.')
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const [torrentOn, setTorrentOn] = useState(true)
  useEffect(() => {
    api.getSettings().then((s) => setTorrentOn(s.torrentEnabled !== false)).catch(() => undefined)
  }, [])

  const usenetList = list.filter((i) => i.protocol === 'usenet')
  const torrentList = list.filter((i) => i.protocol === 'torrent')

  return (
    <>
      <p className="span-all" style={{ color: 'var(--text-dim)', margin: 0 }}>
        An <strong>indexer</strong> is a search service for releases. Mediarium asks it whether a movie or episode is available, then downloads the best match with its built-in downloader. You sign up on the indexer's own website, then paste its address and API key here.
      </p>
      <fieldset className="group usenet">
        <legend>
          <Icon name="server" size={14} /> Usenet indexers
        </legend>
        <div className="group-cols">
          <IndexerTable title="Your Usenet indexers" list={usenetList} error={error} onRemove={remove} onToggle={toggle} />
          <UsenetAddForm onAdded={reload} onError={setError} />
        </div>
      </fieldset>
      <fieldset className={`group torrent${torrentOn ? '' : ' disabled'}`}>
        <legend>
          <Icon name="magnet" size={14} /> Torrent indexers
        </legend>
        {!torrentOn && <p style={{ margin: '0 0 12px' }}>Torrents are turned off in Settings, Downloads, so these are not searched.</p>}
        <fieldset disabled={!torrentOn} className="plain-fieldset">
          <div className="group-cols">
            <IndexerTable title="Your torrent indexers" list={torrentList} error="" onRemove={remove} onToggle={toggle} />
            <TorrentAddForm onAdded={reload} onError={setError} />
          </div>
        </fieldset>
      </fieldset>
    </>
  )
}

function IndexerTable({
  title,
  list,
  error,
  onRemove,
  onToggle,
}: {
  title: string
  list: IndexerConfig[]
  error: string
  onRemove: (id: number) => void
  onToggle: (id: number, enabled: boolean) => void
}) {
  return (
    <section className="card">
      <h2>{title}</h2>
      {error && <p className="error-text">{error}</p>}
      {list.length === 0 ? (
        <p style={{ color: 'var(--text-dim)' }}>None configured yet.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Base URL</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {list.map((i) => (
              <tr key={i.id}>
                <td style={i.enabled ? undefined : { opacity: 0.5 }}>{i.name}</td>
                <td style={i.enabled ? undefined : { opacity: 0.5 }}>{i.baseUrl}</td>
                <td style={{ whiteSpace: 'nowrap' }}>
                  <TestButton run={() => api.testIndexer(i.id)} />{' '}
                  <button onClick={() => onToggle(i.id, !i.enabled)}>{i.enabled ? 'Disable' : 'Enable'}</button>{' '}
                  <button onClick={() => onRemove(i.id)}>Remove</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function UsenetAddForm({ onAdded, onError }: { onAdded: () => void; onError: (e: string) => void }) {
  const [preset, setPreset] = useState<string>('')
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')

  function applyPreset(presetName: string) {
    setPreset(presetName)
    const known = KNOWN_USENET_INDEXERS.find((k) => k.name === presetName)
    if (known) {
      setName(known.name)
      setBaseUrl(known.baseUrl)
    }
  }

  async function add() {
    try {
      await api.createIndexer({ name, definitionId: '', baseUrl, apiKey, protocol: 'usenet' })
      setPreset('')
      setName('')
      setBaseUrl('')
      setApiKey('')
      onAdded()
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="card grid-form">
      <h3 style={{ margin: 0 }}>Add a Usenet indexer</h3>
      <ol className="steps">
        <li>Sign up on the indexer's website.</li>
        <li>Open your account page and copy your <strong>API key</strong>.</li>
        <li>Pick the indexer below (or type its address), paste the key, and press Test connection.</li>
      </ol>
      <label>
        Quick add (optional)
        <select value={preset} onChange={(e) => applyPreset(e.target.value)}>
          <option value="">— pick a known indexer, or fill in manually below —</option>
          {KNOWN_USENET_INDEXERS.map((k) => (
            <option key={k.name} value={k.name}>
              {k.name} ({k.note})
            </option>
          ))}
        </select>
      </label>
      <label>
        Name
        <input value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label>
        Base URL
        <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.example.com" />
      </label>
      <label>
        API key {preset === 'AnimeTosho' && '(not required for AnimeTosho)'}
        <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
      </label>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <button className="primary" onClick={add} disabled={!name || !baseUrl}>
          Add indexer
        </button>
        <TestButton run={() => api.testIndexerConfig({ name, baseUrl, apiKey })} label="Test connection" disabled={!baseUrl} />
      </div>
    </section>
  )
}

function TorrentAddForm({ onAdded, onError }: { onAdded: () => void; onError: (e: string) => void }) {
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')

  async function add() {
    try {
      await api.createIndexer({ name, definitionId: '', baseUrl, apiKey, protocol: 'torrent' })
      setName('')
      setBaseUrl('')
      setApiKey('')
      onAdded()
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="card grid-form">
      <h3 style={{ margin: 0 }}>Add a torrent indexer</h3>
      <ol className="steps">
        <li>On your torrent site, open your account or settings page and look for a <strong>Torznab</strong> or <strong>API</strong> section.</li>
        <li>Copy the <strong>Torznab address</strong> (a link) and your <strong>API key</strong>.</li>
        <li>Paste both below and press Test connection. Sites that do not offer Torznab cannot be added.</li>
      </ol>
      <details className="how-details">
        <summary>My torrent site has no Torznab address</summary>
        <p>
          Most public torrent sites do not offer one themselves. A small search-proxy program that you run alongside Mediarium (Jackett and Prowlarr are the well-known ones) can give any of those sites a Torznab address to paste here. Private trackers often show one on your profile page.
        </p>
      </details>
      <label>
        Name
        <input value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label>
        Base URL
        <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://your-tracker.example/torznab" />
      </label>
      <label>
        API key
        <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
      </label>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <button className="primary" onClick={add} disabled={!name || !baseUrl}>
          Add indexer
        </button>
        <TestButton run={() => api.testIndexerConfig({ name, baseUrl, apiKey })} label="Test connection" disabled={!baseUrl} />
      </div>
    </section>
  )
}

export default function IndexerSettings() {
  return (
    <div className="settings-stack">
      <IndexersSection />
    </div>
  )
}
