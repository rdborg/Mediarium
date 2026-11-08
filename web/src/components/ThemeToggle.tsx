import { useEffect, useState } from 'react'
import { t } from '../i18n'
import Icon from './Icon'
import { applyLook } from '../look'

type Theme = 'light' | 'dark'

const STORAGE_KEY = 'mediarium-theme'

function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
}

function savedTheme(): Theme {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'light' ? 'light' : 'dark'
  } catch {
    // storage can be blocked (private windows): stay dark
    return 'dark'
  }
}

// Dark unless the person picked light; one icon button switches between them.
export default function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(savedTheme)

  useEffect(() => {
    applyTheme(theme)
    applyLook() // the browser bar colour follows light or dark
    try {
      localStorage.setItem(STORAGE_KEY, theme)
    } catch {
      // a blocked localStorage just means the choice will not survive a reload
    }
  }, [theme])

  const next: Theme = theme === 'light' ? 'dark' : 'light'
  const label = theme === 'light' ? t('Theme: light') : t('Theme: dark')

  return (
    <button className="icon-btn top-icon" onClick={() => setTheme(next)} title={t('{label}. Click to change.', { label })} aria-label={label}>
      <Icon name={theme === 'light' ? 'sun' : 'moon'} size={18} />
    </button>
  )
}
