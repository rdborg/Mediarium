import { createPortal } from 'react-dom'
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, DEFAULT_PERMISSIONS, displayName, isAdmin, type Account, type AccountChanges, type APIKey, type Permissions, type Role } from '../api'
import PermissionsChoice from '../components/PermissionsChoice'
import ProxySignIn from '../components/ProxySignIn'
import { useAuth } from '../AuthContext'
import ConfirmDialog from '../components/ConfirmDialog'
import { useConfirm } from '../components/ConfirmProvider'
import PasswordStrength from '../components/PasswordStrength'
import { useToast } from '../components/Toast'
import Icon from '../components/Icon'
import * as check from '../validate'
import { FieldError, FormProblem, useValidation, type FieldProps } from '../useValidation'
import { useFocusTrap } from '../useFocusTrap'

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
    name: check.maxLength(name, 100, 'Your name'),
    username: check.firstError(
      check.required(username, 'Enter the username you sign in with.'),
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
      toast.success('Profile saved.')
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="user" size={14} /> Your details
      </legend>
      <div className="form-cols">
        <label>
          Name <small style={{ color: 'var(--text-dim)' }}>(shown in your greeting)</small>
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" placeholder="Leave empty to use your username" {...v.bind('name', name, setName)} />
          <FieldError v={v} name="name" />
        </label>
        <label>
          Username <small style={{ color: 'var(--text-dim)' }}>(what you sign in with)</small>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" {...v.bind('username', username, setUsername)} />
          <FieldError v={v} name="username" />
        </label>
        <label>
          Email <small style={{ color: 'var(--text-dim)' }}>(used for email notifications)</small>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" {...v.bind('email', email, setEmail)} />
          <FieldError v={v} name="email" />
        </label>
      </div>
      <div style={{ marginTop: 14 }}>
        <button className="primary" onClick={save} disabled={busy}>
          Save details
        </button>
        <FormProblem v={v} verb="save" />
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
    currentPassword: check.required(currentPassword, 'Enter your current password.'),
    newPassword: check.firstError(check.required(newPassword, 'Choose a new password of at least 8 characters.'), check.password(newPassword)),
    confirmPassword: check.firstError(check.required(confirmPassword, 'Type the new password again to make sure it is right.'), check.passwordsMatch(newPassword, confirmPassword)),
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
      toast.success('Password changed.')
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group alerts">
      <legend>
        <Icon name="key" size={14} /> Change password
      </legend>
      <div className="grid-form">
        <label>
          Current password
          <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} autoComplete="current-password" {...v.bind('currentPassword')} />
          <FieldError v={v} name="currentPassword" />
        </label>
        <div className="form-cols">
          <label>
            New password
            <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} autoComplete="new-password" {...v.bind('newPassword')} />
            <FieldError v={v} name="newPassword" />
          </label>
          <label>
            Confirm new password
            <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} autoComplete="new-password" {...v.bind('confirmPassword')} />
            <FieldError v={v} name="confirmPassword" />
          </label>
        </div>
        <PasswordStrength password={newPassword} />
        <div>
          <button className="primary" onClick={submit} disabled={busy}>
            Change password
          </button>
          <FormProblem v={v} verb="change your password" />
        </div>
      </div>
    </fieldset>
  )
}

function APIKeys() {
  const [keys, setKeys] = useState<APIKey[] | null>(null)
  const [name, setName] = useState('')
  const [newKey, setNewKey] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const toast = useToast()
  const confirm = useConfirm()
  const v = useValidation({
    name: check.firstError(check.required(name, 'Give the key a name so you can tell it apart later, for example "my script".'), check.maxLength(name, 100, 'The name')),
  })

  function reload() {
    api.listAPIKeys().then(setKeys).catch(() => undefined)
  }
  useEffect(reload, [])

  async function create() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      const created = await api.createAPIKey(name.trim())
      setNewKey(created.key ?? null)
      setName('')
      v.reset()
      reload()
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function revoke(k: APIKey) {
    if (!(await confirm({ title: `Revoke "${k.name}"?`, body: <p>Anything using this key stops working straight away. This cannot be undone.</p>, confirmLabel: 'Revoke key', danger: true }))) return
    try {
      await api.revokeAPIKey(k.id)
      toast.success('Key revoked.')
      reload()
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  async function remove(k: APIKey) {
    if (!(await confirm({ title: `Delete "${k.name}"?`, body: <p>It is already revoked and does nothing. This takes it off the list for good.</p>, confirmLabel: 'Delete key', danger: true }))) return
    try {
      await api.deleteAPIKey(k.id)
      toast.success('Key deleted.')
      reload()
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  return (
    <fieldset className="group torrent">
      <legend>
        <Icon name="server" size={14} /> API keys
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        For scripts and other apps: send a key as an <code>X-API-Key</code> header instead of signing in. A key is shown only once, when you create it.
      </p>
      {newKey && (
        <p className="newkey">
          {newKey}
          <br />
          <span style={{ color: 'var(--warning)', fontFamily: 'var(--font-body)' }}>Copy it now. You won&apos;t see it again.</span>
        </p>
      )}
      {keys && keys.length > 0 && (
        <table style={{ marginBottom: 16 }}>
          <thead>
            <tr>
              <th>Name</th>
              <th>Created</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id}>
                <td>{k.name}</td>
                <td>{new Date(k.createdAt).toLocaleDateString()}</td>
                <td>
                  <span className={`badge ${k.revokedAt ? 'missing' : 'downloaded'}`}>{k.revokedAt ? 'revoked' : 'active'}</span>
                </td>
                <td>{k.revokedAt ? <button onClick={() => void remove(k)}>Delete</button> : <button onClick={() => void revoke(k)}>Revoke</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="form-cols" style={{ alignItems: 'end' }}>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="for example: my script" {...v.bind('name', name, setName)} />
          <FieldError v={v} name="name" />
        </label>
        <div>
          <button className="primary" onClick={create} disabled={busy}>
            Create key
          </button>
          <FormProblem v={v} verb="create the key" />
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
  if (Number.isNaN(then)) return 'Never signed in'
  const minutes = Math.max(0, Math.round((Date.now() - then) / 60000))
  if (minutes < 2) return 'Signed in just now'
  if (minutes < 60) return `Signed in ${minutes} minutes ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `Signed in ${hours} ${hours === 1 ? 'hour' : 'hours'} ago`
  const days = Math.round(hours / 24)
  if (days === 1) return 'Signed in yesterday'
  if (days < 14) return `Signed in ${days} days ago`
  if (days < 60) return `Signed in ${Math.round(days / 7)} weeks ago`
  return `Last signed in on ${new Date(then).toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' })}`
}

const ROLES: { id: Role; title: string; blurb: string }[] = [
  { id: 'member', title: 'Basic user', blurb: "Can find, add and download movies and shows, but can't change settings." },
  { id: 'admin', title: 'Admin', blurb: 'Full control, including settings and accounts.' },
]

function RoleChoice({ value, onChange, disabled }: { value: Role; onChange: (r: Role) => void; disabled?: boolean }) {
  return (
    <div className="choice-grid role-choice" role="radiogroup" aria-label="Role">
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
      <button type="button" className="icon-btn" onClick={() => setShown((s) => !s)} aria-label={shown ? 'Hide password' : 'Show password'} title={shown ? 'Hide password' : 'Show password'} aria-pressed={shown}>
        <Icon name="eye" size={16} />
      </button>
    </span>
  )
}

function AccountCard({ a, self, onEdit, onRemove }: { a: Account; self: boolean; onEdit: () => void; onRemove: () => void }) {
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
          {self && <span className="badge you-badge">You</span>}
          <span className={`badge role-badge ${a.role}`}>{a.role === 'admin' ? 'Admin' : 'Basic user'}</span>
        </span>
      </div>
      <ul className="account-meta">
        <li title={a.email || undefined}>
          <Icon name="mail" size={14} /> <span>{a.email || 'No email address'}</span>
        </li>
        <li title={a.lastLoginAt ? new Date(a.lastLoginAt).toLocaleString() : undefined}>
          <Icon name="clock" size={14} /> <span>{lastSignIn(a.lastLoginAt)}</span>
        </li>
      </ul>
      <div className="account-actions">
        <button className="btn-sm btn-with-icon" onClick={onEdit}>
          <Icon name="sliders" size={14} /> Edit
        </button>
        <button className="btn-sm btn-with-icon danger-ghost" onClick={onRemove} disabled={self} title={self ? "You can't remove your own account." : undefined}>
          <Icon name="trash" size={14} /> Remove
        </button>
      </div>
    </div>
  )
}

function EditAccountDialog({ a, self, onClose, onSaved }: { a: Account; self: boolean; onClose: () => void; onSaved: (a: Account) => void }) {
  const [name, setName] = useState(a.name)
  const [email, setEmail] = useState(a.email)
  const [role, setRole] = useState<Role>(a.role)
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
    name: check.maxLength(name, 100, 'The name'),
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
      <div ref={box} tabIndex={-1} className="modal account-dialog" role="dialog" aria-modal="true" aria-label={`Edit ${who}`}>
        <div className="modal-head">
          <span className="account-avatar" aria-hidden="true">
            {who.charAt(0).toUpperCase() || '?'}
          </span>
          <div>
            <h2 style={{ margin: 0 }}>Edit {who}</h2>
            <small style={{ color: 'var(--text-dim)' }}>Signs in as @{a.username}</small>
          </div>
        </div>
        <div className="modal-body account-form">
          <div className="form-cols">
            <label>
              Name
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder={a.username} {...v.bind('name', name, setName)} />
              <FieldError v={v} name="name" />
            </label>
            <label>
              <span>
                Email <small style={{ color: 'var(--text-dim)' }}>(optional)</small>
              </span>
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} {...v.bind('email', email, setEmail)} />
              <FieldError v={v} name="email" />
            </label>
          </div>
          <div>
            <span className="field-title">Role</span>
            <RoleChoice value={role} onChange={setRole} disabled={self} />
            {role === 'member' && <PermissionsChoice value={perms} onChange={setPerms} />}
            {self && <p className="field-note">You can&apos;t remove your own admin role. Ask another admin to do it.</p>}
          </div>
          {self ? (
            <p className="field-note">To change your own password, use Change password on this page.</p>
          ) : (
            <label className="account-pw">
              <span>
                New password <small style={{ color: 'var(--text-dim)' }}>(leave empty to keep the current one)</small>
              </span>
              <PasswordInput value={password} onChange={setPassword} field={v.bind('password')} />
              <FieldError v={v} name="password" />
              <PasswordStrength password={password} />
              {password && <span className="field-note">Saving a new password signs {who} out everywhere. They sign in again with the new one.</span>}
            </label>
          )}
          {error && <p className="error-text">{error}</p>}
          <FormProblem v={v} verb="save" />
        </div>
        <div className="modal-foot">
          <button onClick={onClose}>Cancel</button>
          <button className="primary" onClick={() => void save()} disabled={busy}>
            {busy ? 'Saving…' : 'Save changes'}
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
    username: check.firstError(check.required(username, 'Choose a username for them to sign in with, at least 3 characters.'), check.username(username)),
    name: check.maxLength(name, 100, 'The name'),
    email: check.email(email),
    password: check.firstError(check.required(password, 'Choose a password of at least 8 characters for them.'), check.password(password)),
  })

  async function add() {
    if (!v.attempt()) return
    setBusy(true)
    setError('')
    try {
      const created = await api.createAccount({ username: username.trim(), password, name: name.trim() || undefined, email: email.trim() || undefined, role, permissions: role === 'member' ? perms : undefined })
      onAdded(created)
      toast.success(`Account added for ${accountName(created)}. Tell them their username and password.`)
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
        <Icon name="plus" size={14} /> Add an account
      </legend>
      <div className="account-form">
        <div className="triple-cols">
          <label>
            <span>
              Username <small style={{ color: 'var(--text-dim)' }}>(what they sign in with)</small>
            </span>
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" placeholder="mediarium" {...v.bind('username', username, setUsername)} />
            <FieldError v={v} name="username" />
          </label>
          <label>
            <span>
              Name <small style={{ color: 'var(--text-dim)' }}>(optional)</small>
            </span>
            <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" placeholder="Mediarium Admin" {...v.bind('name', name, setName)} />
            <FieldError v={v} name="name" />
          </label>
          <label>
            <span>
              Email <small style={{ color: 'var(--text-dim)' }}>(optional)</small>
            </span>
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" {...v.bind('email', email, setEmail)} />
            <FieldError v={v} name="email" />
          </label>
        </div>
        <div className="account-add-row">
          <label className="account-pw">
            <span>
              Password <small style={{ color: 'var(--text-dim)' }}>(at least 8 characters)</small>
            </span>
            <PasswordInput value={password} onChange={setPassword} field={v.bind('password')} />
            <FieldError v={v} name="password" />
            <PasswordStrength password={password} />
            <span className="field-note">They can change it from their profile once signed in.</span>
          </label>
          <div>
            <span className="field-title">What can they do?</span>
            <RoleChoice value={role} onChange={setRole} />
            {role === 'member' && <PermissionsChoice value={perms} onChange={setPerms} />}
          </div>
        </div>
        {error && <p className="error-text">{error}</p>}
        <div>
          <button className="primary btn-with-icon" onClick={() => void add()} disabled={busy}>
            <Icon name="plus" size={16} /> {busy ? 'Adding…' : 'Add account'}
          </button>
          <FormProblem v={v} verb="add the account" />
        </div>
      </div>
    </fieldset>
  )
}

function Accounts() {
  const { user, setUser } = useAuth()
  const toast = useToast()
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
    toast.success(`Saved the changes to ${accountName(a)}.`)
  }

  async function remove(a: Account) {
    setRemoving(null)
    try {
      await api.deleteAccount(a.id)
      setAccounts((list) => (list ?? []).filter((x) => x.id !== a.id))
      toast.success(`Removed the account for ${accountName(a)}.`)
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  const admins = accounts?.filter((a) => a.role === 'admin').length ?? 0

  return (
    <>
      <fieldset className="group folders">
        <legend>
          <Icon name="user" size={14} /> Accounts
          {accounts && <span className="legend-count">{accounts.length}</span>}
        </legend>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          Everyone who can sign in. Basic users can find, add and download movies and shows. Admins can also change settings and manage accounts.
          {accounts && ` ${admins} ${admins === 1 ? 'admin' : 'admins'}, ${accounts.length - admins} ${accounts.length - admins === 1 ? 'basic user' : 'basic users'}.`}
        </p>
        {error && <p className="error-text">{error}</p>}
        <div className="account-grid">
          {accounts === null && !error && Array.from({ length: 3 }, (_, i) => <div key={i} className="skeleton" style={{ height: 150, borderRadius: 14 }} />)}
          {accounts?.map((a) => (
            <AccountCard key={a.id} a={a} self={a.id === user?.id} onEdit={() => setEditing(a)} onRemove={() => setRemoving(a)} />
          ))}
        </div>
      </fieldset>
      <AddAccountForm onAdded={(a) => setAccounts((list) => [...(list ?? []), a])} />
      {editing && <EditAccountDialog a={editing} self={editing.id === user?.id} onClose={() => setEditing(null)} onSaved={saved} />}
      {removing && (
        <ConfirmDialog title={`Remove ${accountName(removing)}?`} confirmLabel="Remove account" onConfirm={() => void remove(removing)} onCancel={() => setRemoving(null)}>
          <p style={{ marginTop: 0 }}>
            {accountName(removing)} is signed out straight away and can no longer sign in. Any API keys they made stop working.
          </p>
          <p style={{ marginBottom: 0 }}>Movies and shows they added stay in your library. This cannot be undone, but you can add a new account for them later.</p>
        </ConfirmDialog>
      )}
    </>
  )
}

export default function Profile() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  return (
    <div>
      <div className="page-header">
        <h1>{admin ? 'Accounts' : 'Your profile'}</h1>
        <span style={{ color: 'var(--text-dim)' }}>
          Signed in as {displayName(user)}
          {user && ` · ${admin ? 'Admin' : 'Basic user'}`}
        </span>
      </div>
      <div className="half-cols profile-cols">
        <ProfileForm />
        <PasswordForm />
      </div>
      {admin && <Accounts />}
      {admin && <ProxySignIn />}
      <APIKeys />
    </div>
  )
}
