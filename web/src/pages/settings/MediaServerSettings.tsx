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
import { plural, t, tx, st } from '../../i18n'
import TipLabel from '../../components/TipLabel'

const KINDS: MediaServerKind[] = ['plex', 'jellyfin', 'emby', 'silo', 'webhook']
// The apps for reading and listening, offered while ebooks or audiobooks are on.
const BOOK_KINDS: MediaServerKind[] = ['audiobookshelf', 'kavita']
const isBookKind = (k: MediaServerKind) => BOOK_KINDS.includes(k)

const TOKEN_HELP: Record<MediaServerKind, string> = {
  plex: t('Your Plex token. In Plex Web, open any movie, choose ⋯ → Get Info → View XML; the token is the X-Plex-Token part of the address that opens.'),
  jellyfin: t('An API key. In Jellyfin: Dashboard → API Keys → + (name it Mediarium).'),
  emby: t('An API key. In Emby: Settings → Advanced → API Keys → New API Key (name it Mediarium).'),
  silo: t('An API key of an administrator. In Silo: Admin → API keys → create one (name it Mediarium). It starts with sa_.'),
  audiobookshelf: t('An API token. In Audiobookshelf: Settings → API Keys → Add API Key (older versions: Settings → Users → your user → API Token).'),
  kavita: t('Your API key. In Kavita: open your user settings (your name, top right) → 3rd Party Clients, and copy the API key.'),
  webhook: t('Optional. Sent with every message as "Authorization: Bearer …", for an address that wants a key.'),
}

const ADDRESS_HINT: Record<MediaServerKind, string> = {
  plex: 'http://192.168.1.10:32400',
  jellyfin: 'http://192.168.1.10:8096',
  emby: 'http://192.168.1.10:8096',
  silo: 'http://192.168.1.10:8090',
  audiobookshelf: 'http://192.168.1.10:13378',
  kavita: 'http://192.168.1.10:5000',
  webhook: 'http://192.168.1.10:8080/hooks/mediarium',
}

function TestResult({ r }: { r: MediaServerTest }) {
  if (!r.ok) {
    return (
      <div className="indexer-result bad">
        <Icon name="warning" size={14} /> {r.error ?? t('The test failed.')}
      </div>
    )
  }
  return (
    <div className="indexer-result ok">
      <Icon name="check" size={14} />
      <span>
        {r.version
          ? tx('Connected to {name} (version {version}).', { name: <strong>{r.serverName ?? t('the server')}</strong>, version: r.version })
          : tx('Connected to {name}.', { name: <strong>{r.serverName ?? t('the server')}</strong> })}
        {r.libraries && r.libraries.length > 0 && <> {t('Libraries: {list}.', { list: r.libraries.map((l) => l.title).join(', ') })}</>}
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
    out[`${prefix}${i}.from`] = firstError(!from && to ? required(from, t('Enter the folder as Mediarium sees it, for example /movies.')) : null, folderPath(from, '/movies'))
    out[`${prefix}${i}.to`] = firstError(from && !to ? required(to, t('Enter the same folder as the media server sees it, for example /data/movies.')) : null, folderPath(to, '/data/movies'))
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
              aria-label={t('Folder in Mediarium')}
              {...v.bind(`${prefix}${i}.from`, m.from, (val) => onChange(value.map((x, j) => (j === i ? { ...x, from: val } : x))))}
            />
            <FieldError v={v} name={`${prefix}${i}.from`} />
          </div>
          <Icon name="open" size={14} />
          <div>
            <input
              value={m.to}
              onChange={(e) => onChange(value.map((x, j) => (j === i ? { ...x, to: e.target.value } : x)))}
              placeholder="/data/movies"
              aria-label={t('Same folder on the media server')}
              {...v.bind(`${prefix}${i}.to`, m.to, (val) => onChange(value.map((x, j) => (j === i ? { ...x, to: val } : x))))}
            />
            <FieldError v={v} name={`${prefix}${i}.to`} />
          </div>
          <button className="icon-btn" onClick={() => onChange(value.filter((_, j) => j !== i))} aria-label={t('Remove this mapping')}>
            <Icon name="x" size={14} />
          </button>
        </div>
      ))}
      <button className="btn-sm btn-with-icon" onClick={() => onChange([...value, { from: '', to: '' }])}>
        <Icon name="plus" size={14} /> {t('Add a folder mapping')}
      </button>
    </div>
  )
}

const KIND_HINT: Record<MediaServerKind, string> = {
  plex: t('Sign in with your Plex account. No token to find or copy.'),
  jellyfin: t('Quick Connect, or an administrator login. No key to copy.'),
  emby: t('An administrator login, or an API key.'),
  silo: t('Refreshes Silo after every download, with "Watch in Silo" buttons on title pages. Needs an API key.'),
  audiobookshelf: t('Listen to your audiobooks, with apps for phones. Needs an API token.'),
  kavita: t('Read your ebooks and comics in the browser. Needs an API key.'),
  webhook: t('Any other server or tool: a JSON message naming the new folders after each download.'),
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
  const tokenName = kind === 'plex' ? t('Plex token') : kind === 'webhook' ? t('Token (optional)') : t('API key')
  const example = ADDRESS_HINT[kind]
  // The address has its own check so the sign-in buttons can ask for it without demanding a token.
  const va = useValidation({ address: firstError(required(baseUrl, t('Add the address of your server, for example {example}.', { example })), urlCheck(baseUrl, { example })) })
  const v = useValidation({
    token: firstError(kind === 'webhook' ? null : required(token, t('Add your {name}. The hint under the box shows where to find it.', { name: tokenName })), apiKeyCheck(token)),
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
      toast.success(t('{name} added. New downloads will show up there automatically.', { name: brand.label }))
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
      {kind === 'webhook' ? t('Webhook address') : t('Address')}
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
      <small className="field-help">
        {kind === 'webhook'
          ? t('Mediarium POSTs a JSON message here after each download. Inside Docker, use an IP address or container name, not localhost.')
          : kind === 'silo'
          ? t("The address you open Silo at in your browser (port 8090 unless you changed it), not its Jellyfin-compatible port. Inside Docker, use the server's IP address or container name, not localhost.")
          : t("How Mediarium reaches the server. Inside Docker, use the server's IP address or container name, not localhost.")}
      </small>
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
        <summary>{t('More options')}</summary>
        <div className="grid-form" style={{ marginTop: 10 }}>
          {kind !== 'webhook' && <label>
            {t('Address you open in your browser (optional)')}
            <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} placeholder="https://jellyfin.example.com" {...v.bind('publicUrl', publicUrl, setPublicUrl)} />
            <FieldError v={v} name="publicUrl" />
            <small className="field-help">
              {isBookKind(kind)
                ? t('Used for the links on book pages when it differs from the address above.')
                : t('Used for the "Watch in {name}" buttons when it differs from the address above.', { name: brand.label })}
            </small>
          </label>}
          <div>
            <strong style={{ fontSize: '0.9rem' }}><TipLabel text={t('Needed only when the media server sees your library under another path. Each row pairs the folder as Mediarium names it with the same folder on the server.')} learnMore="media-servers#path-mapping">{t('Folder mapping (optional)')}</TipLabel></strong>
            <p style={{ margin: '4px 0 8px', color: 'var(--text-dim)', fontSize: '0.85rem' }}>
              {tx("Only needed when {name} sees your library under a different path, for example Mediarium's {from} is {to} in {name}.", {
                name: brand.label,
                from: <code>/movies</code>,
                to: <code>/data/movies</code>,
              })}
            </p>
            <PathMapEditor value={pathMap} onChange={setPathMap} v={v} />
          </div>
        </div>
      </details>
      <div className="form-actions">
        <button onClick={() => void test()} disabled={testing}>
          {testing ? t('Testing…') : t('Test')}
        </button>
        <button className="primary" onClick={() => void add()} disabled={!result?.ok || saving} title={result?.ok ? undefined : t('Test the connection first')}>
          {saving ? t('Adding…') : t('Add {name}', { name: brand.label })}
        </button>
      </div>
      <FormProblem v={v} action="test" />
      {result && <TestResult r={result} />}
    </>
  )

  return (
    <div className="method-body" hidden={hidden} role="tabpanel">
      <div className="method-head">
        <MediaServerMark kind={kind} size={46} />
        <div>
          <h2>{brand.label}</h2>
          <p>{KIND_HINT[kind]}</p>
        </div>
      </div>
      {kind === 'plex' ? (
        <>
          <PlexSignIn onAdded={onAdded} />
          <details className="how-details">
            <summary>{t('Or enter the address and token by hand')}</summary>
            <div className="grid-form" style={{ marginTop: 10 }}>
              {manual}
            </div>
          </details>
        </>
      ) : isBookKind(kind) || kind === 'webhook' || kind === 'silo' ? (
        <>
          {address}
          {manual}
        </>
      ) : (
        <>
          {address}
          <JellyfinEmbySignIn kind={kind as 'jellyfin' | 'emby'} baseUrl={baseUrl} checkAddress={() => va.attempt()} onAdded={onAdded} />
          <details className="how-details">
            <summary>{t('Or enter an API key by hand')}</summary>
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
  const [addr, setAddr] = useState<Record<MediaServerKind, string>>({ plex: '', jellyfin: '', emby: '', silo: '', audiobookshelf: '', kavita: '', webhook: '' })
  const { on } = useModules()
  const kinds = on('ebooks') || on('audiobooks') ? [...KINDS, ...BOOK_KINDS] : KINDS
  const color = pick === 'find' ? '#f2c14e' : MEDIA_SERVER_BRAND[pick].color

  return (
    <div className="method-add">
      <div className="method-list" role="tablist" aria-label={t('Ways to connect a media server')}>
        <button role="tab" aria-selected={pick === 'find'} className={`method-item${pick === 'find' ? ' active' : ''}`} style={{ ['--mc' as string]: '#f2c14e' }} onClick={() => setPick('find')}>
          <span className="method-find-ico">
            <Icon name="search" size={16} />
          </span>
          <span className="mi-text">
            <strong>{t('Find my media servers')}</strong>
            <small>{t('Look for Plex, Jellyfin and Emby on your home network.')}</small>
          </span>
        </button>
        {kinds.map((k) => (
          <button key={k} role="tab" aria-selected={pick === k} className={`method-item${pick === k ? ' active' : ''}`} style={{ ['--mc' as string]: MEDIA_SERVER_BRAND[k].color }} onClick={() => setPick(k)}>
            <MediaServerMark kind={k} size={30} />
            <span className="mi-text">
              <strong>{MEDIA_SERVER_BRAND[k].label}</strong>
              <small>{KIND_HINT[k]}</small>
            </span>
            {addr[k] && <span className="mi-dot" title={t('You have started filling this in')} />}
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
              <h2>{t('Find my media servers')}</h2>
              <p>{t('Mediarium looks on your home network for Plex, Jellyfin and Emby. Press Connect on the one you want.')}</p>
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
  const tokenName = s.kind === 'plex' ? t('Plex token') : t('API key')
  const v = useValidation({
    address: firstError(required(baseUrl, t('Add the address of your server, for example {example}.', { example: ADDRESS_HINT[s.kind] })), urlCheck(baseUrl, { example: ADDRESS_HINT[s.kind] })),
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
            {!s.enabled && <span className="badge">{t('Off')}</span>}
          </div>
          <small className="indexer-url">{s.baseUrl}</small>
          {s.secretError ? (
            <small className="error-text">{st(s.secretError)}</small>
          ) : s.lastError ? (
            <small className="error-text">{t('Last check failed: {error}', { error: s.lastError })}</small>
          ) : (
            s.lastCheckedAt && <small style={{ color: 'var(--text-dim)' }}>{t('Last checked {when}', { when: timeAgo(s.lastCheckedAt) })}</small>
          )}
        </div>
      </div>
      <Switch
        checked={s.refreshAfterImport}
        onChange={(v) => void update({ refreshAfterImport: v }, v ? t('{name} is refreshed after every download.', { name: s.name }) : t('{name} is no longer refreshed automatically.', { name: s.name }))}
        label={t('Refresh after every download')}
        tip={t('Tells the server about the new folder as soon as a download is imported, so you do not wait for its own scheduled scan.')}
        learnMore="media-servers#what-happens-after-an-import"
        description={
          s.kind === 'webhook'
            ? t('Gets a message naming the new folder after every download.')
            : t('{name} scans just the new folder, so new movies and episodes appear there within a minute.', { name: MEDIA_SERVER_BRAND[s.kind].label })
        }
      />
      <div className="indexer-actions">
        <button className="btn-sm" disabled={!!busy} onClick={() => void run('test', async () => setResult(await api.testMediaServer(s.id)))}>
          {busy === 'test' ? t('Testing…') : t('Test')}
        </button>
        <button
          className="btn-sm"
          disabled={!!busy}
          onClick={() =>
            void run('refresh', async () => {
              const r = await api.refreshMediaServer(s.id)
              if (r.ok) toast.success(t('{name} is scanning your libraries.', { name: s.name }))
              else toast.error(r.error ?? t('The refresh failed.'))
            })
          }
        >
          {busy === 'refresh' ? t('Refreshing…') : t('Refresh now')}
        </button>
        <a className="btn btn-sm btn-with-icon" href={s.webUrl || s.publicUrl || s.baseUrl} target="_blank" rel="noreferrer">
          <Icon name="external" size={14} /> {t('Open')}
        </a>
        <button className="btn-sm" onClick={() => setEditing((v) => !v)} aria-expanded={editing}>
          {editing ? t('Cancel') : t('Edit')}
        </button>
        <button className="btn-sm" disabled={!!busy} onClick={() => void update({ enabled: !s.enabled }, s.enabled ? t('{name} turned off.', { name: s.name }) : t('{name} turned on.', { name: s.name }))}>
          {s.enabled ? t('Disable') : t('Enable')}
        </button>
        <button
          className="btn-sm btn-danger"
          onClick={async () => {
            if (!(await confirm({ title: t('Remove {name}?', { name: s.name }), body: <p>{t('Mediarium stops telling it about new downloads. Nothing on the media server changes.')}</p>, confirmLabel: t('Remove'), danger: true }))) return
            await run('remove', async () => {
              await api.deleteMediaServer(s.id)
              toast.success(t('{name} removed.', { name: s.name }))
              onChanged()
            })
          }}
        >
          {t('Remove')}
        </button>
      </div>
      {result && <TestResult r={result} />}
      {editing && (
        <div className="indexer-edit grid-form">
          <label>
            {t('Address')}
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} {...v.bind('address', baseUrl, setBaseUrl)} />
            <FieldError v={v} name="address" />
          </label>
          <label>
            {tokenName}
            <input value={token} onChange={(e) => setToken(e.target.value)} placeholder={t('Leave empty to keep the saved one')} autoComplete="off" {...v.bind('token', token, setToken)} />
            <FieldError v={v} name="token" />
          </label>
          <label>
            <TipLabel text={t('Used for the Watch in buttons when it differs from the address Mediarium uses to reach the server.')}>{t('Address you open in your browser (optional)')}</TipLabel>
            <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} {...v.bind('publicUrl', publicUrl, setPublicUrl)} />
            <FieldError v={v} name="publicUrl" />
          </label>
          <div>
            <strong style={{ fontSize: '0.9rem' }}><TipLabel text={t('Needed only when the media server sees your library under another path. Each row pairs the folder as Mediarium names it with the same folder on the server.')} learnMore="media-servers#path-mapping">{t('Folder mapping')}</TipLabel></strong>
            <PathMapEditor value={pathMap} onChange={setPathMap} v={v} prefix="edit" />
          </div>
          <div>
            <button
              className="primary btn-sm"
              disabled={!!busy}
              onClick={() => {
                if (!v.attempt()) return
                void update({ baseUrl: baseUrl.trim(), token: token.trim() || undefined, publicUrl: publicUrl.trim(), pathMap: pathMap.filter((m) => m.from && m.to) }, t('{name} saved.', { name: s.name })).then(() => {
                  setEditing(false)
                  setToken('')
                  v.reset()
                })
              }}
            >
              {t('Save changes')}
            </button>
            <FormProblem v={v} />
          </div>
        </div>
      )}
    </div>
  )
}

// Settings > Media servers and apps > Media servers: Plex, Jellyfin and Emby. After each
// download the right library is refreshed, and title pages get "Watch in ..."
// buttons. Your servers are listed on top, and adding one is below.
// Pause downloads while someone is watching on Plex, Jellyfin or Emby.
function StreamPauseSetting() {
  const toast = useToast()
  const [state, setState] = useState<{ enabled: boolean; watching: number; servers: number; held: number } | null>(null)
  const load = useCallback(() => {
    api.getStreamPause().then(setState).catch(() => undefined)
  }, [])
  useEffect(load, [load])
  useLive(load, 15000)
  if (!state) return null
  return (
    <fieldset className="group span-all">
      <legend>
        <Icon name="pause" size={14} /> {t('While someone is watching')}
      </legend>
      <Switch
        checked={state.enabled}
        onChange={(v) =>
          api
            .putStreamPause(v)
            .then((r) => {
              setState(r)
              toast.success(v ? t('Downloads will pause while someone is watching.') : t('Downloads no longer pause for streams.'))
            })
            .catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
        }
        label={t('Pause downloads while someone is watching')}
        tip={t('Every half minute Mediarium asks your Plex, Jellyfin and Emby servers if anyone is watching. While someone is, running and waiting downloads are paused. A minute after the last person stops, they carry on. Only the downloads Mediarium paused itself are put back.')}
        description={t('Useful when your internet is slow, so a download does not make the picture stutter.')}
      />
      {state.enabled && (
        <small style={{ color: 'var(--text-dim)' }}>
          {state.servers === 0
            ? t('No media server has answered yet.')
            : state.watching > 0
              ? plural(state.watching, '{n} person is watching now.', '{n} people are watching now.') + (state.held > 0 ? ' ' + plural(state.held, '{n} download is paused for it.', '{n} downloads are paused for it.') : '')
              : t('Nobody is watching right now.')}
        </small>
      )}
    </fieldset>
  )
}

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
        {tx('Connect the app you watch with. After every download Mediarium tells it to look at the new folder, so movies and episodes appear there within a minute, and title pages here get a {button} (or Jellyfin, Emby) button.', {
          button: <strong>{t('Watch in {name}', { name: 'Plex' })}</strong>,
        })}{' '}
        <a href={`${DOCS_URL}/media-servers.md`} target="_blank" rel="noreferrer">
          {t('How to set it up')}
        </a>
      </p>
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="monitor" size={14} /> {t('Your media servers')} {list && <small>({list.length})</small>}
        </legend>
        {list === null && <div className="skeleton" style={{ height: 120 }} />}
        {list && list.length === 0 && (
          <div className="method-empty">
            <Icon name="monitor" size={28} />
            <div>
              <strong>{t('No media server connected yet.')}</strong>
              <p>{t('Pick a server below, or let Mediarium find Plex, Jellyfin and Emby on your network. Then test the connection and add it.')}</p>
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
          <Icon name="plus" size={14} /> {t('Add a media server')}
        </legend>
        <AddForm onAdded={reload} />
      </fieldset>
      {list && list.some((s) => s.kind === 'plex' || s.kind === 'jellyfin' || s.kind === 'emby') && <StreamPauseSetting />}
      {list && list.some((s) => s.kind === 'plex' || s.kind === 'jellyfin' || s.kind === 'emby') && <WatchedSettings />}
    </div>
  )
}
