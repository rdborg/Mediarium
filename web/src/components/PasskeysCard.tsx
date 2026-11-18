import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { api, isAdmin, type PasskeyList } from '../api'
import { useAuth } from '../AuthContext'
import { locale, t } from '../i18n'
import { createPasskey, passkeyProblem, passkeysSupported } from '../passkey'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { useToast } from './Toast'
import TipLabel from './TipLabel'

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Settings > People and access > Your profile: the passkeys of the signed-in account. A passkey
// signs in without typing a password, with the fingerprint, face or PIN of the
// device. Off until an administrator switches passkeys on (which needs the
// public address of Mediarium); then each person adds as many as they like.
export default function PasskeysCard() {
  const { user } = useAuth()
  const toast = useToast()
  const confirm = useConfirm()
  const [list, setList] = useState<PasskeyList | null>(null)
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [renaming, setRenaming] = useState<number | null>(null)
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .passkeys()
      .then(setList)
      .catch((e) => setError(errorText(e)))
  }, [])
  useEffect(load, [load])

  // Passkeys that are simply off stay out of the way, except for an
  // administrator, who is told how to switch them on.
  if (!list) return error ? <p className="error-text">{error}</p> : null
  if (!list.enabled && list.keys.length === 0 && !isAdmin(user)) return null

  async function add(e: FormEvent) {
    e.preventDefault()
    if (!password) {
      setError(t('Enter your password to add a passkey.'))
      return
    }
    setBusy(true)
    setError('')
    try {
      const begin = await api.passkeyRegisterBegin(password)
      const answer = await createPasskey(begin.options)
      await api.passkeyRegisterFinish(begin.ceremony, name.trim(), answer)
      toast.success(t('Passkey added.'))
      setAdding(false)
      setName('')
      setPassword('')
      load()
    } catch (err) {
      setError(err instanceof DOMException ? passkeyProblem(err, list?.host) : errorText(err))
    } finally {
      setBusy(false)
    }
  }

  async function rename(id: number) {
    try {
      await api.renamePasskey(id, newName.trim())
      setRenaming(null)
      load()
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  async function remove(id: number, label: string) {
    const yes = await confirm({
      title: t('Remove "{name}"?', { name: label }),
      body: <p>{t("You can't sign in with it any more. If it is on a device you still have, delete it there too.")}</p>,
      confirmLabel: t('Remove passkey'),
      danger: true,
    })
    if (!yes) return
    try {
      await api.deletePasskey(id)
      toast.success(t('Passkey removed.'))
      load()
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  const supported = passkeysSupported()
  return (
    <fieldset className="group passkeys">
      <legend>
        <Icon name="key" size={14} /> <TipLabel help="accounts#passkeys">{t('Passkeys')}</TipLabel>
        {list.keys.length > 0 && <span className="legend-count">{list.keys.length}</span>}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t("Sign in with your fingerprint, face or device PIN instead of typing a password. Mediarium keeps only the public half of each passkey. Add one for every phone, computer or security key you use.")}
      </p>
      {!list.available && list.reason && <p className="hint">{list.reason}</p>}
      {error && <p className="error-text">{error}</p>}
      {list.keys.length > 0 && (
        <ul className="passkey-list" style={{ listStyle: 'none', padding: 0, margin: '0 0 14px', display: 'grid', gap: 8 }}>
          {list.keys.map((k) => (
            <li key={k.id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10, justifyContent: 'space-between' }}>
              {renaming === k.id ? (
                <form
                  style={{ display: 'flex', gap: 8, flex: '1 1 260px' }}
                  onSubmit={(e) => {
                    e.preventDefault()
                    void rename(k.id)
                  }}
                >
                  <input value={newName} onChange={(e) => setNewName(e.target.value)} maxLength={60} autoFocus aria-label={t('Passkey name')} />
                  <button className="primary" type="submit" disabled={!newName.trim()}>
                    {t('Save')}
                  </button>
                  <button type="button" onClick={() => setRenaming(null)}>
                    {t('Cancel')}
                  </button>
                </form>
              ) : (
                <>
                  <span>
                    <strong>{k.name}</strong>
                    <small style={{ display: 'block', color: 'var(--text-dim)' }}>
                      {t('Added {date}', { date: new Date(k.createdAt).toLocaleDateString(locale()) })}
                      {' · '}
                      {k.lastUsedAt ? t('Last used {date}', { date: new Date(k.lastUsedAt).toLocaleDateString(locale()) }) : t('Never used')}
                      {k.backedUp ? ' · ' + t('Synced across your devices') : ''}
                    </small>
                  </span>
                  <span style={{ display: 'flex', gap: 8 }}>
                    <button
                      className="btn-sm"
                      onClick={() => {
                        setRenaming(k.id)
                        setNewName(k.name)
                      }}
                    >
                      {t('Rename')}
                    </button>
                    <button className="btn-sm danger-ghost" onClick={() => void remove(k.id, k.name)}>
                      {t('Remove')}
                    </button>
                  </span>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {list.available &&
        (adding ? (
          <form className="form-cols" style={{ alignItems: 'end' }} onSubmit={(e) => void add(e)}>
            <label>
              {t('Name')} <small style={{ color: 'var(--text-dim)' }}>{t('(so you can tell them apart)')}</small>
              <input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} placeholder={t('for example: my phone')} autoFocus />
            </label>
            <label>
              {t('Your password')}
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
            </label>
            <div style={{ display: 'flex', gap: 8 }}>
              <button className="primary" type="submit" disabled={busy || !supported}>
                {busy ? t('Waiting for your device…') : t('Add passkey')}
              </button>
              <button type="button" onClick={() => setAdding(false)} disabled={busy}>
                {t('Cancel')}
              </button>
            </div>
          </form>
        ) : (
          <div>
            <button className="btn-with-icon" onClick={() => setAdding(true)} disabled={list.keys.length >= list.max}>
              <Icon name="plus" size={15} /> {t('Add a passkey')}
            </button>
            {!supported && <span className="hint" style={{ marginLeft: 10 }}>{t("This browser can't make passkeys on this page. Open Mediarium at its public address in a current browser.")}</span>}
          </div>
        ))}
    </fieldset>
  )
}
