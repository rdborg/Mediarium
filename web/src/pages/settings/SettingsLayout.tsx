import { Outlet } from 'react-router-dom'

// The settings pages are listed under Settings in the sidebar; this only
// holds the page that is open.
export default function SettingsLayout() {
  return (
    <div className="settings-content">
      <Outlet />
    </div>
  )
}
