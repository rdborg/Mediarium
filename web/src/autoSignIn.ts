// Sending visitors who are not signed in straight to the single sign-on
// provider, when an administrator chose that (Settings > People and access).
// The sign-in page asks shouldAutoSignIn before it does anything, so every
// case that must keep the normal page is decided in one place.

/** Address parameters that show the normal sign-in page instead of redirecting. */
export const BYPASS_PARAMS = ['no-redirect', 'disable-auth-redirect']

/** Set when someone signs out, so the next sign-in page is shown, not skipped. */
export const SKIP_ONCE_KEY = 'mediarium.skipAutoSignIn'

/** When the page last sent the browser to the provider (ms), to stop a loop. */
export const TRIED_KEY = 'mediarium.autoSignInAt'

/** A second automatic redirect within this long is a loop, not a sign-in. */
export const RETRY_AFTER_MS = 60_000

export interface AutoSignInInput {
  /** What the server says to redirect to: "oidc" or nothing. It is only set while that method works. */
  method: string | undefined
  /** The provider's button is offered on the page. */
  oidcAvailable: boolean
  /** The address's query string, with or without the question mark. */
  search: string
  /** The sign-in problem code the provider's return trip left in the address, or "". */
  problemCode: string
  /** The person just signed out. */
  skipOnce: boolean
  /** The page already shows a notice (signed out for inactivity, backup restored). */
  notice: boolean
  /** When the page last redirected, or null. */
  lastTriedAt: number | null
  now: number
}

/** True when the sign-in page should send the browser to the provider by itself. */
export function shouldAutoSignIn(i: AutoSignInInput): boolean {
  if (i.method !== 'oidc' || !i.oidcAvailable) return false
  if (i.problemCode || i.skipOnce || i.notice) return false
  const params = new URLSearchParams(i.search)
  if (BYPASS_PARAMS.some((p) => params.has(p))) return false
  if (i.lastTriedAt !== null && i.now - i.lastTriedAt >= 0 && i.now - i.lastTriedAt < RETRY_AFTER_MS) return false
  return true
}

/**
 * The page to come back to after signing in: the address without the prefix
 * Mediarium is served under, kept only when it is a short path on this site.
 * The front page needs no mention.
 */
export function returnPath(pathname: string, search: string): string {
  const path = pathname + search
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\') || path.length > 512) return ''
  if (pathname === '/' && search === '') return ''
  return path
}

/** The start address with the page to return to, ready for the browser (prefix already in startPath). */
export function autoSignInUrl(startPath: string, pathname: string, search: string): string {
  const next = returnPath(pathname, search)
  return next ? `${startPath}?next=${encodeURIComponent(next)}` : startPath
}
