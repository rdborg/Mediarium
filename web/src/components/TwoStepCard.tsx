import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { api, type TwoStepSetup, type TwoStepStatus } from '../api'
import { plural, t, tx } from '../i18n'
import Icon from './Icon'
import { copyText } from './CopyBox'
import { useToast } from './Toast'
import TipLabel from './TipLabel'

// The QR code as an SVG: one path for the dark modules, a light square behind
// with the four-module quiet zone scanners need.
export function QRCode({ rows, label }: { rows: string[]; label: string }) {
  const path = useMemo(() => {
    let d = ''
    rows.forEach((row, y) => {
      for (let x = 0; x < row.length; x++) if (row[x] === '1') d += `M${x + 4} ${y + 4}h1v1h-1z`
    })
    return d
  }, [rows])
  const size = rows.length + 8
  return (
    <svg className="qr-code" viewBox={`0 0 ${size} ${size}`} role="img" aria-label={label} shapeRendering="crispEdges">
      <rect width={size} height={size} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  )
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const toast = useToast()
  const [saved, setSaved] = useState(false)
  const text = `${t('Mediarium recovery codes (each works once)')}\n\n${codes.join('\n')}\n`
  return (
    <div className="recovery-codes">
      <p>
        <strong>{t('Save these recovery codes somewhere safe.')}</strong> {t('If you lose your phone, each one signs you in once instead of an app code. They are shown only now.')}
      </p>
      <ol className="recovery-list">
        {codes.map((c) => (
          <li key={c}>
            <code>{c}</code>
          </li>
        ))}
      </ol>
      <div className="row-actions">
        <button
          className="btn-with-icon"
          onClick={() =>
            void copyText(text).then((ok) => (ok ? toast.success(t('Copied.')) : toast.error(t("Couldn't copy. Select the codes and copy them by hand."))))
          }
        >
          <Icon name="list" size={15} /> {t('Copy')}
        </button>
        <a className="btn btn-with-icon" href={`data:text/plain;charset=utf-8,${encodeURIComponent(text)}`} download="mediarium-recovery-codes.txt">
          <Icon name="download" size={15} /> {t('Download')}
        </a>
      </div>
      <label className="check-row">
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} /> {t('I have saved them')}
      </label>
      <button className="primary" disabled={!saved} onClick={onDone}>
        {t('Done')}
      </button>
    </div>
  )
}

// Settings > People and access > Your profile: two-step sign-in for the signed-in person. Off
// until they set it up; suggested (never forced) once Mediarium can be
// reached from outside the home.
export default function TwoStepCard() {
  const toast = useToast()
  const [status, setStatus] = useState<TwoStepStatus | null>(null)
  const [setup, setSetup] = useState<TwoStepSetup | null>(null)
  const [codes, setCodes] = useState<string[] | null>(null)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [asking, setAsking] = useState<'off' | 'recovery' | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .twoStep()
      .then(setStatus)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function run(fn: () => Promise<void>) {
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const start = () => run(async () => setSetup(await api.twoStepSetup()))

  const confirm = (e: FormEvent) => {
    e.preventDefault()
    void run(async () => {
      const res = await api.twoStepEnable(code)
      setSetup(null)
      setCode('')
      setCodes(res.recoveryCodes)
      load()
    })
  }

  const withPassword = (e: FormEvent) => {
    e.preventDefault()
    void run(async () => {
      if (asking === 'off') {
        await api.twoStepDisable(password)
        toast.success(t('Two-step sign-in is off.'))
      } else {
        setCodes((await api.twoStepRecovery(password)).recoveryCodes)
      }
      setPassword('')
      setAsking(null)
      load()
    })
  }

  return (
    <fieldset className="group two-step">
      <legend>
        <Icon name="shield" size={14} /> <TipLabel help="accounts#two-step-sign-in">{t('Two-step sign-in')}</TipLabel>
      </legend>
      {error && <p className="error-text">{error}</p>}
      {codes ? (
        <RecoveryCodes codes={codes} onDone={() => setCodes(null)} />
      ) : setup ? (
        <div className="two-step-setup">
          <QRCode rows={setup.qr} label={t('QR code to add Mediarium to your authenticator app')} />
          <div>
            <ol className="steps-list">
              <li>{t('Open your authenticator app (Google Authenticator, Microsoft Authenticator, 1Password, Bitwarden…) and add an account.')}</li>
              <li>
                {tx('Scan this code. Or type in this key: {key}', { key: <code className="secret-key">{setup.secret}</code> })}
              </li>
              <li>{t('Enter the six-digit code the app now shows.')}</li>
            </ol>
            <form className="row-actions" onSubmit={confirm}>
              <input
                className="code-input"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/[^0-9]/g, '').slice(0, 6))}
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="123456"
                aria-label={t('Six-digit code')}
              />
              <button className="primary" type="submit" disabled={busy || code.length !== 6}>
                {t('Switch on')}
              </button>
              <button type="button" onClick={() => setSetup(null)} disabled={busy}>
                {t('Cancel')}
              </button>
            </form>
          </div>
        </div>
      ) : status === null ? (
        <p className="hint">{t('Loading…')}</p>
      ) : status.enabled ? (
        <>
          <p>
            <span className="badge downloaded">{t('On')}</span> {t('Signing in asks for the code from your authenticator app after your password.')}{' '}
            {plural(status.recoveryLeft, '{n} recovery code is left.', '{n} recovery codes are left.')}
          </p>
          {asking ? (
            <form className="row-actions" onSubmit={withPassword}>
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" placeholder={t('Your password')} aria-label={t('Your password')} autoFocus />
              <button className={asking === 'off' ? 'danger-ghost' : 'primary'} type="submit" disabled={busy || !password}>
                {asking === 'off' ? t('Switch off') : t('Make new codes')}
              </button>
              <button type="button" onClick={() => setAsking(null)} disabled={busy}>
                {t('Cancel')}
              </button>
            </form>
          ) : (
            <div className="row-actions">
              <button onClick={() => setAsking('recovery')}>{t('New recovery codes')}</button>
              <button className="danger-ghost" onClick={() => setAsking('off')}>
                {t('Switch off')}
              </button>
            </div>
          )}
        </>
      ) : (
        <>
          {status.suggested && (
            <p className="callout">
              <Icon name="shield" size={15} /> {status.suggested} {t('A code from your phone on top of your password keeps your account safe even if the password leaks.')}
            </p>
          )}
          <p style={{ color: 'var(--text-dim)' }}>
            {t('After your password, Mediarium also asks for a six-digit code from an authenticator app on your phone. Off unless you switch it on.')}
          </p>
          <button className="primary btn-with-icon" onClick={() => void start()} disabled={busy}>
            <Icon name="shield" size={15} /> {t('Set up two-step sign-in')}
          </button>
        </>
      )}
    </fieldset>
  )
}
