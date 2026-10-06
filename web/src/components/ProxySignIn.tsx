import { useEffect, useState } from 'react'
import { api, type ProxySignIn as State } from '../api'
import { DOCS_URL } from '../docs'
import Icon from './Icon'
import { useToast } from './Toast'

// Settings > Accounts: sign-in through a reverse proxy that does the login
// itself (Authelia, Authentik, Cloudflare Access...). Off until a header name
// is saved. Only for administrators.
export default function ProxySignIn() {
  const toast = useToast()
  const [state, setState] = useState<State | null>(null)
  const [header, setHeader] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getProxySignIn()
      .then((s) => {
        setState(s)
        setHeader(s.header)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  async function save(next: string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putProxySignIn(next.trim())
      setState(s)
      setHeader(s.header)
      toast.success(s.header ? `Sign-in through your proxy is on, using ${s.header}.` : 'Sign-in through your proxy is off.')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const on = !!state?.header
  return (
    <fieldset className="group">
      <legend>
        <Icon name="shield" size={14} /> Sign in through your reverse proxy
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        Only for a proxy that asks for the login itself, such as Authelia, Authentik or Cloudflare Access. It can pass the signed-in user name on in a
        header, and Mediarium signs that account in without its own sign-in page. A plain reverse proxy (like the one on a Synology) doesn&apos;t need
        this: leave it off. Mediarium only believes the header from a proxy listed in TRUSTED_PROXIES, and the name has to match an account here.{' '}
        <a href={`${DOCS_URL}/security.md#sign-in-through-your-reverse-proxy`} target="_blank" rel="noreferrer">
          How to set it up
        </a>
      </p>
      <div className="row" style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'end' }}>
        <label style={{ display: 'grid', gap: 4, flex: '1 1 220px', maxWidth: 320 }}>
          <span>Header with the user name</span>
          <input value={header} placeholder="Remote-User" onChange={(e) => setHeader(e.target.value)} disabled={busy || state === null} />
        </label>
        <button className="primary" onClick={() => void save(header)} disabled={busy || state === null || header.trim() === (state?.header ?? '')}>
          {on ? 'Save' : 'Turn on'}
        </button>
        {on && (
          <button onClick={() => void save('')} disabled={busy}>
            Turn off
          </button>
        )}
      </div>
      {state && (
        <p className="hint" style={{ marginBottom: 0 }}>
          {!state.viaTrustedProxy
            ? 'This page did not come through a trusted proxy, so the header would be ignored for you right now.'
            : on
              ? state.seen
                ? `Your proxy sent ${state.header}: ${state.seen}.`
                : `Your proxy did not send ${state.header} with this page.`
              : 'Off. Everyone signs in with their Mediarium password.'}
        </p>
      )}
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}
