import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './design.css'
import { demoEnabled, installDemo } from './demo'
import App from './App.tsx'
import { ToastProvider } from './components/Toast'

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

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ToastProvider>
      <App />
    </ToastProvider>
  </StrictMode>,
)
