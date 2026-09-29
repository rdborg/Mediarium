import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { isAdmin } from '../../api'
import { useAuth } from '../../AuthContext'
import Icon from '../../components/Icon'
import { groupFor } from '../../settingsNav'

// Holds the open settings page. Pages that share a sidebar entry (for
// example Downloads & VPN) get tabs at the top to switch between them.
export default function SettingsLayout() {
  const { pathname } = useLocation()
  const { user } = useAuth()
  const group = groupFor(pathname, isAdmin(user))
  return (
    <div className="settings-content">
      {group && group.pages.length > 1 && (
        <nav className="page-tabs" aria-label={group.label} style={{ ['--tc' as string]: group.color }}>
          {group.pages.map((p) => (
            <NavLink key={p.to} to={p.to} className={({ isActive }) => (isActive ? 'active' : '')}>
              <Icon name={p.icon} size={15} /> {p.label}
            </NavLink>
          ))}
        </nav>
      )}
      <Outlet />
    </div>
  )
}
