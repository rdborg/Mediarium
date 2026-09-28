import { useEffect, useState } from 'react'
import { api, displayName, type APIKey } from '../api'
import { useAuth } from '../AuthContext'
import PasswordStrength from '../components/PasswordStrength'
import { useToast } from '../components/Toast'
import Icon from '../components/Icon'

function ProfileForm() {
  const { user, setUser } = useAuth()
  const toast = useToast()
  const [name, setName] = useState(user?.name && user.name !== user.username ? user.name : '')
  const [email, setEmail] = useState(user?.email ?? '')
  const [username, setUsername] = useState(user?.username ?? '')
  const [busy, setBusy] = useState(false)

  async function save() {
    setBusy(true)
    try {
      const updated = await api.updateAccount({ username: username.trim(), name: name.trim(), email: email.trim() })
      setUser(updated)
      toast.success('Profile saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
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
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" placeholder="Leave empty to use your username" />
        </label>
        <label>
          Username <small style={{ color: 'var(--text-dim)' }}>(what you sign in with)</small>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
        </label>
        <label>
          Email <small style={{ color: 'var(--text-dim)' }}>(used for email notifications)</small>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
        </label>
      </div>
      <div style={{ marginTop: 14 }}>
        <button className="primary" onClick={save} disabled={busy}>
          Save details
        </button>
      </div>
    </fieldset>
  )
}

function PasswordForm() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const toast = useToast()

  async function submit() {
    if (newPassword.length < 8) {
      toast.error('New password must be at least 8 characters.')
      return
    }
    if (newPassword !== confirmPassword) {
      toast.error('New password and confirmation do not match.')
      return
    }
    try {
      await api.changePassword(currentPassword, newPassword)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      toast.success('Password changed.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
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
          <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} autoComplete="current-password" />
        </label>
        <div className="form-cols">
          <label>
            New password
            <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} autoComplete="new-password" />
          </label>
          <label>
            Confirm new password
            <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} autoComplete="new-password" />
          </label>
        </div>
        <PasswordStrength password={newPassword} />
        <div>
          <button className="primary" onClick={submit} disabled={!currentPassword || !newPassword}>
            Change password
          </button>
        </div>
      </div>
    </fieldset>
  )
}

function APIKeys() {
  const [keys, setKeys] = useState<APIKey[] | null>(null)
  const [name, setName] = useState('')
  const [newKey, setNewKey] = useState<string | null>(null)
  const toast = useToast()

  function reload() {
    api.listAPIKeys().then(setKeys).catch(() => undefined)
  }
  useEffect(reload, [])

  async function create() {
    try {
      const created = await api.createAPIKey(name)
      setNewKey(created.key ?? null)
      setName('')
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function revoke(id: number) {
    await api.revokeAPIKey(id)
    toast.success('Key revoked.')
    reload()
  }

  return (
    <fieldset className="group torrent">
      <legend>
        <Icon name="server" size={14} /> API keys
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        For scripts and other apps: send a key as an <code>X-API-Key</code> header instead of signing in. A key is shown in full only once, right after you create it. If you lose it, revoke it and make a new one.
      </p>
      {newKey && (
        <p className="newkey">
          {newKey}
          <br />
          <span style={{ color: 'var(--warning)', fontFamily: 'var(--font-body)' }}>Copy this now. It will not be shown again.</span>
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
                <td>{!k.revokedAt && <button onClick={() => void revoke(k.id)}>Revoke</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="form-cols" style={{ alignItems: 'end' }}>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="for example: my script" />
        </label>
        <div>
          <button className="primary" onClick={create} disabled={!name}>
            Create key
          </button>
        </div>
      </div>
    </fieldset>
  )
}

export default function Profile() {
  const { user } = useAuth()
  return (
    <div>
      <div className="page-header">
        <h1>Profile</h1>
        <span style={{ color: 'var(--text-dim)' }}>Signed in as {displayName(user)}</span>
      </div>
      <div className="group-cols">
        <ProfileForm />
        <PasswordForm />
      </div>
      <APIKeys />
    </div>
  )
}
