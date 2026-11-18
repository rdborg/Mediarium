import { useEffect, useState } from 'react'
import { api, displayName, isAdmin, type OAuthRequest } from '../api'
import { useAuth } from '../AuthContext'
import BrandMark from '../components/BrandMark'
import AmbientBackdrop from '../components/AmbientBackdrop'
import Switch from '../components/Switch'
import { titleFor } from '../documentTitle'
import { SKIP_ONCE_KEY } from '../autoSignIn'
import { t, tx } from '../i18n'

// Where an AI assistant that signs in with OAuth (Home Assistant, a chat
// program) sends the browser: the app's own request, shown here for an
// administrator to allow or refuse. Reaching this page without being signed in
// shows the usual sign-in page first and comes back here.
export default function ConnectApp() {
  const { user, setUser } = useAuth()
  const id = new URLSearchParams(window.location.search).get('r') ?? ''
  const [req, setReq] = useState<OAuthRequest | null>(null)
  const [problem, setProblem] = useState('')
  const [busy, setBusy] = useState(false)
  const [write, setWrite] = useState(true)
  const [leaving, setLeaving] = useState(false)

  useEffect(() => {
    document.title = titleFor(t('Connect an app'))
    if (!id) {
      setProblem(t('This sign-in link is not complete. Go back to the app and start the connection again.'))
      return
    }
    api
      .oauthRequest(id)
      .then(setReq)
      .catch((e) => setProblem(e instanceof Error ? e.message : String(e)))
  }, [id])

  async function decide(allow: boolean) {
    setBusy(true)
    setProblem('')
    try {
      const res = await api.oauthDecide(id, allow, write)
      setLeaving(true)
      window.location.assign(res.redirect)
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  async function signOut() {
    try {
      await api.logout()
      try {
        sessionStorage.setItem(SKIP_ONCE_KEY, '1')
      } catch {
        // storage blocked: the sign-in page may redirect once
      }
      setUser(null)
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e))
    }
  }

  const admin = isAdmin(user)
  const canWrite = !!req?.scopes.includes('mcp:write')
  const name = req?.clientName ?? ''

  return (
    <main className="auth-page">
      <AmbientBackdrop level="full" />
      <div className="auth-card connect-card">
        <div className="auth-logo">
          <BrandMark className="auth-mark" />
          <span>Mediarium</span>
        </div>
        {!req && !problem && <p className="auth-sub" role="status">{t('Loading')}</p>}
        {problem && <p className="error-text" role="alert">{problem}</p>}
        {req && !leaving && (
          <>
            <h1 className="connect-title">
              {canWrite
                ? tx('Allow {name} to read your library (and make changes after confirmation)?', { name: <strong>{name}</strong> })
                : tx('Allow {name} to read your library?', { name: <strong>{name}</strong> })}
            </h1>
            <p className="auth-sub">{tx('Signed in as {name}', { name: <strong>{displayName(user)}</strong> })}</p>
            <p className="connect-note">
              {tx("It can ask about titles, files, downloads and requests. It can't see your passwords or API keys. After this your browser goes to {host}.", { host: <code>{req.redirectHost}</code> })}
            </p>
            {admin ? (
              <>
                {canWrite && (
                  <Switch
                    checked={write}
                    onChange={setWrite}
                    label={t('Let it make changes too')}
                    description={t('Adding a title or changing a profile still only happens after it has shown you what would change and you said yes.')}
                  />
                )}
                <div className="connect-actions">
                  <button className="primary" disabled={busy} onClick={() => void decide(true)}>
                    {t('Allow')}
                  </button>
                  <button disabled={busy} onClick={() => void decide(false)}>
                    {t('Deny')}
                  </button>
                </div>
                <p className="connect-note">{t('You can disconnect it again under Settings > Media servers and apps > Other apps.')}</p>
              </>
            ) : (
              <>
                <p className="error-text" role="alert">
                  {t('Only an administrator can let an app connect to Mediarium. Sign in with an administrator account to continue.')}
                </p>
                <div className="connect-actions">
                  <button className="primary" disabled={busy} onClick={() => void signOut()}>
                    {t('Sign out and use another account')}
                  </button>
                  <button disabled={busy} onClick={() => void decide(false)}>
                    {t('Deny')}
                  </button>
                </div>
              </>
            )}
          </>
        )}
        {leaving && <p className="auth-done" role="status">{t('Taking you back to the app… You can close this tab afterwards.')}</p>}
      </div>
    </main>
  )
}
