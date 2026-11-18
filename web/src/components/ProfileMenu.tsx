import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, displayName, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { demoEnabled, setDemo } from '../demoFlag'
import { DOCS_URL, FEEDBACK_URL, REDDIT_URL, REPO_URL, WEBSITE_URL } from '../docs'
import { LANGUAGES, chooseLanguage, currentLanguage, t } from '../i18n'
import { openReport } from '../report'
import { QUEUE_STORAGE_KEY } from '../musicQueue'
import { SKIP_ONCE_KEY } from '../autoSignIn'
import Icon from './Icon'
import ShareDialog from './ShareDialog'
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
  const [sharing, setSharing] = useState(false)
  const [langOpen, setLangOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const demo = demoEnabled()

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    setLangOpen(false)
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
      // If the sign-in page sends people to single sign-on by itself, it
      // should not do that right after they chose to sign out.
      try {
        sessionStorage.setItem(SKIP_ONCE_KEY, '1')
      } catch {
        // storage blocked: the sign-in page may redirect once
      }
      setUser(null)
      // The next person on this browser shouldn't find this one's music queue.
      try {
        localStorage.removeItem(QUEUE_STORAGE_KEY)
      } catch {
        // storage blocked: nothing was kept anyway
      }
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
            <Icon name="user" size={16} /> {t('Your profile')}
          </Link>
          <button role="menuitem" aria-haspopup="true" aria-expanded={langOpen} onClick={() => setLangOpen((o) => !o)}>
            <Icon name="globe" size={16} /> {t('App language')} <small className="menu-current">{LANGUAGES.find((l) => l.code === currentLanguage())?.name}</small>
          </button>
          {langOpen && (
            <div className="menu-sub" role="group" aria-label={t('App language')}>
              {LANGUAGES.map((l) => (
                <button key={l.code} role="menuitemradio" aria-checked={l.code === currentLanguage()} lang={l.code} onClick={() => (l.code === currentLanguage() ? setOpen(false) : chooseLanguage(l.code))}>
                  <span className="menu-tick">{l.code === currentLanguage() ? <Icon name="check" size={14} /> : null}</span> {l.name}
                </button>
              ))}
            </div>
          )}
          {admin && (
            <Link to="/settings" role="menuitem" onClick={() => setOpen(false)}>
              <Icon name="sliders" size={16} /> {t('Settings')}
            </Link>
          )}
          <div className="menu-sep" role="separator" />
          <a href={WEBSITE_URL} target="_blank" rel="noreferrer" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="globe" size={16} /> {t('Website')}
          </a>
          <Link to="/help" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="book" size={16} /> {t('User guide')}
          </Link>
          <a href={DOCS_URL} target="_blank" rel="noreferrer" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="external" size={16} /> {t('Documentation')}
          </a>
          <a href={REPO_URL} target="_blank" rel="noreferrer" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="star" size={16} /> {t('Star on GitHub')}
          </a>
          <button
            role="menuitem"
            onClick={() => {
              setOpen(false)
              openReport()
            }}
          >
            <Icon name="bug" size={16} /> {t('Report a problem or share an idea')}
          </button>
          <a href={FEEDBACK_URL} target="_blank" rel="noreferrer" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="chat" size={16} /> {t('Questions and discussions')}
          </a>
          <a href={REDDIT_URL} target="_blank" rel="noreferrer" role="menuitem" onClick={() => setOpen(false)}>
            <Icon name="chat" size={16} /> {t('Reddit community')}
          </a>
          <button
            role="menuitem"
            onClick={() => {
              setOpen(false)
              setSharing(true)
            }}
          >
            <Icon name="share" size={16} /> {t('Share Mediarium')}
          </button>
          <div className="menu-sep" role="separator" />
          {admin && (
            <button role="menuitem" onClick={() => setDemo(!demo)} title={t('Made-up titles for screenshots and trying the app out. Only in this browser; nothing is saved to your library.')}>
              <Icon name="flask" size={16} /> {demo ? t('Turn off demo data') : t('Show demo data')}
            </button>
          )}
          <button role="menuitem" className="danger" onClick={() => void signOut()}>
            <Icon name="logout" size={16} /> {t('Sign out')}
          </button>
        </div>
      )}
      {sharing && <ShareDialog onClose={() => setSharing(false)} />}
    </div>
  )
}
