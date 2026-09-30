// The signal a request runs under: the caller's own (a search that the next
// keystroke replaces) together with a time limit for reads. Whichever fires
// first stops the call. No React or fetch in here, so it can be tested alone.
export function signalFor(own: AbortSignal | null | undefined, limitMs: number | undefined): AbortSignal | undefined {
  const limit = limitMs === undefined ? undefined : AbortSignal.timeout(limitMs)
  if (own && limit) return typeof AbortSignal.any === 'function' ? AbortSignal.any([own, limit]) : own
  return own ?? limit
}
