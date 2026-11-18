import { Link } from 'react-router-dom'
import { useAuth } from '../AuthContext'
import { isAdmin } from '../api'
import { t } from '../i18n'

// A page that could not load: what went wrong, then a way on. A missing TMDB key points an
// administrator to the page where it goes; anything else offers trying again and the library.
export default function LoadFailed({ message, library = '/library' }: { message: string; library?: string }) {
  const admin = isAdmin(useAuth().user)
  const needsKey = /TMDB/.test(message)
  return (
    <div className="load-failed">
      <p className="error-text">{message}</p>
      <div className="load-failed-actions">
        <button type="button" onClick={() => window.location.reload()}>
          {t('Try again')}
        </button>
        {needsKey && admin && (
          <Link className="btn-link" to="/settings/metadata">
            {t('Movie and show info')}
          </Link>
        )}
        <Link className="btn-link" to={library}>
          {t('Library')}
        </Link>
      </div>
    </div>
  )
}
