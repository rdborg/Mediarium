import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, displayName, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { demoEnabled, setDemo } from '../demo'
import Icon from './Icon'
import { useToast } from './Toast'

function initials(name?: string, username?: string): string {
  const first = ((name ?? '').trim() || (username ?? '?').trim()).charAt(0)
  return first.toUpperCase() || '?'
}

// Avatar in the top-right corner: opens the profile, demo mode and sign out.
export default function ProfileMenu() {
  const { user, setUser } = useAuth()
  const toast = useToast()
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
  const admin = isAdmin(user)

  async function signOut() {
    try {
      await api.logout()
      setUser(null)
    } catch (e) {
      // Still signed in on the server: say so instead of showing the sign-in page.
      toast.error(e instanceof Error ? e.message : String(e))
    }
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
            <Icon name="user" size={16} /> {admin ? 'Accounts' : 'Your profile'}
          </Link>
          {admin && (
            <Link to="/settings" role="menuitem" onClick={() => setOpen(false)}>
              <Icon name="sliders" size={16} /> Settings
            </Link>
          )}
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
