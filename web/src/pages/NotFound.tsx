import { Link, useLocation } from 'react-router-dom'
import Icon from '../components/Icon'
import { useDocumentTitle } from '../documentTitle'
import { t, tx } from '../i18n'

// Shown for an address the app does not have, instead of quietly jumping to
// the dashboard.
export default function NotFound() {
  const path = useLocation().pathname
  useDocumentTitle(t("That page doesn't exist"))
  return (
    <div className="not-found">
      <Icon name="search" size={34} />
      <h1>{t("That page doesn't exist")}</h1>
      <p>{tx('Nothing lives at {path}. The link may be old, or the address has a typo.', { path: <code>{path}</code> })}</p>
      <p className="not-found-links">
        <Link className="btn-link primary-look" to="/">
          {t('Go to the dashboard')}
        </Link>
        <Link className="btn-link" to="/library">{t('Library')}</Link>
        <Link className="btn-link" to="/discover">{t('Discover')}</Link>
        <Link className="btn-link" to="/settings">{t('Settings')}</Link>
      </p>
    </div>
  )
}
