import { useCallback, useEffect, useState } from 'react'
import { api, type MediaServer, type MediaServerKind, type MediaServerTest, type PathMapping } from '../../api'
import { useConfirm } from '../../components/ConfirmProvider'
import Icon from '../../components/Icon'
import WatchedSettings from '../../components/WatchedSettings'
import { MEDIA_SERVER_BRAND, MediaServerMark } from '../../components/mediaServerBrand'
import { FindServers, JellyfinEmbySignIn, PlexSignIn } from '../../components/MediaServerSignIn'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import { DOCS_URL } from '../../docs'
import { timeAgo } from '../../format'
import { FieldError, FormProblem, useValidation, type Validation } from '../../useValidation'
import { firstError, folderPath, required, apiKey as apiKeyCheck, url as urlCheck } from '../../validate'
import { useLive } from '../../useLive'
import { useModules } from '../../ModulesContext'

const KINDS: MediaServerKind[] = ['plex', 'jellyfin', 'emby']
// The apps for reading and listening, offered while ebooks or audiobooks are on.
const BOOK_KINDS: MediaServerKind[] = ['audiobookshelf', 'kavita']
const isBookKind = (k: MediaServerKind) => BOOK_KINDS.includes(k)

const TOKEN_HELP: Record<MediaServerKind, string> = {
  plex: 'Your Plex token. In Plex Web, open any movie, choose ⋯ → Get Info → View XML; the token is the X-Plex-Token part of the address that opens.',
  jellyfin: 'An API key. In Jellyfin: Dashboard → API Keys → + (name it Mediarium).',
  emby: 'An API key. In Emby: Settings → Advanced → API Keys → New API Key (name it Mediarium).',
  audiobookshelf: 'An API token. In Audiobookshelf: Settings → API Keys → Add API Key (older versions: Settings → Users → your user → API Token).',
  kavita: 'Your API key. In Kavita: open your user settings (your name, top right) → 3rd Party Clients, and copy the API key.',
}

const ADDRESS_HINT: Record<MediaServerKind, string> = {
  plex: 'http://192.168.1.10:32400',
  jellyfin: 'http://192.168.1.10:8096',
  emby: 'http://192.168.1.10:8096',
  audiobookshelf: 'http://192.168.1.10:13378',
  kavita: 'http://192.168.1.10:5000',
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

// One row is "unfinished" when only one side is filled in; a fully blank row is ignored on save.
function pathMapErrors(rows: PathMapping[], prefix = 'map'): Record<string, string | null> {
  const out: Record<string, string | null> = {}
  rows.forEach((m, i) => {
    const from = m.from.trim()
    const to = m.to.trim()
    out[`${prefix}${i}.from`] = firstError(!from && to ? required(from, 'Enter the folder as Mediarium sees it, for example /movies.') : null, folderPath(from, '/movies'))
    out[`${prefix}${i}.to`] = firstError(from && !to ? required(to, 'Enter the same folder as the media server sees it, for example /data/movies.') : null, folderPath(to, '/data/movies'))
  })
  return out
}

function PathMapEditor({ value, onChange, v, prefix = 'map' }: { value: PathMapping[]; onChange: (v: PathMapping[]) => void; v: Validation; prefix?: string }) {
  return (
    <div className="pathmap">
      {value.map((m, i) => (
        <div key={i} className="pathmap-row">
          <div>
            <input
              value={m.from}
              onChange={(e) => onChange(value.map((x, j) => (j === i ? { ...x, from: e.target.value } : x)))}
              placeholder="/movies"
              aria-label="Folder in Mediarium"
              {...v.bind(`${prefix}${i}.from`, m.from, (t) => onChange(value.map((x, j) => (j === i ? { ...x, from: t } : x))))}
            />
            <FieldError v={v} name={`${prefix}${i}.from`} />
          </div>
          <Icon name="open" size={14} />
          <div>
            <input
              value={m.to}
              onChange={(e) => onChange(value.map((x, j) => (j === i ? { ...x, to: e.target.value } : x)))}
              placeholder="/data/movies"
              aria-label="Same folder on the media server"
              {...v.bind(`${prefix}${i}.to`, m.to, (t) => onChange(value.map((x, j) => (j === i ? { ...x, to: t } : x))))}
            />
            <FieldError v={v} name={`${prefix}${i}.to`} />
          </div>
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

const KIND_HINT: Record<MediaServerKind, string> = {
  plex: 'Sign in with your Plex account. No token to find or copy.',
  jellyfin: 'Quick Connect, or an administrator login. No key to copy.',
  emby: 'An administrator login, or an API key.',
  audiobookshelf: 'Listen to your audiobooks, with apps for phones. Needs an API token.',
  kavita: 'Read your ebooks and comics in the browser. Needs an API key.',
}

// The form for one kind of media server. All three stay mounted while you
// look at the others, so nothing you typed is lost until you add the server.
function KindPanel({
  kind,
  hidden,
  baseUrl,
  setBaseUrl,
  onAdded,
}: {
  kind: MediaServerKind
  hidden: boolean
  baseUrl: string
  setBaseUrl: (v: string) => void
  onAdded: () => void
}) {
  const toast = useToast()
  const [token, setToken] = useState('')
  const [publicUrl, setPublicUrl] = useState('')
  const [pathMap, setPathMap] = useState<PathMapping[]>([])
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<MediaServerTest | null>(null)
  const [saving, setSaving] = useState(false)
  const brand = MEDIA_SERVER_BRAND[kind]
  const tokenName = kind === 'plex' ? 'Plex token' : 'API key'
  const example = ADDRESS_HINT[kind]
  // The address has its own check so the sign-in buttons can ask for it without demanding a token.
  const va = useValidation({ address: firstError(required(baseUrl, `Add the address of your server, for example ${example}.`), urlCheck(baseUrl, { example })) })
  const v = useValidation({
    token: firstError(required(token, `Add your ${tokenName}. The hint under the box shows where to find it.`), apiKeyCheck(token)),
    publicUrl: urlCheck(publicUrl, { example: 'https://jellyfin.example.com' }),
    ...pathMapErrors(pathMap),
  })
  const input = { kind, baseUrl: baseUrl.trim(), token: token.trim(), publicUrl: publicUrl.trim() || undefined, pathMap: pathMap.filter((m) => m.from && m.to) }

  async function test() {
    const a = va.attempt()
    const b = v.attempt()
    if (!a || !b) return
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
    if (!va.attempt() || !v.attempt()) return
    setSaving(true)
    try {
      await api.createMediaServer({ ...input, name: result?.serverName || brand.label })
      toast.success(`${brand.label} added. New downloads will show up there automatically.`)
      setBaseUrl('')
      setToken('')
      setPublicUrl('')
      setPathMap([])
      setResult(null)
      va.reset()
      v.reset()
      onAdded()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const address = (
    <label>
      Address
      <input
        value={baseUrl}
        onChange={(e) => {
          setBaseUrl(e.target.value)
          setResult(null)
        }}
        placeholder={ADDRESS_HINT[kind]}
        {...va.bind('address', baseUrl, setBaseUrl)}
      />
      <FieldError v={va} name="address" />
      <small className="field-help">How Mediarium reaches the server. Inside Docker, use the server&apos;s IP address or container name, not localhost.</small>
    </label>
  )

  const manual = (
    <>
      {kind === 'plex' && address}
      <label>
        {tokenName}
        <input
          type="password"
          value={token}
          onChange={(e) => {
            setToken(e.target.value)
            setResult(null)
          }}
          autoComplete="off"
          {...v.bind('token', token, setToken)}
        />
        <FieldError v={v} name="token" />
        <small className="field-help">{TOKEN_HELP[kind]}</small>
      </label>
      <details className="how-details">
        <summary>More options</summary>
        <div className="grid-form" style={{ marginTop: 10 }}>
          <label>
            Address you open in your browser (optional)
            <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} placeholder="https://jellyfin.example.com" {...v.bind('publicUrl', publicUrl, setPublicUrl)} />
            <FieldError v={v} name="publicUrl" />
            <small className="field-help">
              Used for the {isBookKind(kind) ? 'links on book pages' : <>&quot;Watch in {brand.label}&quot; buttons</>} when it differs from the address above.
            </small>
          </label>
          <div>
            <strong style={{ fontSize: '0.9rem' }}>Folder mapping (optional)</strong>
            <p style={{ margin: '4px 0 8px', color: 'var(--text-dim)', fontSize: '0.85rem' }}>
              Only needed when {brand.label} sees your library under a different path, for example Mediarium&apos;s <code>/movies</code> is <code>/data/movies</code> in {brand.label}.
            </p>
            <PathMapEditor value={pathMap} onChange={setPathMap} v={v} />
          </div>
        </div>
      </details>
      <div className="form-actions">
        <button onClick={() => void test()} disabled={testing}>
          {testing ? 'Testing…' : 'Test'}
        </button>
        <button className="primary" onClick={() => void add()} disabled={!result?.ok || saving} title={result?.ok ? undefined : 'Test the connection first'}>
          {saving ? 'Adding…' : `Add ${brand.label}`}
        </button>
      </div>
      <FormProblem v={v} verb="test the connection" />
      {result && <TestResult r={result} />}
    </>
  )

  return (
    <div className="method-body" hidden={hidden} role="tabpanel">
      <div className="method-head">
        <MediaServerMark kind={kind} size={46} />
        <div>
          <h3>{brand.label}</h3>
          <p>{KIND_HINT[kind]}</p>
        </div>
      </div>
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
      ) : isBookKind(kind) ? (
        <>
          {address}
          {manual}
        </>
      ) : (
        <>
          {address}
          <JellyfinEmbySignIn kind={kind as 'jellyfin' | 'emby'} baseUrl={baseUrl} checkAddress={() => va.attempt()} onAdded={onAdded} />
          <details className="how-details">
            <summary>Or enter an API key by hand</summary>
            <div className="grid-form" style={{ marginTop: 10 }}>
              {manual}
            </div>
          </details>
        </>
      )}
    </div>
  )
}

type Pick = MediaServerKind | 'find'

// Add a media server: the ways to connect on the left, the form for the one
// you picked on the right.
function AddForm({ onAdded }: { onAdded: () => void }) {
  const [pick, setPick] = useState<Pick>('find')
  const [addr, setAddr] = useState<Record<MediaServerKind, string>>({ plex: '', jellyfin: '', emby: '', audiobookshelf: '', kavita: '' })
  const { on } = useModules()
  const kinds = on('ebooks') || on('audiobooks') ? [...KINDS, ...BOOK_KINDS] : KINDS
  const color = pick === 'find' ? '#f2c14e' : MEDIA_SERVER_BRAND[pick].color

  return (
    <div className="method-add">
      <div className="method-list" role="tablist" aria-label="Ways to connect a media server">
        <button role="tab" aria-selected={pick === 'find'} className={`method-item${pick === 'find' ? ' active' : ''}`} style={{ ['--mc' as string]: '#f2c14e' }} onClick={() => setPick('find')}>
          <span className="method-find-ico">
            <Icon name="search" size={16} />
          </span>
          <span className="mi-text">
            <strong>Find my media servers</strong>
            <small>Look for Plex, Jellyfin and Emby on your home network.</small>
          </span>
        </button>
        {kinds.map((k) => (
          <button key={k} role="tab" aria-selected={pick === k} className={`method-item${pick === k ? ' active' : ''}`} style={{ ['--mc' as string]: MEDIA_SERVER_BRAND[k].color }} onClick={() => setPick(k)}>
            <MediaServerMark kind={k} size={30} />
            <span className="mi-text">
              <strong>{MEDIA_SERVER_BRAND[k].label}</strong>
              <small>{KIND_HINT[k]}</small>
            </span>
            {addr[k] && <span className="mi-dot" title="You have started filling this in" />}
          </button>
        ))}
      </div>
      <div className="method-panel" style={{ ['--mc' as string]: color }}>
        <div className="method-body" hidden={pick !== 'find'} role="tabpanel">
          <div className="method-head">
            <span className="method-find-ico big">
              <Icon name="search" size={22} />
            </span>
            <div>
              <h3>Find my media servers</h3>
              <p>Mediarium looks on your home network for Plex, Jellyfin and Emby. Press Connect on the one you want.</p>
            </div>
          </div>
          <FindServers
            onPick={(f) => {
              setAddr((a) => ({ ...a, [f.kind]: f.address }))
              setPick(f.kind)
            }}
            onManual={() => setPick(KINDS[0])}
          />
        </div>
        {kinds.map((k) => (
          <KindPanel key={k} kind={k} hidden={pick !== k} baseUrl={addr[k]} setBaseUrl={(v) => setAddr((a) => ({ ...a, [k]: v }))} onAdded={onAdded} />
        ))}
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
  const tokenName = s.kind === 'plex' ? 'Plex token' : 'API key'
  const v = useValidation({
    address: firstError(required(baseUrl, `Add the address of your server, for example ${ADDRESS_HINT[s.kind]}.`), urlCheck(baseUrl, { example: ADDRESS_HINT[s.kind] })),
    token: apiKeyCheck(token),
    publicUrl: urlCheck(publicUrl, { example: 'https://jellyfin.example.com' }),
    ...pathMapErrors(pathMap, 'edit'),
  })

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
    <div className={`indexer-card ms-card${s.enabled ? '' : ' is-off'}${editing ? ' editing' : ''}`} style={{ ['--ms' as string]: MEDIA_SERVER_BRAND[s.kind].color }}>
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
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} {...v.bind('address', baseUrl, setBaseUrl)} />
            <FieldError v={v} name="address" />
          </label>
          <label>
            {tokenName}
            <input value={token} onChange={(e) => setToken(e.target.value)} placeholder="Leave empty to keep the saved one" autoComplete="off" {...v.bind('token', token, setToken)} />
            <FieldError v={v} name="token" />
          </label>
          <label>
            Address you open in your browser (optional)
            <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} {...v.bind('publicUrl', publicUrl, setPublicUrl)} />
            <FieldError v={v} name="publicUrl" />
          </label>
          <div>
            <strong style={{ fontSize: '0.9rem' }}>Folder mapping</strong>
            <PathMapEditor value={pathMap} onChange={setPathMap} v={v} prefix="edit" />
          </div>
          <div>
            <button
              className="primary btn-sm"
              disabled={!!busy}
              onClick={() => {
                if (!v.attempt()) return
                void update({ baseUrl: baseUrl.trim(), token: token.trim() || undefined, publicUrl: publicUrl.trim(), pathMap: pathMap.filter((m) => m.from && m.to) }, `${s.name} saved.`).then(() => {
                  setEditing(false)
                  setToken('')
                  v.reset()
                })
              }}
            >
              Save changes
            </button>
            <FormProblem v={v} />
          </div>
        </div>
      )}
    </div>
  )
}

// Settings > Connections > Media servers: Plex, Jellyfin and Emby. After each
// download the right library is refreshed, and title pages get "Watch in ..."
// buttons. Your servers are listed on top, and adding one is below.
export default function MediaServerSettings() {
  const [list, setList] = useState<MediaServer[] | null>(null)
  const reload = useCallback(() => {
    api.listMediaServers().then(setList).catch(() => setList((cur) => cur ?? []))
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
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="monitor" size={14} /> Your media servers {list && <small>({list.length})</small>}
        </legend>
        {list === null && <div className="skeleton" style={{ height: 120 }} />}
        {list && list.length === 0 && (
          <div className="method-empty">
            <Icon name="monitor" size={28} />
            <div>
              <strong>No media server connected yet.</strong>
              <p>Pick a server below, or let Mediarium find Plex, Jellyfin and Emby on your network. Then test the connection and add it.</p>
            </div>
          </div>
        )}
        {list && list.length > 0 && (
          <div className="ms-cards">
            {list.map((s) => (
              <ServerCard key={s.id} s={s} onChanged={reload} />
            ))}
          </div>
        )}
      </fieldset>
      <fieldset className="group alerts span-all">
        <legend>
          <Icon name="plus" size={14} /> Add a media server
        </legend>
        <AddForm onAdded={reload} />
      </fieldset>
      {list && list.some((s) => s.kind === 'plex' || s.kind === 'jellyfin' || s.kind === 'emby') && <WatchedSettings />}
    </div>
  )
}
