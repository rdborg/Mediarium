import { useEffect, useState, type FormEvent } from 'react'
import { api, isTwoStepChallenge, type SignInMethods } from '../api'
import { getPasskey, passkeyProblem, passkeysSupported } from '../passkey'
import { signInProblem } from '../signInProblem'
import { useAuth } from '../AuthContext'
import BrandMark from '../components/BrandMark'
import AmbientBackdrop from '../components/AmbientBackdrop'
import { DOCS_URL } from '../docs'
import { titleFor } from '../documentTitle'
import { required } from '../validate'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { LANGUAGES, chooseLanguage, currentLanguage, plural, t, tx, type Lang } from '../i18n'
import { withBase, withoutBase } from '../base'
import { SKIP_ONCE_KEY, TRIED_KEY, autoSignInUrl, shouldAutoSignIn } from '../autoSignIn'
import { takeRestored } from '../restoreNotice'
import { signedOutForIdle } from '../sessionEnd'

// Reads the problem code from the address and takes it out again, so a reload doesn't show it twice.
function takeSignInError(): string {
  try {
    const url = new URL(window.location.href)
    const code = url.searchParams.get('signin_error') ?? ''
    if (code) {
      url.searchParams.delete('signin_error')
      window.history.replaceState(null, '', url.pathname + url.search + url.hash)
    }
    return code.replace(/[^a-z_]/g, '').slice(0, 40)
  } catch {
    return ''
  }
}

// The sessionStorage may be blocked; then nothing is remembered and the page works as before.
function readStored(key: string): string | null {
  try {
    return sessionStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStored(key: string, value: string | null) {
  try {
    if (value === null) sessionStorage.removeItem(key)
    else sessionStorage.setItem(key, value)
  } catch {
    // storage blocked: the page just does not remember
  }
}

export default function Login() {
  const { refresh } = useAuth()
  const [methods, setMethods] = useState<SignInMethods>({ local: true, oidc: null, ldap: false, passkeys: false })
  const [problemCode] = useState(takeSignInError)
  const [restored] = useState(() => takeRestored())
  const [idle] = useState(() => signedOutForIdle()) // the server ended the session for sitting unused
  const [skipAutoOnce] = useState(() => readStored(SKIP_ONCE_KEY) === '1') // the person just signed out
  const [redirecting, setRedirecting] = useState(false)
  useEffect(() => {
    api
      .signInMethods()
      .then((m) => {
        setMethods(m)
        const tried = Number(readStored(TRIED_KEY))
        const go = shouldAutoSignIn({
          method: m.autoSignIn,
          oidcAvailable: !!m.oidc,
          search: window.location.search,
          problemCode,
          skipOnce: skipAutoOnce,
          notice: restored || idle,
          lastTriedAt: tried > 0 ? tried : null,
          now: Date.now(),
        })
        writeStored(SKIP_ONCE_KEY, null) // after a sign-out the normal page is shown once
        if (go) {
          writeStored(TRIED_KEY, String(Date.now()))
          setRedirecting(true)
          window.location.replace(autoSignInUrl(withBase('/api/auth/oidc/start'), withoutBase(window.location.pathname), window.location.search))
        }
      })
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const showForm = methods.local || methods.ldap
  async function passkeySignIn() {
    setError('')
    setBusy(true)
    try {
      const begin = await api.passkeyLoginBegin()
      const answer = await getPasskey(begin.options)
      await api.passkeyLoginFinish(begin.ceremony, answer)
      await refresh()
    } catch (err) {
      setError(err instanceof Error && !(err instanceof DOMException) ? err.message : passkeyProblem(err, methods.passkeyHost))
    } finally {
      setBusy(false)
    }
  }
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // With two-step sign-in on, the password leads to a second step: the code
  // from the authenticator app (or a recovery code).
  const [challenge, setChallenge] = useState('')
  useEffect(() => {
    document.title = titleFor(t('Sign in'))
  }, [])

  // Sign-in only checks that both boxes are filled in. The username format is
  // never checked here, so an older account with an unusual name can still get in.
  const v = useValidation({
    username: required(username, t('Enter your username.')),
    password: required(password, t('Enter your password.')),
  })

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!v.attempt()) return
    setError('')
    setBusy(true)
    try {
      const res = await api.login(username, password)
      if (isTwoStepChallenge(res)) {
        setChallenge(res.challenge)
        return
      }
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (redirecting) {
    return (
      <main className="auth-page">
        <AmbientBackdrop level="full" />
        <div className="auth-card">
          <div className="auth-logo">
            <BrandMark className="auth-mark" />
            <span>Mediarium</span>
          </div>
          <p className="auth-sub" role="status">
            {t('Taking you to {name}…', { name: methods.oidc?.name ?? t('single sign-on') })}
          </p>
        </div>
      </main>
    )
  }

  if (challenge) {
    return (
      <CodeStep
        challenge={challenge}
        onSignedIn={refresh}
        onStartOver={() => {
          setChallenge('')
          setPassword('')
        }}
      />
    )
  }

  return (
    <main className="auth-page">
      <AmbientBackdrop level="full" />
      <form className="auth-card" onSubmit={onSubmit}>
        <h1 className="sr-only">{t('Sign in to Mediarium')}</h1>
        <div className="auth-logo">
          <BrandMark className="auth-mark" />
          <span>Mediarium</span>
        </div>
        <p className="auth-sub">{t('Sign in to continue')}</p>
        {restored && !error && <p className="auth-done" role="status">{t('The backup was restored. Sign in again.')}</p>}
        {idle && !restored && !error && <p className="auth-done" role="status">{t('You were signed out because of inactivity.')}</p>}
        {error && <p className="error-text">{error}</p>}
        {!error && problemCode && <p className="error-text">{signInProblem(problemCode, methods.oidc?.name ?? t('single sign-on'))}</p>}
        {showForm && (
          <>
        <label>
          {t('Username')}
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus autoComplete="username" {...v.bind('username', username, setUsername)} />
          <FieldError v={v} name="username" />
        </label>
        <label>
          {t('Password')}
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" {...v.bind('password')} />
          <FieldError v={v} name="password" />
        </label>
        <button className="primary" type="submit" disabled={busy}>
          {busy ? t('Signing in…') : t('Sign in')}
        </button>
        <FormProblem v={v} action="signin" />
          </>
        )}
        {(methods.oidc || methods.passkeys) && (
          <div className="auth-alt">
            {showForm && <span className="auth-or">{t('or')}</span>}
            {methods.oidc && (
              <a className="btn auth-alt-btn" href={withBase('/api/auth/oidc/start')}>
                {t('Sign in with {name}', { name: methods.oidc.name })}
              </a>
            )}
            {methods.passkeys && (
              <button type="button" className="auth-alt-btn" onClick={() => void passkeySignIn()} disabled={busy || !passkeysSupported()} title={passkeysSupported() ? undefined : t("This browser can't use passkeys on this page.")}>
                {t('Sign in with a passkey')}
              </button>
            )}
          </div>
        )}
        {!methods.local && methods.ldap && <p className="auth-sub">{t('Use your directory account.')}</p>}
        {methods.local && (
        <details className="forgot">
          <summary>{t('Forgot your password?')}</summary>
          <p>
            {t("Mediarium can't send reset emails. Ask an administrator to set a new password in Settings > People and access > All accounts. If you're the only administrator, run this on the machine that runs Mediarium (swap 1000:1000 for your own PUID:PGID):")}
          </p>
          <code className="forgot-cmd">docker exec -it -u 1000:1000 mediarium /app/app reset-password &lt;username&gt;</code>
          <p>
            {tx('It asks for a new password and signs that account out everywhere. Not using Docker? See {link}.', {
              link: (
                <a href={`${DOCS_URL}/accounts.md`} target="_blank" rel="noreferrer">
                  {t('the accounts page')}
                </a>
              ),
            })}
          </p>
        </details>
        )}
        <select
          aria-label={t('App language')}
          value={currentLanguage()}
          onChange={(e) => chooseLanguage(e.target.value as Lang)}
          style={{ justifySelf: 'center', width: 'auto', fontSize: '0.85rem' }}
        >
          {LANGUAGES.map((l) => (
            <option key={l.code} value={l.code}>
              {l.name}
            </option>
          ))}
        </select>
      </form>
    </main>
  )
}

// The second step of signing in: the six-digit code from the authenticator
// app, or one of the recovery codes.
function CodeStep({ challenge, onSignedIn, onStartOver }: { challenge: string; onSignedIn: () => Promise<void> | void; onStartOver: () => void }) {
  const [useRecovery, setUseRecovery] = useState(false)
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [left, setLeft] = useState<number | null>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    const typed = code.trim()
    if (!typed) {
      setError(useRecovery ? t('Enter one of your recovery codes.') : t('Enter the six-digit code from your app.'))
      return
    }
    setError('')
    setBusy(true)
    try {
      const res = await api.loginCode(challenge, useRecovery ? { recoveryCode: typed } : { code: typed })
      if (useRecovery && typeof res.recoveryLeft === 'number' && res.recoveryLeft <= 3) {
        // Say how few are left before moving on.
        setLeft(res.recoveryLeft)
        return
      }
      await onSignedIn()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setCode('')
    } finally {
      setBusy(false)
    }
  }

  if (left !== null) {
    return (
      <main className="auth-page">
      <AmbientBackdrop level="full" />
        <div className="auth-card">
          <h1 className="auth-sub">{t("You're signed in")}</h1>
          <p>
            {left === 0
              ? t('That was your last recovery code. Make new ones under your profile, in Two-step sign-in.')
              : plural(left, 'Only {n} recovery code is left. Make new ones under your profile, in Two-step sign-in.', 'Only {n} recovery codes are left. Make new ones under your profile, in Two-step sign-in.')}
          </p>
          <button className="primary" onClick={() => void onSignedIn()}>
            {t('Continue')}
          </button>
        </div>
      </main>
    )
  }

  return (
    <main className="auth-page">
      <AmbientBackdrop level="full" />
      <form className="auth-card" onSubmit={submit}>
        <h1 className="sr-only">{t('Two-step sign-in')}</h1>
        <div className="auth-logo">
          <BrandMark className="auth-mark" />
          <span>Mediarium</span>
        </div>
        <p className="auth-sub">{useRecovery ? t('Enter a recovery code') : t('Enter the code from your authenticator app')}</p>
        {error && <p className="error-text">{error}</p>}
        <label>
          {useRecovery ? t('Recovery code') : t('Six-digit code')}
          {useRecovery ? (
            <input value={code} onChange={(e) => setCode(e.target.value)} autoFocus autoComplete="off" placeholder="abcd-efgh" />
          ) : (
            <input
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/[^0-9]/g, '').slice(0, 6))}
              autoFocus
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="123456"
              className="code-input"
            />
          )}
        </label>
        <button className="primary" type="submit" disabled={busy}>
          {busy ? t('Checking…') : t('Sign in')}
        </button>
        <button
          type="button"
          className="link-btn"
          onClick={() => {
            setUseRecovery(!useRecovery)
            setCode('')
            setError('')
          }}
        >
          {useRecovery ? t('Use the code from my app instead') : t("Can't use your app? Use a recovery code")}
        </button>
        <button type="button" className="link-btn" onClick={onStartOver}>
          {t('Start over')}
        </button>
        <details className="forgot">
          <summary>{t('Lost your phone and your recovery codes?')}</summary>
          <p>{t("An administrator can switch two-step sign-in off for your account in Settings > People and access > All accounts. If you're the only administrator, run this on the machine that runs Mediarium:")}</p>
          <code className="forgot-cmd">docker exec -it -u 1000:1000 mediarium /app/app reset-two-step &lt;username&gt;</code>
        </details>
      </form>
    </main>
  )
}
