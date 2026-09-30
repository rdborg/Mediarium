import { useState } from 'react'
import { api, type IndexerConfig } from '../api'
import Icon from './Icon'
import SitePicker from './SitePicker'
import { useToast } from './Toast'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { apiKey as apiKeyCheck, firstError, maxLength, required, url as urlCheck } from '../validate'

// Usenet indexers that are open to new members, offered as a quick fill for the
// address. You still need your own account and API key on each one.
export const KNOWN_USENET_INDEXERS = [
  { name: 'NZBgeek', baseUrl: 'https://api.nzbgeek.info', note: 'registration opens periodically' },
  { name: 'NZBFinder', baseUrl: 'https://nzbfinder.ws', note: 'open registration' },
  { name: 'nzb.life', baseUrl: 'https://nzb.life', note: 'open registration' },
  { name: 'NZBPlanet', baseUrl: 'https://api.nzbplanet.org', note: 'free tier available' },
  { name: 'NZBIndex', baseUrl: 'https://nzbindex.com', note: 'no account needed' },
  { name: 'AnimeTosho', baseUrl: 'https://feed.animetosho.org', note: 'anime only, no API key needed' },
] as const

type Protocol = 'usenet' | 'torrent'

// Add an indexer in one place: pick the kind (Usenet or torrent), pick one from
// the list or enter your own address, paste the API key, and add it. It clears
// afterwards so you can add another straight away.
export default function IndexerForm({ onAdded, torrentDisabled }: { onAdded: (i: IndexerConfig) => void; torrentDisabled?: boolean }) {
  const toast = useToast()
  const [protocol, setProtocol] = useState<Protocol>('usenet')
  const [preset, setPreset] = useState('')
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [torrentMode, setTorrentMode] = useState<'list' | 'link'>('list')
  // Add unlocks only after a successful test of exactly these details.
  const [test, setTest] = useState<{ state: 'idle' | 'running' | 'ok' | 'fail'; message: string; forKey: string }>({ state: 'idle', message: '', forKey: '' })

  const custom = preset === '' || preset === '__custom'
  const needsKey = preset !== 'AnimeTosho'

  const addressExample = protocol === 'usenet' ? 'https://api.example.com' : 'https://your-tracker.example/torznab'
  const v = useValidation({
    name: firstError(required(name, 'Give this indexer a name, for example the name of the website.'), maxLength(name, 80, 'The name')),
    baseUrl: firstError(required(baseUrl, `Add the address of the indexer, for example ${addressExample}.`), urlCheck(baseUrl, { requireScheme: true, example: addressExample })),
    apiKey: needsKey ? firstError(required(apiKey, 'Add your API key, from your account page on that site.'), apiKeyCheck(apiKey)) : apiKeyCheck(apiKey),
  })

  function chooseProtocol(p: Protocol) {
    v.reset()
    setProtocol(p)
    setPreset('')
    setName('')
    setBaseUrl('')
  }

  function choosePreset(value: string) {
    v.reset()
    setPreset(value)
    const known = KNOWN_USENET_INDEXERS.find((k) => k.name === value)
    if (known) {
      setName(known.name)
      setBaseUrl(known.baseUrl)
    } else {
      setName('')
      setBaseUrl('')
    }
  }

  const detailsKey = `${protocol}|${baseUrl.trim()}|${apiKey.trim()}`
  const tested = test.state === 'ok' && test.forKey === detailsKey

  async function runTest() {
    if (!v.attempt()) return
    setTest({ state: 'running', message: '', forKey: detailsKey })
    try {
      const r = await api.testIndexerConfig({ name: name || 'test', baseUrl: baseUrl.trim(), apiKey: apiKey.trim() })
      setTest({ state: r.ok ? 'ok' : 'fail', message: friendlyIndexerError(r.message), forKey: detailsKey })
    } catch (e) {
      setTest({ state: 'fail', message: friendlyIndexerError(e instanceof Error ? e.message : String(e)), forKey: detailsKey })
    }
  }

  async function add() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      const created = await api.createIndexer({ name: name.trim(), definitionId: '', baseUrl: baseUrl.trim(), apiKey: apiKey.trim(), protocol })
      toast.success(`${created.name} added.`)
      setPreset('')
      setName('')
      setBaseUrl('')
      setApiKey('')
      setTest({ state: 'idle', message: '', forKey: '' })
      v.reset()
      onAdded(created)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={`indexer-form proto-${protocol}`}>
      <div className="kind-tabs" role="tablist" aria-label="Kind of indexer">
        <button role="tab" aria-selected={protocol === 'usenet'} className={`kind-tab usenet${protocol === 'usenet' ? ' active' : ''}`} onClick={() => chooseProtocol('usenet')}>
          <Icon name="server" size={16} /> Usenet (NZB)
        </button>
        <button role="tab" aria-selected={protocol === 'torrent'} className={`kind-tab torrent${protocol === 'torrent' ? ' active' : ''}`} onClick={() => chooseProtocol('torrent')}>
          <Icon name="magnet" size={16} /> Torrent
        </button>
      </div>
      <div className="kind-panel grid-form" role="tabpanel">
      {protocol === 'torrent' && torrentDisabled && <div className="notice notice-warn">Torrents are off in Settings &gt; Downloading &gt; Usenet and torrents, so a torrent indexer is saved but not searched.</div>}
      {protocol === 'torrent' && (
        <div className="seg" role="radiogroup" aria-label="How to add">
          <button role="radio" aria-checked={torrentMode === 'list'} className={torrentMode === 'list' ? 'active' : ''} onClick={() => setTorrentMode('list')}>
            Pick a site from the list
          </button>
          <button role="radio" aria-checked={torrentMode === 'link'} className={torrentMode === 'link' ? 'active' : ''} onClick={() => setTorrentMode('link')}>
            Paste a Torznab link
          </button>
        </div>
      )}
      {protocol === 'torrent' && torrentMode === 'list' ? (
        <SitePicker protocol="torrent" onAdded={onAdded} />
      ) : (
        <>

      <ol className="steps">
        {protocol === 'usenet' ? (
          <>
            <li>Sign up on the indexer's website and copy your <strong>API key</strong> from your account page.</li>
            <li>Pick the indexer below, or choose Other and type its address.</li>
          </>
        ) : (
          <>
            <li>For sites that give you a <strong>Torznab</strong> link (some private trackers, or your own Prowlarr/Jackett), copy the link and your <strong>API key</strong>.</li>
            <li>Paste both below. For most sites, use <strong>Pick a site from the list</strong> instead.</li>
          </>
        )}
      </ol>

      <div className="form-cols">
        <label>
          Indexer
          <select value={preset || '__custom'} onChange={(e) => choosePreset(e.target.value)}>
            {protocol === 'usenet' && (
              <>
                <option value="__custom">Other (type the address)</option>
                {KNOWN_USENET_INDEXERS.map((k) => (
                  <option key={k.name} value={k.name}>
                    {k.name} ({k.note})
                  </option>
                ))}
              </>
            )}
            {protocol === 'torrent' && <option value="__custom">Other (paste the Torznab address)</option>}
          </select>
        </label>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="The name to show in the app" disabled={!custom} {...v.bind('name', name, setName)} />
          <FieldError v={v} name="name" />
        </label>
        <label>
          Address
          <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder={addressExample} disabled={!custom} {...v.bind('baseUrl', baseUrl, setBaseUrl)} />
          <FieldError v={v} name="baseUrl" />
        </label>
        <label>
          API key {!needsKey && <small style={{ color: 'var(--text-dim)' }}>(not needed for this one)</small>}
          <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} autoComplete="off" spellCheck={false} {...v.bind('apiKey', apiKey, setApiKey)} />
          <FieldError v={v} name="apiKey" />
        </label>
      </div>
      <div className="test-add-row">
        <button className="btn-with-icon" onClick={() => void runTest()} disabled={test.state === 'running'}>
          <Icon name="refresh" size={15} /> {test.state === 'running' ? 'Testing…' : 'Test connection'}
        </button>
        <button className="primary btn-with-icon" onClick={() => void add()} disabled={!tested || busy} title={tested ? '' : 'Test the connection first'}>
          <Icon name="plus" size={15} /> Add this indexer
        </button>
        {!tested && test.state !== 'fail' && <small className="test-hint">Test the connection to unlock Add.</small>}
      </div>
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
        </>
      )}
      </div>
    </div>
  )
}

// Indexer errors come back with internal prefixes ("newznab error from X:",
// "(code 103)"); show just the part a person can act on, plus a hint for the
// common ones.
export function friendlyIndexerError(raw: string): string {
  let m = raw.replace(/^(newznab|torznab) error from [^:]+:\s*/i, '').replace(/\s*\(code \d+\)\s*$/i, '').trim()
  // Network failures: say what went wrong in plain words instead of Go's error text.
  if (/no such host|server misbehaving/i.test(m)) return 'That address couldn\'t be found. Check it for typos.'
  if (/connection refused/i.test(m)) return 'The indexer refused the connection. Check the address and port.'
  if (/timeout|deadline exceeded|i\/o timeout/i.test(m)) return 'The indexer didn\'t answer in time. It may be down, so try again in a few minutes.'
  if (/x509|certificate/i.test(m)) return 'The indexer\'s security certificate isn\'t valid. Check the address (https vs http).'
  if (/ip address .* not approved/i.test(m)) m += '. Add this address to the allowed IPs in your account on the indexer\'s website (changes can take a few minutes).'
  else if (/incorrect user credentials|api key|unauthori[sz]ed|\b401\b/i.test(m)) m += '. Check that the API key is copied correctly.'
  return m
}
