import { useEffect, useState } from 'react'
import { api, type ProxySignIn as State } from '../api'
import { DOCS_URL } from '../docs'
import { t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'
import TipLabel from './TipLabel'

// Settings > People and access: sign-in through a reverse proxy that does the login
// itself (Authelia, Authentik, Cloudflare Access...). Off until a header name
// is saved. Only for administrators.
export default function ProxySignIn() {
  const toast = useToast()
  const [state, setState] = useState<State | null>(null)
  const [header, setHeader] = useState('')
  const [addresses, setAddresses] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getProxySignIn()
      .then((s) => {
        setState(s)
        setHeader(s.header)
        setAddresses(s.addresses || s.from)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  async function save(next: string, from: string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putProxySignIn(next.trim(), from.trim())
      setState(s)
      setHeader(s.header)
      setAddresses(s.addresses || s.from)
      toast.success(s.header ? t('Sign-in through your proxy is on, using {header}.', { header: s.header }) : t('Sign-in through your proxy is off.'))
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
        <Icon name="shield" size={14} /> {t('Sign in through your reverse proxy')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t("Only for a proxy that asks for the login itself, such as Authelia, Authentik or Cloudflare Access. It can pass the signed-in username on in a header, and Mediarium signs that account in without its own sign-in page. A plain reverse proxy (like the one on a Synology) doesn't need this: leave it off. Mediarium only believes the header when it comes straight from your proxy's address, and the name has to match an account here.")}{' '}
        <a href={`${DOCS_URL}/security.md#sign-in-through-your-reverse-proxy`} target="_blank" rel="noreferrer">
          {t('How to set it up')}
        </a>
      </p>
      <div className="row" style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'end' }}>
        <label style={{ display: 'grid', gap: 4, flex: '1 1 220px', maxWidth: 320 }}>
          <TipLabel text={t('The name of the header your proxy adds with the signed-in username, such as Remote-User. The name must match an account here.')}>{t('Header with the username')}</TipLabel>
          <input value={header} placeholder="Remote-User" onChange={(e) => setHeader(e.target.value)} disabled={busy || state === null} />
        </label>
        <label style={{ display: 'grid', gap: 4, flex: '1 1 220px', maxWidth: 320 }}>
          <TipLabel text={t('Mediarium only believes the header when the request comes straight from this address, so nobody else can fake it.')} learnMore="internet#sign-in-through-your-reverse-proxy">{t("Your proxy's address")}</TipLabel>
          <input value={addresses} placeholder="172.18.0.5" onChange={(e) => setAddresses(e.target.value)} disabled={busy || state === null} />
        </label>
        <button
          className="primary"
          onClick={() => void save(header, addresses)}
          disabled={busy || state === null || (header.trim() === (state?.header ?? '') && addresses.trim() === (state?.addresses ?? ''))}
        >
          {on ? t('Save') : t('Turn on')}
        </button>
        {on && (
          <button onClick={() => void save('', '')} disabled={busy}>
            {t('Turn off')}
          </button>
        )}
      </div>
      {state && (
        <p className="hint" style={{ marginBottom: 0 }}>
          {!state.viaTrustedProxy
            ? on
              ? t("This page came from {from}, which isn't the proxy address above, so the header is ignored for you right now. Open Mediarium through your proxy to see its address here.", { from: state.from })
              : t('This page came from {from}. Open Mediarium through your proxy to see its address here.', { from: state.from })
            : on
              ? state.seen
                ? t('Your proxy sent {header}: {value}.', { header: state.header, value: state.seen })
                : t('Your proxy did not send {header} with this page.', { header: state.header })
              : t('Off. Everyone signs in with their Mediarium password.')}
        </p>
      )}
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}
