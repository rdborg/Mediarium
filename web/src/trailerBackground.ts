import { useCallback, useSyncExternalStore } from 'react'
import { t } from './i18n.ts'

// "Play the trailer behind the page" is on out of the box. An administrator can
// switch it off for everyone (Settings > Library and files > Movie and show
// info), and a browser can still choose for itself with the button on a
// title page: that choice is remembered in the browser and wins over the
// server's. The video comes from YouTube, which the setting says.
const EVENT = 'mediarium-trailer-bg-change'

// A choice this browser made, on or off, with the server's as the fallback.
function useBrowserChoice(key: string, serverDefault: boolean): [boolean, (on: boolean) => void] {
  // What this browser chose: '1' (on), '0' (off) or '' (nothing yet).
  const read = useCallback((): string => {
    try {
      const v = localStorage.getItem(key)
      return v === '1' || v === '0' ? v : ''
    } catch {
      return ''
    }
  }, [key])
  const chosen = useSyncExternalStore(subscribe, read, () => '')
  const on = chosen === '' ? serverDefault : chosen === '1'
  const set = useCallback(
    (next: boolean) => {
      try {
        localStorage.setItem(key, next ? '1' : '0')
      } catch {
        // A private window may refuse; the choice then lasts until the page is closed.
      }
      window.dispatchEvent(new Event(EVENT))
    },
    [key],
  )
  return [on, set]
}

function subscribe(cb: () => void) {
  window.addEventListener(EVENT, cb)
  window.addEventListener('storage', cb)
  return () => {
    window.removeEventListener(EVENT, cb)
    window.removeEventListener('storage', cb)
  }
}

export function useTrailerBackground(serverDefault = false): [boolean, (on: boolean) => void] {
  return useBrowserChoice('mediarium-trailer-bg', serverDefault)
}

// Whether the trailer starts with its sound on (the sound button on a title page changes it for this browser).
export function useTrailerSound(serverDefault = false): [boolean, (on: boolean) => void] {
  return useBrowserChoice('mediarium-trailer-sound', serverDefault)
}

// The embed address: muted, no controls, played once, from the privacy-friendly
// YouTube domain. enablejsapi lets the sound button unmute it.
export function trailerEmbedUrl(key: string): string {
  const q = new URLSearchParams({
    autoplay: '1',
    mute: '1',
    controls: '0',
    playsinline: '1',
    modestbranding: '1',
    rel: '0',
    disablekb: '1',
    iv_load_policy: '3',
    cc_load_policy: '0',
    enablejsapi: '1',
    vq: 'hd1080', // a hint for the best picture; YouTube still decides by the size of the player and the connection
  })
  return `https://www.youtube-nocookie.com/embed/${encodeURIComponent(key)}?${q.toString()}`
}

// What the trailer backdrop did on this page, kept in this browser only (key
// mediarium-trailer-log) so a problem can be looked at afterwards: no text
// from the page, no address, just what the player reported and when.
const LOG_KEY = 'mediarium-trailer-log'
let logStart = 0

export function trailerLogReset(): void {
  logStart = performance.now()
  try {
    const conn = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection
    localStorage.setItem(LOG_KEY, JSON.stringify([`begin visible=${document.visibilityState} focus=${document.hasFocus()} saveData=${!!conn?.saveData} reducedMotion=${window.matchMedia?.('(prefers-reduced-motion: reduce)').matches} width=${window.innerWidth} ua=${navigator.userAgent.replace(/^Mozilla\/5\.0 /, '').slice(0, 90)}`]))
  } catch {
    // Without storage there is no log.
  }
}

export function trailerLog(line: string): void {
  try {
    const list: string[] = JSON.parse(localStorage.getItem(LOG_KEY) ?? '[]')
    list.push(`${((performance.now() - logStart) / 1000).toFixed(1)}s ${line}`)
    localStorage.setItem(LOG_KEY, JSON.stringify(list.slice(-60)))
  } catch {
    // Without storage there is no log.
  }
}

// trailerProblem says in plain words why a trailer didn't play, from what
// YouTube's player reported (its error code), or 'timeout' when it never
// started.
export function trailerProblem(reason: string): string {
  switch (reason) {
    case '101':
    case '150':
      return t("The studio doesn't allow this trailer to play outside YouTube.")
    case '153':
    case '152':
      return t('YouTube refused to play the trailer here (error {code}).', { code: reason })
    case '2':
    case '5':
    case '100':
      return t("YouTube couldn't play this trailer (error {code}).", { code: reason })
    default:
      return t("The trailer didn't start. Your browser may be blocking video from YouTube, or the tab was in the background.")
  }
}

// Connections that ask to save data, and people who asked for less motion,
// get the still backdrop.
export function trailerAllowed(): boolean {
  if (typeof window === 'undefined') return false
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return false
  const conn = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection
  return !conn?.saveData
}
