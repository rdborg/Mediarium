import { useEffect, useRef, useState } from 'react'
import { api, type FoundMediaServer, type MediaServerKind, type PlexAccountServer } from '../api'
import Icon from './Icon'
import { MEDIA_SERVER_BRAND, MediaServerMark } from './mediaServerBrand'
import { useToast } from './Toast'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Search the home network for Plex, Jellyfin and Emby. Inside Docker the
// search cannot see your network's range by itself, so a range can be typed.
export function FindServers({ onPick }: { onPick: (s: FoundMediaServer) => void }) {
  const [subnet, setSubnet] = useState('')
  const [busy, setBusy] = useState(false)
  const [found, setFound] = useState<FoundMediaServer[] | null>(null)
  const [note, setNote] = useState('')
  const [error, setError] = useState('')

  async function search() {
    setBusy(true)
    setError('')
    setNote('')
    try {
      const nets = subnet
        .split(/[\s,]+/)
        .map((s) => s.trim())
        .filter(Boolean)
        .map((s) => (/^\d+\.\d+\.\d+$/.test(s) ? `${s}.0/24` : s))
      const r = await api.discoverMediaServers(nets)
      setFound(r.found)
      setNote(r.note ?? (r.found.length === 0 ? `Nothing answered on ${r.scanned.join(', ')}. If your server is on another network range, type it above (for example 192.168.1 or 10.0.0).` : ''))
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="ms-find">
      <div className="ms-find-row">
        <button className="btn-with-icon" onClick={() => void search()} disabled={busy}>
          <Icon name="search" size={15} /> {busy ? 'Searching your network…' : 'Find my media servers'}
        </button>
        <input value={subnet} onChange={(e) => setSubnet(e.target.value)} placeholder="Your network, optional (e.g. 192.168.1)" aria-label="Your home network" />
      </div>
      {busy && <small className="ms-find-hint">This takes up to about 10 seconds.</small>}
      {error && <p className="error-text" style={{ margin: 0 }}>{error}</p>}
      {found && found.length > 0 && (
        <ul className="ms-found">
          {found.map((f) => (
            <li key={`${f.kind}-${f.address}`}>
              <MediaServerMark kind={f.kind} size={26} />
              <span className="ms-found-name">
                <strong>{f.name || MEDIA_SERVER_BRAND[f.kind].label}</strong>
                <small>
                  {MEDIA_SERVER_BRAND[f.kind].label}
                  {f.version ? ` ${f.version}` : ''} · {f.address}
                </small>
              </span>
              {f.alreadyAdded ? (
                <span className="badge downloaded">Added</span>
              ) : (
                <button className="btn-sm primary" onClick={() => onPick(f)}>
                  Connect
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {note && <small className="ms-find-hint">{note}</small>}
    </div>
  )
}

// Sign in with Plex: plex.tv confirms who you are and hands Mediarium a key,
// then lists your Plex servers to pick from. No token to copy.
export function PlexSignIn({ onAdded }: { onAdded: () => void }) {
  const toast = useToast()
  const [pin, setPin] = useState<{ pinId: number; code: string; authUrl: string } | null>(null)
  const [servers, setServers] = useState<PlexAccountServer[] | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState('')
  const timer = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  useEffect(() => () => clearInterval(timer.current), [])

  async function start() {
    setError('')
    setServers(null)
    try {
      const p = await api.plexPin()
      setPin(p)
      window.open(p.authUrl, 'plex-sign-in', 'width=620,height=760')
      clearInterval(timer.current)
      timer.current = setInterval(async () => {
        try {
          const st = await api.plexPinStatus(p.pinId)
          if (st.done) {
            clearInterval(timer.current)
            setServers(st.servers)
          } else if (st.expired) {
            clearInterval(timer.current)
            setPin(null)
            setError(st.error ?? 'The sign-in expired. Try again.')
          }
        } catch (e) {
          clearInterval(timer.current)
          setPin(null)
          setError(errText(e))
        }
      }, 2000)
    } catch (e) {
      setError(errText(e))
    }
  }

  async function add(s: PlexAccountServer) {
    if (!pin) return
    setAdding(s.machineIdentifier)
    try {
      await api.plexPinAdd(pin.pinId, s.machineIdentifier)
      toast.success(`${s.name} added. New downloads now show up in Plex by themselves.`)
      onAdded()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setAdding('')
    }
  }

  if (servers) {
    return (
      <div className="ms-signin">
        <p className="ms-signin-ok">
          <Icon name="check" size={15} /> Signed in to Plex. Pick the server that holds your library:
        </p>
        {servers.length === 0 && <p style={{ margin: 0 }}>This Plex account has no servers.</p>}
        <ul className="ms-found">
          {servers.map((s) => (
            <li key={s.machineIdentifier}>
              <MediaServerMark kind="plex" size={26} />
              <span className="ms-found-name">
                <strong>{s.name}</strong>
                <small>
                  {s.owned ? 'Your server' : 'Shared with you'}
                  {s.version ? ` · ${s.version}` : ''} · {s.connections.find((c) => c.local)?.uri ?? s.connections[0]?.uri ?? ''}
                </small>
              </span>
              {s.alreadyAdded ? (
                <span className="badge downloaded">Added</span>
              ) : (
                <button className="btn-sm primary" onClick={() => void add(s)} disabled={!!adding}>
                  {adding === s.machineIdentifier ? 'Adding…' : 'Add'}
                </button>
              )}
            </li>
          ))}
        </ul>
      </div>
    )
  }

  return (
    <div className="ms-signin">
      <button className="ms-plex-btn" onClick={() => void start()} disabled={!!pin}>
        <MediaServerMark kind="plex" size={22} /> {pin ? 'Waiting for you to approve in the Plex window…' : 'Sign in with Plex'}
      </button>
      {pin && (
        <small className="ms-find-hint">
          A plex.tv window opened. Sign in there and approve Mediarium. No window?{' '}
          <a href={pin.authUrl} target="_blank" rel="noreferrer">
            Open it here
          </a>{' '}
          (code {pin.code}).
        </small>
      )}
      {!pin && <small className="ms-find-hint">The easiest way: no token to find or copy. Mediarium then lists your Plex servers.</small>}
      {error && <p className="error-text" style={{ margin: 0 }}>{error}</p>}
    </div>
  )
}

// Jellyfin Quick Connect (approve a code in Jellyfin) or, for Jellyfin and
// Emby, signing in once with an administrator's username and password. The
// password is only used to get a key; it is never stored.
export function JellyfinEmbySignIn({ kind, baseUrl, onAdded }: { kind: Exclude<MediaServerKind, 'plex'>; baseUrl: string; onAdded: () => void }) {
  const toast = useToast()
  const [qc, setQc] = useState<{ id: string; code: string } | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const timer = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const label = MEDIA_SERVER_BRAND[kind].label

  useEffect(() => () => clearInterval(timer.current), [])
  useEffect(() => {
    clearInterval(timer.current)
    setQc(null)
    setError('')
  }, [baseUrl, kind])

  async function quickConnect() {
    setError('')
    try {
      const r = await api.jellyfinQuickConnect(baseUrl.trim())
      setQc({ id: r.id, code: r.code })
      clearInterval(timer.current)
      timer.current = setInterval(async () => {
        try {
          const st = await api.jellyfinQuickConnectStatus(r.id)
          if (st.done) {
            clearInterval(timer.current)
            setQc(null)
            toast.success(`${st.server.name} added. New downloads now show up in Jellyfin by themselves.`)
            onAdded()
          } else if (st.expired) {
            clearInterval(timer.current)
            setQc(null)
            setError(st.error ?? 'The code expired. Try again.')
          }
        } catch (e) {
          clearInterval(timer.current)
          setQc(null)
          setError(errText(e))
        }
      }, 2000)
    } catch (e) {
      setError(errText(e))
    }
  }

  async function login() {
    setBusy(true)
    setError('')
    try {
      const s = await api.mediaServerLogin({ kind, baseUrl: baseUrl.trim(), username: username.trim(), password })
      setPassword('')
      toast.success(`${s.name} added. New downloads now show up in ${label} by themselves.`)
      onAdded()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const noAddress = !baseUrl.trim()
  return (
    <div className="ms-signin">
      {kind === 'jellyfin' && (
        <>
          <button className="primary btn-with-icon" onClick={() => void quickConnect()} disabled={noAddress || !!qc}>
            <Icon name="key" size={15} /> {qc ? 'Waiting for approval…' : 'Use Quick Connect'}
          </button>
          {qc ? (
            <div className="ms-qc">
              <span>In Jellyfin, open your profile → <strong>Quick Connect</strong> and enter:</span>
              <strong className="ms-qc-code">{qc.code}</strong>
            </div>
          ) : (
            <small className="ms-find-hint">{noAddress ? 'Enter the address above first.' : 'Jellyfin shows a code to approve; no key to copy.'}</small>
          )}
          <div className="ms-or">or sign in with an administrator account</div>
        </>
      )}
      <div className="form-cols">
        <label>
          {label} username
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
        </label>
      </div>
      <div>
        <button className={kind === 'emby' ? 'primary' : ''} onClick={() => void login()} disabled={busy || noAddress || !username.trim()}>
          {busy ? 'Signing in…' : `Sign in to ${label}`}
        </button>
        <small className="ms-find-hint" style={{ display: 'block', marginTop: 6 }}>
          Used once to get a key for Mediarium; the password is not stored.
        </small>
      </div>
      {error && <p className="error-text" style={{ margin: 0 }}>{error}</p>}
    </div>
  )
}
