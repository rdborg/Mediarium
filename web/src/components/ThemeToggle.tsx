import { useEffect, useState } from 'react'
import Icon from './Icon'

type Theme = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'mediarium-theme'

function applyTheme(theme: Theme) {
  if (theme === 'system') {
    delete document.documentElement.dataset.theme
  } else {
    document.documentElement.dataset.theme = theme
  }
}

// The app follows the OS colour scheme by default; this is the explicit
// override, shown as a single icon button (sun, moon or auto).
export default function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => (localStorage.getItem(STORAGE_KEY) as Theme) || 'system')

  useEffect(() => {
    applyTheme(theme)
    try {
      localStorage.setItem(STORAGE_KEY, theme)
    } catch {
      // a blocked localStorage just means the choice will not survive a reload
    }
  }, [theme])

  const next: Theme = theme === 'system' ? 'light' : theme === 'light' ? 'dark' : 'system'
  const label = theme === 'system' ? 'Theme: follows your device' : theme === 'light' ? 'Theme: light' : 'Theme: dark'

  return (
    <button className="icon-btn top-icon" onClick={() => setTheme(next)} title={`${label}. Click to change.`} aria-label={label}>
      <Icon name={theme === 'system' ? 'monitor' : theme === 'light' ? 'sun' : 'moon'} size={18} />
    </button>
  )
}
