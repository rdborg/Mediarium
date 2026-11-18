import { createPortal } from 'react-dom'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { api, DEFAULT_PERMISSIONS, displayName, isAdmin, type Account, type AccountChanges, type APIKey, type Permissions, type Role } from '../api'
import PermissionsChoice from '../components/PermissionsChoice'
import ProxySignIn from '../components/ProxySignIn'
import SignInMethods from '../components/SignInMethods'
import PasskeysCard from '../components/PasskeysCard'
import SsoLinkCard from '../components/SsoLinkCard'
import { useAuth } from '../AuthContext'
import ConfirmDialog from '../components/ConfirmDialog'
import { useConfirm } from '../components/ConfirmProvider'
import PasswordStrength from '../components/PasswordStrength'
import TwoStepCard from '../components/TwoStepCard'
import IdleSignOutCard from '../components/IdleSignOutCard'
import SessionsCard from '../components/SessionsCard'
import PlexWatchlistCard from '../components/PlexWatchlistCard'
import ListenBrainzCard from '../components/ListenBrainzCard'
import RequestNoticesCard from '../components/RequestNoticesCard'
import RequestSettingsCard from '../components/RequestSettingsCard'
import EReaderCard from '../components/EReaderCard'
import { useToast } from '../components/Toast'
import Icon from '../components/Icon'
import * as check from '../validate'
import { FieldError, FormProblem, useValidation, type FieldProps } from '../useValidation'
import { useFocusTrap } from '../useFocusTrap'
import { locale, plural, t, tx } from '../i18n'
import TipLabel from '../components/TipLabel'

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

function ProfileForm() {
  const { user, setUser } = useAuth()
  const toast = useToast()
  const [name, setName] = useState(user?.name && user.name !== user.username ? user.name : '')
  const [email, setEmail] = useState(user?.email ?? '')
  const [username, setUsername] = useState(user?.username ?? '')
  const [busy, setBusy] = useState(false)
  // An older username that breaks today's rules can stay as it is; the format
  // is only checked when it is changed.
  const v = useValidation({
    name: check.maxLength(name, 100, t('Your name')),
    username: check.firstError(
      check.required(username, t('Enter the username you sign in with.')),
      username.trim() !== user?.username && check.username(username),
    ),
    email: check.email(email),
  })

  async function save() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      const updated = await api.updateAccount({ username: username.trim(), name: name.trim(), email: email.trim() })
      setUser({ ...user, ...updated })
      toast.success(t('Profile saved.'))
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="user" size={14} /> {t('Your details')}
      </legend>
      <div className="form-cols">
        <label>
          {t('Name')} <small style={{ color: 'var(--text-dim)' }}>{t('(shown in your greeting)')}</small>
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" placeholder={t('Leave empty to use your username')} {...v.bind('name', name, setName)} />
          <FieldError v={v} name="name" />
        </label>
        <label>
          {t('Username')} <small style={{ color: 'var(--text-dim)' }}>{t('(what you sign in with)')}</small>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" {...v.bind('username', username, setUsername)} />
          <FieldError v={v} name="username" />
        </label>
        <label>
          {t('Email')} <small style={{ color: 'var(--text-dim)' }}>{t('(used for email notifications)')}</small>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" {...v.bind('email', email, setEmail)} />
          <FieldError v={v} name="email" />
        </label>
      </div>
      <div style={{ marginTop: 14 }}>
        <button className="primary" onClick={save} disabled={busy}>
          {t('Save details')}
        </button>
        <FormProblem v={v} action="save" />
      </div>
    </fieldset>
  )
}

function PasswordForm() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const toast = useToast()
  const v = useValidation({
    currentPassword: check.required(currentPassword, t('Enter your current password.')),
    newPassword: check.firstError(check.required(newPassword, t('Choose a new password of at least 8 characters.')), check.password(newPassword)),
    confirmPassword: check.firstError(check.required(confirmPassword, t('Type the new password again to make sure it is right.')), check.passwordsMatch(newPassword, confirmPassword)),
  })

  async function submit() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      await api.changePassword(currentPassword, newPassword)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      v.reset()
      toast.success(t('Password changed.'))
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group alerts">
      <legend>
        <Icon name="key" size={14} /> {t('Change password')}
      </legend>
      <div className="grid-form">
        <label>
          {t('Current password')}
          <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} autoComplete="current-password" {...v.bind('currentPassword')} />
          <FieldError v={v} name="currentPassword" />
        </label>
        <div className="form-cols">
          <label>
            {t('New password')}
            <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} autoComplete="new-password" {...v.bind('newPassword')} />
            <FieldError v={v} name="newPassword" />
          </label>
          <label>
            {t('Confirm new password')}
            <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} autoComplete="new-password" {...v.bind('confirmPassword')} />
            <FieldError v={v} name="confirmPassword" />
          </label>
        </div>
        <PasswordStrength password={newPassword} />
        <div>
          <button className="primary" onClick={submit} disabled={busy}>
            {t('Change password')}
          </button>
          <FormProblem v={v} action="password" />
        </div>
      </div>
    </fieldset>
  )
}

function APIKeys() {
  const [keys, setKeys] = useState<APIKey[] | null>(null)
  const [name, setName] = useState('')
  const [readOnly, setReadOnly] = useState(false)
  const [newKey, setNewKey] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const toast = useToast()
  const confirm = useConfirm()
  const v = useValidation({
    name: check.firstError(check.required(name, t('Give the key a name so you can tell it apart later, for example "my script".')), check.maxLength(name, 100, t('The name'))),
  })

  function reload() {
    api.listAPIKeys().then(setKeys).catch(() => undefined)
  }
  useEffect(reload, [])

  async function create() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      const created = await api.createAPIKey(name.trim(), readOnly)
      setNewKey(created.key ?? null)
      setName('')
      setReadOnly(false)
      v.reset()
      reload()
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function revoke(k: APIKey) {
    if (!(await confirm({ title: t('Revoke "{name}"?', { name: k.name }), body: <p>{t('Anything using this key stops working straight away. This cannot be undone.')}</p>, confirmLabel: t('Revoke key'), danger: true }))) return
    try {
      await api.revokeAPIKey(k.id)
      toast.success(t('Key revoked.'))
      reload()
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  async function remove(k: APIKey) {
    if (!(await confirm({ title: t('Delete "{name}"?', { name: k.name }), body: <p>{t('It is already revoked and does nothing. This takes it off the list for good.')}</p>, confirmLabel: t('Delete key'), danger: true }))) return
    try {
      await api.deleteAPIKey(k.id)
      toast.success(t('Key deleted.'))
      reload()
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  return (
    <fieldset className="group torrent">
      <legend>
        <Icon name="server" size={14} /> <TipLabel help="accounts#read-only-api-keys">{t('API keys')}</TipLabel>
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {tx('For scripts and other apps: send a key as an {header} header instead of signing in. A key is shown only once, when you create it.', { header: <code>X-API-Key</code> })}
      </p>
      {newKey && (
        <p className="newkey">
          {newKey}
          <br />
          <span style={{ color: 'var(--warning)', fontFamily: 'var(--font-body)' }}>{t("Copy it now. You won't see it again.")}</span>
        </p>
      )}
      {keys && keys.length > 0 && (
        <table style={{ marginBottom: 16 }}>
          <thead>
            <tr>
              <th>{t('Name')}</th>
              <th>{t('Created')}</th>
              <th>{t('Status')}</th>
              <th>
              <span className="sr-only">{t('Actions')}</span>
            </th>
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id}>
                <td>
                  {k.name}
                  {k.readOnly && (
                    <span className="badge" style={{ marginLeft: 8 }} title={t('Can only read. Any request that changes something is refused.')}>
                      {t('read-only')}
                    </span>
                  )}
                </td>
                <td>{new Date(k.createdAt).toLocaleDateString(locale())}</td>
                <td>
                  <span className={`badge ${k.revokedAt ? 'missing' : 'downloaded'}`}>{k.revokedAt ? t('revoked') : t('active')}</span>
                </td>
                <td>{k.revokedAt ? <button onClick={() => void remove(k)}>{t('Delete')}</button> : <button onClick={() => void revoke(k)}>{t('Revoke')}</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="form-cols" style={{ alignItems: 'end' }}>
        <label>
          {t('Name')}
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('for example: my script')} {...v.bind('name', name, setName)} />
          <FieldError v={v} name="name" />
        </label>
        <div>
          <label className="check-row" style={{ marginBottom: 8 }}>
            <input type="checkbox" checked={readOnly} onChange={(e) => setReadOnly(e.target.checked)} />
            <span>
              {t('Read-only')} <small style={{ color: 'var(--text-dim)' }}>{t('(can look, never change; good for a dashboard)')}</small>
            </span>
          </label>
          <button className="primary" onClick={create} disabled={busy}>
            {t('Create key')}
          </button>
          <FormProblem v={v} action="key" />
        </div>
      </div>
    </fieldset>
  )
}

// ---- Accounts (administrators only) ---------------------------------------

const accountName = (a: Account) => a.name.trim() || a.username

// When someone last signed in, in plain words.
function lastSignIn(iso: string | null): string {
  const then = iso ? new Date(iso).getTime() : NaN
  if (Number.isNaN(then)) return t('Never signed in')
  const minutes = Math.max(0, Math.round((Date.now() - then) / 60000))
  if (minutes < 2) return t('Signed in just now')
  if (minutes < 60) return plural(minutes, 'Signed in {n} minute ago', 'Signed in {n} minutes ago')
  const hours = Math.round(minutes / 60)
  if (hours < 24) return plural(hours, 'Signed in {n} hour ago', 'Signed in {n} hours ago')
  const days = Math.round(hours / 24)
  if (days === 1) return t('Signed in yesterday')
  if (days < 14) return plural(days, 'Signed in {n} day ago', 'Signed in {n} days ago')
  if (days < 60) return plural(Math.round(days / 7), 'Signed in {n} week ago', 'Signed in {n} weeks ago')
  return t('Last signed in on {date}', { date: new Date(then).toLocaleDateString(locale(), { day: 'numeric', month: 'long', year: 'numeric' }) })
}

const ROLES: { id: Role; title: string; blurb: string }[] = [
  { id: 'member', title: t('Basic user'), blurb: t("Can find, add and download movies and shows, but can't change settings.") },
  { id: 'viewer', title: t('Viewer'), blurb: t('Can look around and watch, but never change anything.') },
  { id: 'admin', title: t('Admin'), blurb: t('Full control, including settings and accounts.') },
]

const roleName = (r: Role) => (r === 'admin' ? t('Admin') : r === 'viewer' ? t('Viewer') : t('Basic user'))
const isExternal = (a: { source?: string }) => !!a.source && a.source !== 'local'
const sourceName = (s?: string) => (s === 'oidc' ? t('Single sign-on') : s === 'ldap' ? t('Directory') : '')

function RoleChoice({ value, onChange, disabled }: { value: Role; onChange: (r: Role) => void; disabled?: boolean }) {
  return (
    <div className="choice-grid role-choice" role="radiogroup" aria-label={t('Role')}>
      {ROLES.map((r) => (
        <button key={r.id} type="button" role="radio" aria-checked={value === r.id} className={`choice-card${value === r.id ? ' active' : ''}`} disabled={disabled} onClick={() => onChange(r.id)}>
          <span className="choice-dot" aria-hidden="true" />
          <strong>{r.title}</strong>
          <small>{r.blurb}</small>
        </button>
      ))}
    </div>
  )
}

// A password box with a button to show what was typed, handy when you are
// choosing a password to hand to someone else.
function PasswordInput({ value, onChange, placeholder, field }: { value: string; onChange: (v: string) => void; placeholder?: string; field?: FieldProps }) {
  const [shown, setShown] = useState(false)
  return (
    <span className="pw-field">
      <input type={shown ? 'text' : 'password'} value={value} onChange={(e) => onChange(e.target.value)} autoComplete="new-password" placeholder={placeholder} {...field} />
      <button type="button" className="icon-btn" onClick={() => setShown((s) => !s)} aria-label={shown ? t('Hide password') : t('Show password')} title={shown ? t('Hide password') : t('Show password')} aria-pressed={shown}>
        <Icon name="eye" size={16} />
      </button>
    </span>
  )
}

function AccountCard({ a, self, onEdit, onRemove, onTwoStepOff }: { a: Account; self: boolean; onEdit: () => void; onRemove: () => void; onTwoStepOff: () => void }) {
  const name = accountName(a)
  return (
    <div className={`account-card role-${a.role}`}>
      <div className="account-top">
        <span className="account-avatar" aria-hidden="true">
          {name.charAt(0).toUpperCase() || '?'}
        </span>
        <div className="account-who">
          <strong title={name}>{name}</strong>
          <small title={`@${a.username}`}>@{a.username}</small>
        </div>
        <span className="account-badges">
          {self && <span className="badge you-badge">{t('You')}</span>}
          <span className={`badge role-badge ${a.role}`}>{roleName(a.role)}</span>
          {a.disabled && (
            <span className="badge missing" title={t("Turned off: can't sign in")}>
              {t('Off')}
            </span>
          )}
          {isExternal(a) && (
            <span className="badge" title={t('Signs in through {where}, with no password here', { where: sourceName(a.source) })}>
              {sourceName(a.source)}
            </span>
          )}
          {a.twoStep && (
            <span className="badge" title={t('Signs in with a code from an authenticator app as well as the password')}>
              {t('Two-step')}
            </span>
          )}
        </span>
      </div>
      <ul className="account-meta">
        <li title={a.email || undefined}>
          <Icon name="mail" size={14} /> <span>{a.email || t('No email address')}</span>
        </li>
        <li title={a.lastLoginAt ? new Date(a.lastLoginAt).toLocaleString(locale()) : undefined}>
          <Icon name="clock" size={14} /> <span>{lastSignIn(a.lastLoginAt)}</span>
        </li>
      </ul>
      <div className="account-actions">
        <button className="btn-sm btn-with-icon" onClick={onEdit}>
          <Icon name="sliders" size={14} /> {t('Edit')}
        </button>
        {a.twoStep && !self && (
          <button className="btn-sm btn-with-icon" onClick={onTwoStepOff} title={t('For someone who lost their phone and their recovery codes')}>
            <Icon name="shield" size={14} /> {t('Two-step off')}
          </button>
        )}
        <button className="btn-sm btn-with-icon danger-ghost" onClick={onRemove} disabled={self} title={self ? t("You can't remove your own account.") : undefined}>
          <Icon name="trash" size={14} /> {t('Remove')}
        </button>
      </div>
    </div>
  )
}

function EditAccountDialog({ a, self, onClose, onSaved }: { a: Account; self: boolean; onClose: () => void; onSaved: (a: Account) => void }) {
  const [name, setName] = useState(a.name)
  const [email, setEmail] = useState(a.email)
  const [role, setRole] = useState<Role>(a.role)
  const [off, setOff] = useState(!!a.disabled)
  const [perms, setPerms] = useState<Permissions>(a.permissions ?? DEFAULT_PERMISSIONS)
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const who = accountName(a)
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  // Only what was changed is checked, so an older address that is not quite
  // right does not stop the other changes from being saved.
  const v = useValidation({
    name: check.maxLength(name, 100, t('The name')),
    email: email.trim() !== a.email ? check.email(email) : null,
    password: check.password(password),
  })

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function save() {
    if (!v.attempt()) return
    const changes: AccountChanges = {}
    if (name.trim() !== a.name) changes.name = name.trim()
    if (email.trim() !== a.email) changes.email = email.trim()
    if (role !== a.role) changes.role = role
    if (off !== !!a.disabled) changes.disabled = off
    if (role === 'member' && JSON.stringify(perms) !== JSON.stringify(a.permissions ?? DEFAULT_PERMISSIONS)) changes.permissions = perms
    if (password) changes.password = password
    if (Object.keys(changes).length === 0) {
      onClose()
      return
    }
    setBusy(true)
    setError('')
    try {
      onSaved(await api.updateAccountById(a.id, changes))
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal account-dialog" role="dialog" aria-modal="true" aria-label={t('Edit {name}', { name: who })}>
        <div className="modal-head">
          <span className="account-avatar" aria-hidden="true">
            {who.charAt(0).toUpperCase() || '?'}
          </span>
          <div>
            <h2 style={{ margin: 0 }}>{t('Edit {name}', { name: who })}</h2>
            <small style={{ color: 'var(--text-dim)' }}>{t('Signs in as @{username}', { username: a.username })}</small>
          </div>
        </div>
        <div className="modal-body account-form">
          <div className="form-cols">
            <label>
              {t('Name')}
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder={a.username} {...v.bind('name', name, setName)} />
              <FieldError v={v} name="name" />
            </label>
            <label>
              <span>
                {t('Email')} <small style={{ color: 'var(--text-dim)' }}>{t('(optional)')}</small>
              </span>
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} {...v.bind('email', email, setEmail)} />
              <FieldError v={v} name="email" />
            </label>
          </div>
          <div>
            <span className="field-title">{t('Role')}</span>
            <RoleChoice value={role} onChange={setRole} disabled={self} />
            {role === 'member' && <PermissionsChoice value={perms} onChange={setPerms} />}
            {self && <p className="field-note">{t("You can't remove your own admin role. Ask another admin to do it.")}</p>}
          </div>
          <label className="check-row">
            <input type="checkbox" checked={off} onChange={(e) => setOff(e.target.checked)} disabled={self} />
            <span>
              {t('Turn this account off')} <small style={{ color: 'var(--text-dim)' }}>{t("(it can't sign in and its keys stop working; nothing is deleted)")}</small>
            </span>
          </label>
          {isExternal(a) ? (
            <p className="field-note">{t('This account signs in through {where}, so it has no password here. Its name and email are updated each time it signs in.', { where: sourceName(a.source) })}</p>
          ) : self ? (
            <p className="field-note">{t('To change your own password, use Change password on this page.')}</p>
          ) : (
            <label className="account-pw">
              <span>
                {t('New password')} <small style={{ color: 'var(--text-dim)' }}>{t('(leave empty to keep the current one)')}</small>
              </span>
              <PasswordInput value={password} onChange={setPassword} field={v.bind('password')} />
              <FieldError v={v} name="password" />
              <PasswordStrength password={password} />
              {password && <span className="field-note">{t('Saving a new password signs {name} out everywhere. They sign in again with the new one.', { name: who })}</span>}
            </label>
          )}
          {error && <p className="error-text">{error}</p>}
          <FormProblem v={v} action="save" />
        </div>
        <div className="modal-foot">
          <button onClick={onClose}>{t('Cancel')}</button>
          <button className="primary" onClick={() => void save()} disabled={busy}>
            {busy ? t('Saving…') : t('Save changes')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

function AddAccountForm({ onAdded }: { onAdded: (a: Account) => void }) {
  const toast = useToast()
  const [username, setUsername] = useState('')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('member')
  const [perms, setPerms] = useState<Permissions>(DEFAULT_PERMISSIONS)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const v = useValidation({
    username: check.firstError(check.required(username, t('Choose a username for them to sign in with, at least 3 characters.')), check.username(username)),
    name: check.maxLength(name, 100, t('The name')),
    email: check.email(email),
    password: check.firstError(check.required(password, t('Choose a password of at least 8 characters for them.')), check.password(password)),
  })

  async function add() {
    if (!v.attempt()) return
    setBusy(true)
    setError('')
    try {
      const created = await api.createAccount({ username: username.trim(), password, name: name.trim() || undefined, email: email.trim() || undefined, role, permissions: role === 'member' ? perms : undefined })
      onAdded(created)
      toast.success(t('Account added for {name}. Tell them their username and password.', { name: accountName(created) }))
      setUsername('')
      setName('')
      setEmail('')
      setPassword('')
      setRole('member')
      v.reset()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group usenet">
      <legend>
        <Icon name="plus" size={14} /> <TipLabel help="accounts#adding-a-family-account">{t('Add an account')}</TipLabel>
      </legend>
      <div className="account-form">
        <div className="triple-cols">
          <label>
            <span>
              {t('Username')} <small style={{ color: 'var(--text-dim)' }}>{t('(what they sign in with)')}</small>
            </span>
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" placeholder="mediarium" {...v.bind('username', username, setUsername)} />
            <FieldError v={v} name="username" />
          </label>
          <label>
            <span>
              {t('Name')} <small style={{ color: 'var(--text-dim)' }}>{t('(optional)')}</small>
            </span>
            <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" placeholder="Mediarium Admin" {...v.bind('name', name, setName)} />
            <FieldError v={v} name="name" />
          </label>
          <label>
            <span>
              {t('Email')} <small style={{ color: 'var(--text-dim)' }}>{t('(optional)')}</small>
            </span>
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" {...v.bind('email', email, setEmail)} />
            <FieldError v={v} name="email" />
          </label>
        </div>
        <div className="account-add-row">
          <label className="account-pw">
            <span>
              {t('Password')} <small style={{ color: 'var(--text-dim)' }}>{t('(at least 8 characters)')}</small>
            </span>
            <PasswordInput value={password} onChange={setPassword} field={v.bind('password')} />
            <FieldError v={v} name="password" />
            <PasswordStrength password={password} />
            <span className="field-note">{t('They can change it from their profile once signed in.')}</span>
          </label>
          <div>
            <span className="field-title">{t('What can they do?')}</span>
            <RoleChoice value={role} onChange={setRole} />
            {role === 'member' && <PermissionsChoice value={perms} onChange={setPerms} />}
          </div>
        </div>
        {error && <p className="error-text">{error}</p>}
        <div>
          <button className="primary btn-with-icon" onClick={() => void add()} disabled={busy}>
            <Icon name="plus" size={16} /> {busy ? t('Adding…') : t('Add account')}
          </button>
          <FormProblem v={v} action="account" />
        </div>
      </div>
    </fieldset>
  )
}

function Accounts() {
  const { user, setUser } = useAuth()
  const toast = useToast()
  const ask = useConfirm()
  // For someone who lost their phone and their recovery codes.
  async function twoStepOff(a: Account) {
    const yes = await ask({
      title: t('Switch off two-step sign-in for {name}?', { name: accountName(a) }),
      body: t('They can then sign in with their password alone, and set two-step sign-in up again from their profile.'),
      confirmLabel: t('Switch it off'),
    })
    if (!yes) return
    try {
      await api.userTwoStepOff(a.id)
      setAccounts((list) => list?.map((x) => (x.id === a.id ? { ...x, twoStep: false } : x)) ?? null)
      toast.success(t('Two-step sign-in is off for {name}.', { name: accountName(a) }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const [accounts, setAccounts] = useState<Account[] | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<Account | null>(null)
  const [removing, setRemoving] = useState<Account | null>(null)

  const load = useCallback(() => {
    api
      .listAccounts()
      .then((list) => {
        setAccounts(list)
        setError('')
      })
      .catch((e) => setError(errorText(e)))
  }, [])
  useEffect(load, [load])

  function saved(a: Account) {
    setEditing(null)
    setAccounts((list) => (list ?? []).map((x) => (x.id === a.id ? a : x)))
    if (user && a.id === user.id) setUser({ ...user, name: a.name, email: a.email, role: a.role, isAdmin: a.isAdmin })
    toast.success(t('Saved the changes to {name}.', { name: accountName(a) }))
  }

  async function remove(a: Account) {
    setRemoving(null)
    try {
      await api.deleteAccount(a.id)
      setAccounts((list) => (list ?? []).filter((x) => x.id !== a.id))
      toast.success(t('Removed the account for {name}.', { name: accountName(a) }))
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  const admins = accounts?.filter((a) => a.role === 'admin').length ?? 0
  const viewers = accounts?.filter((a) => a.role === 'viewer').length ?? 0

  return (
    <>
      <fieldset className="group folders">
        <legend>
          <Icon name="user" size={14} /> {t('Accounts')}
          {accounts && <span className="legend-count">{accounts.length}</span>}
        </legend>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          {t('Everyone who can sign in. Basic users can find, add and download movies and shows. Admins can also change settings and manage accounts.')}
          {accounts && ' ' + t('{admins}, {members}.', { admins: plural(admins, '{n} admin', '{n} admins'), members: plural(accounts.length - admins - viewers, '{n} basic user', '{n} basic users') })}
          {viewers > 0 && ' ' + plural(viewers, '{n} viewer.', '{n} viewers.')}
        </p>
        {error && <p className="error-text">{error}</p>}
        <div className="account-grid">
          {accounts === null && !error && Array.from({ length: 3 }, (_, i) => <div key={i} className="skeleton" style={{ height: 150, borderRadius: 14 }} />)}
          {accounts?.map((a) => (
            <AccountCard key={a.id} a={a} self={a.id === user?.id} onEdit={() => setEditing(a)} onRemove={() => setRemoving(a)} onTwoStepOff={() => void twoStepOff(a)} />
          ))}
        </div>
      </fieldset>
      <AddAccountForm onAdded={(a) => setAccounts((list) => [...(list ?? []), a])} />
      {editing && <EditAccountDialog a={editing} self={editing.id === user?.id} onClose={() => setEditing(null)} onSaved={saved} />}
      {removing && (
        <ConfirmDialog title={t('Remove {name}?', { name: accountName(removing) })} confirmLabel={t('Remove account')} onConfirm={() => void remove(removing)} onCancel={() => setRemoving(null)}>
          <p style={{ marginTop: 0 }}>{t('{name} is signed out straight away and can no longer sign in. Any API keys they made stop working.', { name: accountName(removing) })}</p>
          <p style={{ marginBottom: 0 }}>{t('Movies and shows they added stay in your library. This cannot be undone, but you can add a new account for them later.')}</p>
        </ConfirmDialog>
      )}
    </>
  )
}

// Settings > People and access > Your profile: your name, password and request messages.
export default function Profile() {
  const { user } = useAuth()
  const location = useLocation()
  // The single sign-on link comes back to the profile; its card is on the security page.
  if (new URLSearchParams(location.search).has('linked')) return <Navigate to={`/settings/security${location.search}`} replace />
  const admin = isAdmin(user)
  const local = !user?.source || user.source === 'local'
  const viewer = user?.role === 'viewer'
  return (
    <div>
      <div className="page-header">
        <h1>{t('Your profile')}</h1>
        <span style={{ color: 'var(--text-dim)' }}>
          {t('Signed in as {name}', { name: displayName(user) })}
          {user && ` · ${roleName(admin ? 'admin' : viewer ? 'viewer' : 'member')}`}
        </span>
      </div>
      <div className="half-cols profile-cols">
        <ProfileForm />
        {local && <PasswordForm />}
      </div>
      {!local && (
        <p className="hint">
          {t('You sign in through {where}, so there is no password to change here. Your name and email are updated each time you sign in.', { where: sourceName(user?.source) })}
        </p>
      )}
      {!viewer && <RequestNoticesCard />}
    </div>
  )
}

// Settings > People and access > Security: how you sign in and the keys that act as you.
export function SecuritySettings() {
  const { user } = useAuth()
  const local = !user?.source || user.source === 'local'
  const viewer = user?.role === 'viewer'
  return (
    <div className="settings-stack">
      <PasskeysCard />
      <SsoLinkCard />
      {local && <TwoStepCard />}
      <SessionsCard />
      {isAdmin(user) && <IdleSignOutCard />}
      {!viewer && <APIKeys />}
    </div>
  )
}

// Settings > People and access > Linked services: the accounts and devices you connect to yours.
export function ServicesSettings() {
  const { user } = useAuth()
  if (user?.role === 'viewer') return <p className="hint">{t('View-only accounts have nothing to link here.')}</p>
  return (
    <div className="settings-stack">
      <PlexWatchlistCard />
      <ListenBrainzCard />
      <EReaderCard />
    </div>
  )
}

// Everyone with an account and what each may do (administrators).
export function AllAccounts() {
  return (
    <div>
      <div className="page-header">
        <h1>{t('All accounts')}</h1>
      </div>
      <Accounts />
    </div>
  )
}

// Settings > People and access > Requests and reports (administrators).
export function RequestsSettings() {
  return (
    <div className="settings-stack">
      <RequestSettingsCard />
    </div>
  )
}

// Settings > People and access > Ways to sign in: password, passkeys, single sign-on, a
// directory and a login proxy (administrators).
export function SignInSettings() {
  return (
    <div className="settings-stack">
      <SignInMethods />
      <ProxySignIn />
    </div>
  )
}
