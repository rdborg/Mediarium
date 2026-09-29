import { useCallback, useEffect, useState } from 'react'
import { api, type MediaServer, type MediaServerKind, type MediaServerTest, type PathMapping } from '../../api'
import { useConfirm } from '../../components/ConfirmProvider'
import Icon from '../../components/Icon'
import { MEDIA_SERVER_BRAND, MediaServerMark } from '../../components/mediaServerBrand'
import { FindServers, JellyfinEmbySignIn, PlexSignIn } from '../../components/MediaServerSignIn'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import { DOCS_URL } from '../../docs'
import { timeAgo } from '../../format'
import { useLive } from '../../useLive'

const KINDS: MediaServerKind[] = ['plex', 'jellyfin', 'emby']

const TOKEN_HELP: Record<MediaServerKind, string> = {
  plex: 'Your Plex token. In Plex Web, open any movie, choose ⋯ → Get Info → View XML; the token is the X-Plex-Token part of the address that opens.',
  jellyfin: 'An API key. In Jellyfin: Dashboard → API Keys → + (name it Mediarium).',
  emby: 'An API key. In Emby: Settings → Advanced → API Keys → New API Key (name it Mediarium).',
}

const ADDRESS_HINT: Record<MediaServerKind, string> = {
  plex: 'http://192.168.1.10:32400',
  jellyfin: 'http://192.168.1.10:8096',
  emby: 'http://192.168.1.10:8096',
}

function TestResult({ r }: { r: MediaServerTest }) {
  if (!r.ok) {
    return (
      <div className="indexer-result bad">
        <Icon name="warning" size={14} /> {r.error ?? 'The test failed.'}
      </div>
    )
  }
  return (
    <div className="indexer-result ok">
      <Icon name="check" size={14} />
      <span>
        Connected to <strong>{r.serverName ?? 'the server'}</strong>
        {r.version ? ` (version ${r.version})` : ''}.
        {r.libraries && r.libraries.length > 0 && <> Libraries: {r.libraries.map((l) => l.title).join(', ')}.</>}
      </span>
    </div>
  )
}

function PathMapEditor({ value, onChange }: { value: PathMapping[]; onChange: (v: PathMapping[]) => void }) {
  return (
    <div className="pathmap">
      {value.map((m, i) => (
        <div key={i} className="pathmap-row">
          <input value={m.from} onChange={(e) => onChange(value.map((x, j) => (j === i ? { ...x, from: e.target.value } : x)))} placeholder="/movies" aria-label="Folder in Mediarium" />
          <Icon name="open" size={14} />
          <input value={m.to} onChange={(e) => onChange(value.map((x, j) => (j === i ? { ...x, to: e.target.value } : x)))} placeholder="/data/movies" aria-label="Same folder on the media server" />
          <button className="icon-btn" onClick={() => onChange(value.filter((_, j) => j !== i))} aria-label="Remove this mapping">
            <Icon name="x" size={14} />
          </button>
        </div>
      ))}
      <button className="btn-sm btn-with-icon" onClick={() => onChange([...value, { from: '', to: '' }])}>
        <Icon name="plus" size={14} /> Add a folder mapping
      </button>
    </div>
  )
}

function AddForm({ onAdded }: { onAdded: () => void }) {
  const toast = useToast()
  const [kind, setKind] = useState<MediaServerKind>('plex')
  const [baseUrl, setBaseUrl] = useState('')
  const [token, setToken] = useState('')
  const [publicUrl, setPublicUrl] = useState('')
  const [pathMap, setPathMap] = useState<PathMapping[]>([])
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<MediaServerTest | null>(null)
  const [saving, setSaving] = useState(false)
  const brand = MEDIA_SERVER_BRAND[kind]
  const input = { kind, baseUrl: baseUrl.trim(), token: token.trim(), publicUrl: publicUrl.trim() || undefined, pathMap: pathMap.filter((m) => m.from && m.to) }

  function choose(k: MediaServerKind) {
    setKind(k)
    setResult(null)
  }

  async function test() {
    setTesting(true)
    setResult(null)
    try {
      setResult(await api.testMediaServerConfig(input))
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : String(e) })
    } finally {
      setTesting(false)
    }
  }

  async function add() {
    setSaving(true)
    try {
      await api.createMediaServer({ ...input, name: result?.serverName || brand.label })
      toast.success(`${brand.label} added. New downloads now show up there by themselves.`)
      setBaseUrl('')
      setToken('')
      setPublicUrl('')
      setPathMap([])
      setResult(null)
      onAdded()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const manual = (
    <>
      {kind === 'plex' && (
        <label>
          Address
          <input value={baseUrl} onChange={(e) => { setBaseUrl(e.target.value); setResult(null) }} placeholder={ADDRESS_HINT[kind]} />
          <small>How Mediarium reaches the server. Inside Docker, use the server&apos;s IP address or container name, not localhost.</small>
        </label>
      )}
      <label>
        {kind === 'plex' ? 'Plex token' : 'API key'}
        <input value={token} onChange={(e) => { setToken(e.target.value); setResult(null) }} autoComplete="off" />
        <small>{TOKEN_HELP[kind]}</small>
      </label>
      <details className="how-details">
        <summary>More options</summary>
        <div className="grid-form" style={{ marginTop: 10 }}>
          <label>
            Address you open in your browser (optional)
            <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} placeholder="https://jellyfin.example.com" />
            <small>Used for the &quot;Watch in {brand.label}&quot; buttons when it differs from the address above.</small>
          </label>
          <div>
            <strong style={{ fontSize: '0.9rem' }}>Folder mapping (optional)</strong>
            <p style={{ margin: '4px 0 8px', color: 'var(--text-dim)', fontSize: '0.85rem' }}>
              Only needed when {brand.label} sees your library under a different path, for example Mediarium&apos;s <code>/movies</code> is <code>/data/movies</code> in {brand.label}.
            </p>
            <PathMapEditor value={pathMap} onChange={setPathMap} />
          </div>
        </div>
      </details>
      <div className="form-actions">
        <button onClick={() => void test()} disabled={testing || !baseUrl.trim() || !token.trim()}>
          {testing ? 'Testing…' : 'Test'}
        </button>
        <button className="primary" onClick={() => void add()} disabled={!result?.ok || saving} title={result?.ok ? undefined : 'Test the connection first'}>
          {saving ? 'Adding…' : `Add ${brand.label}`}
        </button>
      </div>
      {result && <TestResult r={result} />}
    </>
  )

  return (
    <div className="grid-form">
      <FindServers
        onPick={(f) => {
          setKind(f.kind)
          setBaseUrl(f.address)
          setResult(null)
        }}
      />
      <div className="indexer-form ms-form" style={{ ['--kc' as string]: brand.color }}>
        <div className="kind-tabs" role="tablist" aria-label="Kind of media server">
          {KINDS.map((k) => (
            <button key={k} role="tab" aria-selected={kind === k} className={`kind-tab${kind === k ? ' active' : ''}`} style={{ ['--tk' as string]: MEDIA_SERVER_BRAND[k].color }} onClick={() => choose(k)}>
              <MediaServerMark kind={k} size={18} /> {MEDIA_SERVER_BRAND[k].label}
            </button>
          ))}
        </div>
        <div className="kind-panel grid-form" role="tabpanel">
          {kind === 'plex' ? (
            <>
              <PlexSignIn onAdded={onAdded} />
              <details className="how-details">
                <summary>Or enter the address and token by hand</summary>
                <div className="grid-form" style={{ marginTop: 10 }}>
                  {manual}
                </div>
              </details>
            </>
          ) : (
            <>
              <label>
                Address
                <input value={baseUrl} onChange={(e) => { setBaseUrl(e.target.value); setResult(null) }} placeholder={ADDRESS_HINT[kind]} />
                <small>How Mediarium reaches the server. Inside Docker, use the server&apos;s IP address or container name, not localhost.</small>
              </label>
              <JellyfinEmbySignIn kind={kind} baseUrl={baseUrl} onAdded={onAdded} />
              <details className="how-details">
                <summary>Or enter an API key by hand</summary>
                <div className="grid-form" style={{ marginTop: 10 }}>
                  {manual}
                </div>
              </details>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function ServerCard({ s, onChanged }: { s: MediaServer; onChanged: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [busy, setBusy] = useState('')
  const [result, setResult] = useState<MediaServerTest | null>(null)
  const [editing, setEditing] = useState(false)
  const [baseUrl, setBaseUrl] = useState(s.baseUrl)
  const [token, setToken] = useState('')
  const [publicUrl, setPublicUrl] = useState(s.publicUrl === s.baseUrl ? '' : s.publicUrl)
  const [pathMap, setPathMap] = useState<PathMapping[]>(s.pathMap ?? [])

  async function run(label: string, fn: () => Promise<void>) {
    setBusy(label)
    try {
      await fn()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  const update = (data: Parameters<typeof api.updateMediaServer>[1], msg?: string) =>
    run('save', async () => {
      await api.updateMediaServer(s.id, data)
      if (msg) toast.success(msg)
      onChanged()
    })

  return (
    <div className={`indexer-card ms-card${s.enabled ? '' : ' is-off'}`} style={{ ['--ms' as string]: MEDIA_SERVER_BRAND[s.kind].color }}>
      <div className="indexer-top">
        <MediaServerMark kind={s.kind} size={34} />
        <div className="indexer-info">
          <div className="indexer-title">
            <strong>{s.name}</strong>
            <span className="badge">{MEDIA_SERVER_BRAND[s.kind].label}</span>
            {!s.enabled && <span className="badge">Off</span>}
          </div>
          <small className="indexer-url">{s.baseUrl}</small>
          {s.lastError ? (
            <small className="error-text">Last check failed: {s.lastError}</small>
          ) : (
            s.lastCheckedAt && <small style={{ color: 'var(--text-dim)' }}>Last checked {timeAgo(s.lastCheckedAt)}</small>
          )}
        </div>
      </div>
      <Switch
        checked={s.refreshAfterImport}
        onChange={(v) => void update({ refreshAfterImport: v }, v ? `${s.name} is refreshed after every download.` : `${s.name} is no longer refreshed automatically.`)}
        label="Refresh after every download"
        description={`${MEDIA_SERVER_BRAND[s.kind].label} scans just the new folder, so new movies and episodes appear there within a minute.`}
      />
      <div className="indexer-actions">
        <button className="btn-sm" disabled={!!busy} onClick={() => void run('test', async () => setResult(await api.testMediaServer(s.id)))}>
          {busy === 'test' ? 'Testing…' : 'Test'}
        </button>
        <button
          className="btn-sm"
          disabled={!!busy}
          onClick={() =>
            void run('refresh', async () => {
              const r = await api.refreshMediaServer(s.id)
              if (r.ok) toast.success(`${s.name} is scanning your libraries.`)
              else toast.error(r.error ?? 'The refresh failed.')
            })
          }
        >
          {busy === 'refresh' ? 'Refreshing…' : 'Refresh now'}
        </button>
        <a className="btn btn-sm btn-with-icon" href={s.webUrl || s.publicUrl || s.baseUrl} target="_blank" rel="noreferrer">
          <Icon name="external" size={14} /> Open
        </a>
        <button className="btn-sm" onClick={() => setEditing((v) => !v)} aria-expanded={editing}>
          {editing ? 'Cancel' : 'Edit'}
        </button>
        <button className="btn-sm" disabled={!!busy} onClick={() => void update({ enabled: !s.enabled }, s.enabled ? `${s.name} turned off.` : `${s.name} turned on.`)}>
          {s.enabled ? 'Disable' : 'Enable'}
        </button>
        <button
          className="btn-sm btn-danger"
          onClick={async () => {
            if (!(await confirm({ title: `Remove ${s.name}?`, body: <p>Mediarium stops telling it about new downloads. Nothing on the media server changes.</p>, confirmLabel: 'Remove', danger: true }))) return
            await run('remove', async () => {
              await api.deleteMediaServer(s.id)
              toast.success(`${s.name} removed.`)
              onChanged()
            })
          }}
        >
          Remove
        </button>
      </div>
      {result && <TestResult r={result} />}
      {editing && (
        <div className="indexer-edit grid-form">
          <label>
            Address
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
          </label>
          <label>
            {s.kind === 'plex' ? 'Plex token' : 'API key'}
            <input value={token} onChange={(e) => setToken(e.target.value)} placeholder="Leave empty to keep the saved one" autoComplete="off" />
          </label>
          <label>
            Address you open in your browser (optional)
            <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} />
          </label>
          <div>
            <strong style={{ fontSize: '0.9rem' }}>Folder mapping</strong>
            <PathMapEditor value={pathMap} onChange={setPathMap} />
          </div>
          <div>
            <button
              className="primary btn-sm"
              disabled={!!busy}
              onClick={() =>
                void update({ baseUrl: baseUrl.trim(), token: token.trim() || undefined, publicUrl: publicUrl.trim(), pathMap: pathMap.filter((m) => m.from && m.to) }, `${s.name} saved.`).then(() => {
                  setEditing(false)
                  setToken('')
                })
              }
            >
              Save changes
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// Settings → Media Servers: Plex, Jellyfin and Emby. After each download the
// right library is refreshed, and title pages get "Watch in …" buttons.
export default function MediaServerSettings() {
  const [list, setList] = useState<MediaServer[] | null>(null)
  const reload = useCallback(() => {
    api.listMediaServers().then(setList).catch(() => setList([]))
  }, [])
  useEffect(reload, [reload])
  useLive(reload, 30000)

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ color: 'var(--text-dim)', margin: 0 }}>
        Connect the app you watch with. After every download Mediarium tells it to look at the new folder, so movies and episodes appear there
        within a minute, and title pages here get a <strong>Watch in Plex</strong> (or Jellyfin, Emby) button.{' '}
        <a href={`${DOCS_URL}/media-servers.md`} target="_blank" rel="noreferrer">
          How to set it up
        </a>
      </p>
      <div className="half-cols span-all">
        <fieldset className="group usenet">
          <legend>
            <Icon name="plus" size={14} /> Add a media server
          </legend>
          <AddForm onAdded={reload} />
        </fieldset>
        <fieldset className="group folders">
          <legend>
            <Icon name="monitor" size={14} /> Your media servers {list && <small>({list.length})</small>}
          </legend>
          {list === null && <div className="skeleton" style={{ height: 120 }} />}
          {list && list.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>None yet. Pick Plex, Jellyfin or Emby on the left, enter its address and key, test, and add it.</p>}
          {list && list.length > 0 && (
            <div className="indexer-cards">
              {list.map((s) => (
                <ServerCard key={s.id} s={s} onChanged={reload} />
              ))}
            </div>
          )}
        </fieldset>
      </div>
    </div>
  )
}
