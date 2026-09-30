import { Link } from 'react-router-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'

// The one quiet line title pages show instead of the subtitle tools while
// subtitles are switched off. Only administrators can change the switch, so
// only they get the link.
export default function SubtitlesOffNote() {
  const admin = isAdmin(useAuth().user)
  return (
    <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: '0 0 24px' }}>
      Subtitles are off.
      {admin && (
        <>
          {' '}
          Turn them on in <Link to="/settings/subtitles">Settings</Link>.
        </>
      )}
    </p>
  )
}
