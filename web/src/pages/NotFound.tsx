import { Link, useLocation } from 'react-router-dom'
import Icon from '../components/Icon'

// Shown for an address the app does not have, instead of quietly jumping to
// the dashboard.
export default function NotFound() {
  const path = useLocation().pathname
  return (
    <div className="not-found">
      <Icon name="search" size={34} />
      <h1>That page doesn&apos;t exist</h1>
      <p>
        Nothing lives at <code>{path}</code>. The link may be old, or the address has a typo.
      </p>
      <p className="not-found-links">
        <Link className="btn-link primary-look" to="/">
          Go to the dashboard
        </Link>
        <Link to="/library">Library</Link>
        <Link to="/discover">Discover</Link>
        <Link to="/settings">Settings</Link>
      </p>
    </div>
  )
}
