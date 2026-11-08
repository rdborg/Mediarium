import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './design.css'
import './setup.css'
import './projection.css'
import './mobile.css'
import './twopane.css'
import './fold.css'
import { demoEnabled, installDemo } from './demo'
import App from './App.tsx'
import { ToastProvider } from './components/Toast'
import { ConfirmProvider } from './components/ConfirmProvider'
import { PUSH_WORKER } from './push'
import { BASE_PATH } from './base'
import { isStaleWorker } from './staleWorker'
import { watchInstallPrompt } from './pwa'

if (demoEnabled()) installDemo()
watchInstallPrompt()

// Mediarium's only service worker is push-sw.js. It keeps the built scripts
// and styles (never anything from the API) and shows the "can't be reached"
// page. One left behind by another app that once ran on the same address
// would answer API calls from its own cache and show stale data, so remove
// such a worker and reload once. Workers of other apps that share the domain
// under their own path are not ours to touch (see staleWorker.ts).
if ('serviceWorker' in navigator) {
  navigator.serviceWorker
    .getRegistrations()
    .then(async (regs) => {
      const stale = regs.filter((r) => isStaleWorker(r.scope, [r.active, r.waiting, r.installing].map((w) => w?.scriptURL), BASE_PATH, PUSH_WORKER))
      if (stale.length === 0) return
      await Promise.all(stale.map((r) => r.unregister()))
      const ctrl = navigator.serviceWorker.controller
      if (ctrl && !ctrl.scriptURL.endsWith(PUSH_WORKER)) location.reload()
    })
    .catch(() => {
      // ignore — not having permission to look is fine
    })
}

// The worker also shows a short "can't be reached" page when the server is
// down (the installed app then doesn't open to the browser's error). It needs
// a secure address (https or localhost); anywhere else it is simply skipped.
if ('serviceWorker' in navigator && window.isSecureContext && !demoEnabled()) {
  navigator.serviceWorker.register(PUSH_WORKER).catch(() => {
    // not allowed here (a private window, a blocked setting): the app works the same
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
