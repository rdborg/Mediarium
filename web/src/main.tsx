import { loadLanguage } from './i18n'
import { applyLook } from './look'
import { startFold } from './fold'

// Dark unless light was picked (ThemeToggle.tsx), set before anything paints.
let theme = 'dark'
try {
  if (localStorage.getItem('mediarium-theme') === 'light') theme = 'light'
} catch {
  // blocked storage: dark
}
document.documentElement.dataset.theme = theme
applyLook() // pure black and the text size (look.ts)
startFold() // foldable and dual-screen layout (fold.ts)

// The app puts a page where it belongs when going back or forward (App shell); the browser's own guess would fight it.
try {
  history.scrollRestoration = 'manual'
} catch {
  // not available: nothing to do
}

// The chosen language is loaded before anything else, so text the app sets up
// as it starts is already in that language.
void loadLanguage().finally(() => import('./start.tsx'))

// A picture that fails to load (a cover that does not exist, a host that is down) shows the browser's broken-image
// icon. Hide it instead; the box it sits in keeps its own background. A new address that loads clears the mark.
document.addEventListener(
  'error',
  (e) => {
    if (e.target instanceof HTMLImageElement) e.target.dataset.broken = '1'
  },
  true,
)
document.addEventListener(
  'load',
  (e) => {
    if (e.target instanceof HTMLImageElement) delete e.target.dataset.broken
  },
  true,
)
