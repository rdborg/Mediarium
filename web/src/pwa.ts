// Installing Mediarium as an app: what this browser offers (a real install
// button, or steps to follow by hand), and whether it already runs as one.

export interface InstallEnv {
  userAgent: string
  maxTouchPoints: number
  standalone: boolean
  secure: boolean
  canPrompt: boolean
}

export type InstallKind =
  | 'installed' // already running as an app: nothing to show
  | 'insecure' // not https: browsers won't install it
  | 'prompt' // Chrome, Edge, Android: one button
  | 'iphone'
  | 'ipad'
  | 'mac-safari'
  | 'menu' // any other browser: its own menu

// installKind decides which help fits this browser.
export function installKind(env: InstallEnv): InstallKind {
  if (env.standalone) return 'installed'
  if (!env.secure) return 'insecure'
  if (env.canPrompt) return 'prompt'
  const ua = env.userAgent
  if (/iPhone|iPod/.test(ua)) return 'iphone'
  // iPadOS 13+ says it is a Mac: a Mac with a touch screen is an iPad.
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && env.maxTouchPoints > 1)) return 'ipad'
  const safari = /Safari\//.test(ua) && !/Chrome\/|Chromium\/|Edg\/|OPR\/|Firefox\/|FxiOS\//.test(ua)
  if (/Macintosh/.test(ua) && safari) return 'mac-safari'
  return 'menu'
}

interface InstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

let deferred: InstallPromptEvent | null = null
const listeners = new Set<() => void>()

// watchInstallPrompt keeps the browser's install offer, which arrives once
// and early, so the Settings page can use it later. Call it at start-up.
export function watchInstallPrompt(): void {
  if (typeof window === 'undefined') return
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferred = e as InstallPromptEvent
    listeners.forEach((f) => f())
  })
  window.addEventListener('appinstalled', () => {
    deferred = null
    listeners.forEach((f) => f())
  })
}

export function onInstallChange(f: () => void): () => void {
  listeners.add(f)
  return () => {
    listeners.delete(f)
  }
}

export function currentInstallKind(): InstallKind {
  return installKind({
    userAgent: navigator.userAgent,
    maxTouchPoints: navigator.maxTouchPoints || 0,
    standalone: window.matchMedia('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true,
    secure: window.isSecureContext,
    canPrompt: deferred !== null,
  })
}

// installNow shows the browser's own install question.
export async function installNow(): Promise<void> {
  const e = deferred
  if (!e) return
  await e.prompt()
  await e.userChoice
  deferred = null
  listeners.forEach((f) => f())
}
