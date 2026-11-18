import { useEffect, useState } from 'react'
import { api, type SignInMethods } from '../api'
import { useAuth } from '../AuthContext'
import { t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'

// Settings > People and access > Your profile: tie this account to the single sign-on provider, so
// "Sign in with <name>" lands here. Only for an account with a password here;
// accounts made through single sign-on or the directory have no other way in.
export default function SsoLinkCard() {
  const { user, refresh } = useAuth()
  const toast = useToast()
  const [methods, setMethods] = useState<SignInMethods | null>(null)
  const [busy, setBusy] = useState(false)
  const [password, setPassword] = useState('')

  useEffect(() => {
    api.signInMethods().then(setMethods).catch(() => undefined)
  }, [])

  // Back from the provider: say how the linking went, and drop the marker from the address.
  useEffect(() => {
    const url = new URL(window.location.href)
    const result = url.searchParams.get('linked')
    if (!result) return
    url.searchParams.delete('linked')
    window.history.replaceState(null, '', url.pathname + url.search + url.hash)
    if (result === 'ok') {
      toast.success(t('Your account is linked.'))
      void refresh()
    } else {
      toast.error(t('That sign-in is already linked to another account.'))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (!methods?.oidc || !user || (user.source && user.source !== 'local')) return null
  const name = methods.oidc.name
  const linked = !!user.oidcLinked

  async function link() {
    if (!password) {
      toast.error(t('Enter your password to link this account.'))
      return
    }
    setBusy(true)
    try {
      const { url } = await api.linkOidc(password)
      window.location.assign(url)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  async function unlink() {
    setBusy(true)
    try {
      await api.unlinkOidc()
      toast.success(t('Unlinked.'))
      await refresh()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="shield" size={14} /> {t('Sign in with {name}', { name })}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {linked
          ? t('This account is linked to {name}. Pressing "Sign in with {name}" on the sign-in page lands here. Your password still works.', { name })
          : t('Link this account to {name}, and the "Sign in with {name}" button on the sign-in page lands here. Your password keeps working.', { name })}
      </p>
      {linked ? (
        <button onClick={() => void unlink()} disabled={busy}>
          {t('Unlink')}
        </button>
      ) : (
        <>
          <label>
            {t('Your password')}
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
          </label>
          <button className="primary" onClick={() => void link()} disabled={busy}>
            {t('Link to {name}', { name })}
          </button>
        </>
      )}
    </fieldset>
  )
}
