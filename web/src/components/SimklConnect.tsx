import { useEffect, useRef, useState } from 'react'
import { api, type SimklStatus } from '../api'
import { t } from '../i18n'
import { pollPin } from '../pinPoll'
import { useConfirm } from './ConfirmProvider'
import { copyText } from './CopyBox'
import Icon from './Icon'
import TestButton from './TestButton'
import { useToast } from './Toast'

// Settings > Library and files > Import lists: connect the Simkl
// account whose lists a Simkl import list reads. The person enters a short
// code on Simkl's site (the server works out which of Simkl's two sign-ins the
// app uses); the sign-in itself is kept on the server and never sent to the
// browser.

// Minutes and seconds left, such as 9:05.
function clock(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`
}

export default function SimklConnect({ hasClientId, onChanged }: { hasClientId: boolean; onChanged?: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [st, setSt] = useState<SimklStatus | null>(null)
  const [pin, setPin] = useState<{ code: string; url: string; endsAt: number } | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [copied, setCopied] = useState(false)
  const [name, setName] = useState('')
  const wait = useRef(5000)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const stopPoll = useRef<() => void>(() => undefined)

  const gone = useRef(false)

  useEffect(() => {
    api.simklStatus().then(setSt).catch(() => setSt({ connected: false, unreadable: false, hasClientId }))
  }, [hasClientId])

  // The code is only waited for while this card is on screen. (Not tied to
  // hasClientId: changing that must not stop the wait and leave the code up.)
  useEffect(() => {
    gone.current = false
    return () => {
      gone.current = true
      stopPoll.current()
    }
  }, [])

  // The countdown to the code running out.
  useEffect(() => {
    if (!pin) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [pin])

  async function copyCode(code: string) {
    setCopied(await copyText(code))
    setTimeout(() => setCopied(false), 2200)
  }

  async function connect() {
    setError('')
    setBusy(true)
    try {
      const p = await api.simklStartPin()
      if (gone.current) return
      setPin({ code: p.code, url: p.fullUrl || p.url, endsAt: Date.now() + p.expiresIn * 1000 })
      setCopied(false)
      wait.current = Math.max(3, p.interval || 5) * 1000
      stopPoll.current()
      stopPoll.current = pollPin({
        check: async () => {
          const r = await api.simklPinStatus(p.code)
          if (r.interval) wait.current = Math.max(3, r.interval) * 1000
          return r.done && r.status ? { status: r.status, name: r.name ?? '' } : undefined
        },
        lifeSeconds: p.expiresIn,
        delayMs: () => wait.current,
        onDone: ({ status, name }) => {
          setPin(null)
          setSt(status)
          setName(name)
          toast.success(name ? t('Connected to Simkl as {name}.', { name }) : t('Simkl account connected.'))
          onChanged?.()
        },
        onExpired: () => {
          setPin(null)
          setError(t('The code expired. Start again.'))
        },
        onError: (e) => {
          setPin(null)
          setError(e instanceof Error ? e.message : String(e))
        },
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  function cancel() {
    stopPoll.current()
    setPin(null)
  }

  async function disconnect() {
    const ok = await confirm({
      title: t('Disconnect your Simkl account?'),
      body: <p>{t('Your Simkl lists in Mediarium stop being read until you connect again. Titles they already added stay in your library.')}</p>,
      confirmLabel: t('Disconnect'),
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    setError('')
    try {
      await api.simklDisconnect()
      setSt({ connected: false, unreadable: false, hasClientId })
      setName('')
      toast.success(t('Simkl account disconnected.'))
      onChanged?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!st) return null
  return (
    <div className="simkl-connect" style={{ marginTop: 14 }}>
      <h3 style={{ margin: '0 0 6px' }}>{t('Your Simkl account')}</h3>
      {st.connected ? (
        <>
          <p style={{ margin: '0 0 10px' }}>{name ? t('Connected as {name}.', { name }) : t('Your Simkl account is connected.')}</p>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            <button disabled={busy} onClick={() => void disconnect()}>
              {t('Disconnect')}
            </button>
            <TestButton label={t('Test connection')} run={() => api.simklTest()} />
          </div>
        </>
      ) : pin ? (
        <>
          <p style={{ margin: '0 0 8px' }}>{t('Open Simkl, enter this code and approve the sign-in.')}</p>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'center', margin: '0 0 10px' }}>
            <p aria-live="polite" style={{ margin: 0, fontSize: '1.8rem', fontWeight: 700, letterSpacing: '0.2em' }}>
              <code style={{ userSelect: 'all' }}>{pin.code}</code>
            </p>
            <button type="button" className="btn-sm btn-with-icon" onClick={() => void copyCode(pin.code)}>
              {copied && <Icon name="check" size={14} />}
              {copied ? t('Copied') : t('Copy')}
            </button>
            <a className="btn-link primary-look" href={pin.url} target="_blank" rel="noreferrer">
              <Icon name="open" size={15} /> {t('Open Simkl and enter this code')}
            </a>
          </div>
          <p role="status" style={{ color: 'var(--text-dim)', margin: '0 0 4px' }}>
            {t('Waiting for you to approve it on Simkl…')}
          </p>
          <p style={{ color: 'var(--text-dim)', margin: '0 0 10px', fontSize: '0.85rem' }}>
            {t('The code runs out in {time}.', { time: clock(pin.endsAt - now) })}
          </p>
          <button onClick={cancel}>{t('Cancel')}</button>
        </>
      ) : (
        <>
          {st.unreadable && <div className="notice notice-warn">{t("The saved Simkl sign-in can't be read. Connect your account again.")}</div>}
          <p style={{ margin: '0 0 10px', color: 'var(--text-dim)' }}>
            {hasClientId ? t('Not connected yet. Connect your account so Mediarium can read your Simkl lists.') : t('Add your Simkl client ID above first.')}
          </p>
          <button className="primary" disabled={busy || !hasClientId} onClick={() => void connect()}>
            {error ? t('Start again') : t('Connect my Simkl account')}
          </button>
        </>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
