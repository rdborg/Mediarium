import { useEffect, useState } from 'react'
import { api, type EReaderAccess } from '../api'
import { DOCS_URL } from '../docs'
import { t, tx } from '../i18n'
import CopyBox from './CopyBox'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { useToast } from './Toast'

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Your profile: read your Mediarium books on an e-reader. Two ways, both off
// until you turn them on: a catalogue that reading apps (KOReader, Moon+
// Reader, Librera, Marvin, Foliate) connect to, and sync for a Kobo.
export default function EReaderCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [access, setAccess] = useState<EReaderAccess | null>(null)
  const [password, setPassword] = useState('') // shown once, right after it is made
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.eReader().then(setAccess).catch(() => undefined)
  }, [])

  async function run(work: () => Promise<EReaderAccess>, done: string) {
    setBusy(true)
    try {
      const a = await work()
      setAccess(a)
      toast.success(done)
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function makePassword() {
    setBusy(true)
    try {
      const a = await api.newEReaderPassword()
      setPassword(a.password)
      setAccess(a)
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  if (!access) return null
  const origin = window.location.origin
  const catalogue = origin + access.opds.path
  const kobo = access.kobo.on && access.kobo.path ? origin + access.kobo.path : ''

  return (
    <fieldset className="group">
      <legend>
        <Icon name="book" size={14} /> {t('Read on an e-reader')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Get the ebooks in your library onto an e-reader. Both ways below are off until you turn them on, and each only lets the e-reader read books.')}
      </p>
      {!access.ebooks && <p className="error-text">{t('Ebooks are switched off, so nothing will show up on an e-reader yet. An administrator can switch them on under Settings > Basics > Media types.')}</p>}
      {access.ebooks && !access.allowed && <p className="error-text">{t("Your account isn't allowed to read books, so an e-reader will be turned away. Ask an administrator.")}</p>}

      <h3 aria-level={2} style={{ margin: '16px 0 6px', fontSize: '1rem' }}>{t('Reading apps (OPDS)')}</h3>
      <p style={{ color: 'var(--text-dim)', margin: '0 0 8px' }}>
        {t('For KOReader, Moon+ Reader, Librera, Marvin, Foliate and other apps that can connect to an OPDS catalogue. Add a catalogue in the app with this address, your username and an e-reader password. It is not your sign-in password.')}
      </p>
      {access.opds.on ? (
        <>
          <p style={{ margin: '6px 0 2px' }}>{t('Catalogue address')}</p>
          <CopyBox text={catalogue} label={t('Catalogue address')} />
          <p style={{ margin: '10px 0 2px' }}>{t('User name')}</p>
          <CopyBox text={access.username} label={t('User name')} />
          {password ? (
            <>
              <p style={{ margin: '10px 0 2px' }}>{t('E-reader password')}</p>
              <CopyBox text={password} label={t('E-reader password')} />
              <p className="field-hint" style={{ color: 'var(--warning)' }}>
                {t("Copy it now. You won't see it again. If you lose it, make a new one.")}
              </p>
            </>
          ) : (
            <p className="field-hint">{t("The password was shown when you made it. If you've lost it, make a new one (the old one stops working).")}</p>
          )}
          <div className="form-actions" style={{ justifyContent: 'flex-start' }}>
            <button disabled={busy} onClick={() => void makePassword()}>
              {t('Make a new password')}
            </button>
            <button
              disabled={busy}
              onClick={() => {
                setPassword('')
                void run(() => api.removeEReaderPassword(), t('The e-reader password is turned off.'))
              }}
            >
              {t('Turn it off')}
            </button>
          </div>
        </>
      ) : (
        <button className="primary" disabled={busy} onClick={() => void makePassword()}>
          {t('Make an e-reader password')}
        </button>
      )}

      <h3 style={{ margin: '22px 0 6px', fontSize: '1rem' }}>{t('Kobo sync')}</h3>
      <p style={{ color: 'var(--text-dim)', margin: '0 0 8px' }}>
        {tx('Your Kobo downloads your ebooks over Wi-Fi, and your place in each book is kept in step with Mediarium Books. You paste one line into a settings file on the Kobo. {guide}', {
          guide: (
            <a href={`${DOCS_URL}/books.md#kobo-sync`} target="_blank" rel="noreferrer">
              {t('Step by step')}
            </a>
          ),
        })}
      </p>
      {kobo ? (
        <>
          <p style={{ margin: '6px 0 2px' }}>{tx('The line to put in {file}, under {section}', { file: <code>Kobo eReader.conf</code>, section: <code>[OneStoreServices]</code> })}</p>
          <CopyBox text={`api_endpoint=${kobo}`} label={t('The Kobo address line')} />
          <p className="field-hint">{t('Anyone with this address can read your books, so keep it to yourself. A new address stops the old one working and your Kobo needs the new line.')}</p>
          <div className="form-actions" style={{ justifyContent: 'flex-start' }}>
            <button
              disabled={busy}
              onClick={() => void run(() => api.koboSyncAgain(), t('The next sync sends every book again.'))}
              title={t('Use this after a factory reset, or to bring back a book you took off the Kobo.')}
            >
              {t('Send everything again')}
            </button>
            <button
              disabled={busy}
              onClick={async () => {
                if (!(await confirm({ title: t('Make a new Kobo address?'), body: <p>{t('The old address stops working. You have to put the new line on your Kobo.')}</p>, confirmLabel: t('Make a new address') }))) return
                await run(() => api.newKoboAddress(), t('New address made. Update it on your Kobo.'))
              }}
            >
              {t('Make a new address')}
            </button>
            <button
              disabled={busy}
              onClick={async () => {
                if (!(await confirm({ title: t('Turn off Kobo sync?'), body: <p>{t('Your Kobo can no longer reach Mediarium. The books already on it stay there.')}</p>, confirmLabel: t('Turn it off'), danger: true }))) return
                await run(() => api.removeKoboAddress(), t('Kobo sync is turned off.'))
              }}
            >
              {t('Turn it off')}
            </button>
          </div>
        </>
      ) : (
        <button className="primary" disabled={busy} onClick={() => void run(() => api.newKoboAddress(), t('Your Kobo address is ready.'))}>
          {t('Make my Kobo address')}
        </button>
      )}
    </fieldset>
  )
}
