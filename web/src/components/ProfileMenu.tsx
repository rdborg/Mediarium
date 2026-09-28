import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, displayName } from '../api'
import { useAuth } from '../AuthContext'
import { demoEnabled, setDemo } from '../demo'
import Icon from './Icon'

function initials(name?: string, username?: string): string {
  const parts = (name ?? '').trim().split(/\s+/).filter(Boolean)
  if (parts.length >= 2) return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (username ?? '?').slice(0, 2).toUpperCase()
}

// Avatar in the top-right corner: opens the profile, demo mode and sign out.
export default function ProfileMenu() {
  const { user, setUser } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const demo = demoEnabled()

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!user) return null

  async function signOut() {
    await api.logout()
    setUser(null)
  }

  return (
    <div className="profile-menu" ref={ref}>
      <button className="avatar" onClick={() => setOpen((o) => !o)} aria-haspopup="menu" aria-expanded={open} title={displayName(user)}>
        {initials(user.name, user.username)}
      </button>
      {open && (
        <div className="menu-pop" role="menu">
          <div className="menu-who">
            <strong>{displayName(user)}</strong>
            <small>{user.email || `@${user.username}`}</small>
          </div>
          <Link to="/settings/profile" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="user" size={16} /> Profile &amp; password
          </Link>
          <Link to="/settings/media" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="sliders" size={16} /> Settings
          </Link>
          <button role="menuitem" onClick={() => setDemo(!demo)}>
            <Icon name="flask" size={16} /> {demo ? 'Turn off demo data' : 'Show demo data'}
          </button>
          <button role="menuitem" className="danger" onClick={() => void signOut()}>
            <Icon name="logout" size={16} /> Sign out
          </button>
        </div>
      )}
    </div>
  )
}
