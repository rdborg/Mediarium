import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './design.css'
import './setup.css'
import { demoEnabled, installDemo } from './demo'
import App from './App.tsx'
import { ToastProvider } from './components/Toast'
import { ConfirmProvider } from './components/ConfirmProvider'

if (demoEnabled()) installDemo()

// Applied before first paint so a stored theme override (ThemeToggle.tsx)
// takes effect immediately instead of flashing the OS default first.
try {
  const stored = localStorage.getItem('mediarium-theme')
  if (stored === 'light' || stored === 'dark') {
    document.documentElement.dataset.theme = stored
  }
} catch {
  // ignore — worst case is one flash of the OS-default theme
}

// Mediarium has no service worker. One left behind by another app that once
// ran on the same address would answer API calls from its own cache and
// show stale data, so remove any we find and reload once.
if ('serviceWorker' in navigator) {
  navigator.serviceWorker
    .getRegistrations()
    .then(async (regs) => {
      if (regs.length === 0) return
      await Promise.all(regs.map((r) => r.unregister()))
      if (navigator.serviceWorker.controller) location.reload()
    })
    .catch(() => {
      // ignore — not having permission to look is fine
    })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ToastProvider>
      <ConfirmProvider>
        <App />
      </ConfirmProvider>
    </ToastProvider>
  </StrictMode>,
)
