// Foldable and dual-screen phones (V4): the installed app draws under the system
// status strip (index.html) and this file works out when to use that strip and the
// side strips for the app's own buttons. Kept in this browser, set before anything
// paints (main.tsx). No React here.

export type FoldChoice = '' | 'on' | 'off' // '' is automatic

const MODE_KEY = 'mediarium-fold'
const TOP_KEY = 'mediarium-fold-top'
const SIDE_KEY = 'mediarium-fold-side'
const SEEN_KEY = 'mediarium-fold-seen' // this device has shown a strip once: it is a foldable in every posture

export const FOLD_TOP_DEFAULT = 130 // width kept free beside the clock, signal and battery at the top
export const FOLD_STRIP = 72 // width of the side strip that holds the buttons when the phone is held sideways
export const FOLD_SIDE_DEFAULT = 130 // height kept free for them when they sit in a side strip

function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function write(key: string, value: string) {
  try {
    if (value) localStorage.setItem(key, value)
    else localStorage.removeItem(key)
  } catch {
    // a blocked storage only means the choice isn't kept
  }
}

export function savedFold(): FoldChoice {
  const v = read(MODE_KEY)
  return v === 'on' || v === 'off' ? v : ''
}

function savedNumber(key: string, fallback: number): number {
  const n = Number(read(key))
  return Number.isFinite(n) && n > 0 ? Math.min(400, n) : fallback
}

export const savedTop = () => savedNumber(TOP_KEY, FOLD_TOP_DEFAULT)
export const savedSide = () => savedNumber(SIDE_KEY, FOLD_SIDE_DEFAULT)

export type DisplayFacts = {
  screenW: number
  screenH: number
  innerW: number
  innerH: number
  ratio: number
  insets: { top: number; right: number; bottom: number; left: number }
  installed: boolean
}

// The safe-area values only exist as env() in CSS, so a hidden box is padded with
// them and measured.
function insets() {
  const probe = document.createElement('div')
  probe.style.cssText =
    'position:fixed;left:0;top:0;width:0;height:0;visibility:hidden;pointer-events:none;' +
    'padding:env(safe-area-inset-top,0px) env(safe-area-inset-right,0px) env(safe-area-inset-bottom,0px) env(safe-area-inset-left,0px)'
  document.body.appendChild(probe)
  const cs = getComputedStyle(probe)
  const px = (v: string) => Math.round(parseFloat(v) || 0)
  const out = { top: px(cs.paddingTop), right: px(cs.paddingRight), bottom: px(cs.paddingBottom), left: px(cs.paddingLeft) }
  probe.remove()
  return out
}

export function readFacts(): DisplayFacts {
  const innerW = window.innerWidth
  const innerH = window.innerHeight
  let installed = false
  try {
    installed = window.matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true
  } catch {
    // not available: treated as a browser tab
  }
  return {
    screenW: window.screen?.width ?? 0,
    screenH: window.screen?.height ?? 0,
    innerW,
    innerH,
    ratio: Math.round(window.devicePixelRatio * 100) / 100,
    insets: insets(),
    installed,
  }
}

// A near-square screen (a folded-out phone) that shows a system strip: the installed
// app on a tablet has a thin strip and a plain phone is much narrower than it is tall.
// A foldable iPhone draws its clock and battery as a floating group and reports no
// safe area for it, so an iPhone that wide counts without one (an iPad says "Macintosh").
// The closed outer screen is about 466 x 678 pt and the open inner one 626 x 890 pt, so a short
// side of 440 is the line (a plain phone is about 0.46 as wide as it is tall, these are 0.69).
function looksFoldable(f: DisplayFacts): boolean {
  const short = Math.min(f.innerW, f.innerH)
  const long = Math.max(f.innerW, f.innerH)
  const strip = Math.max(f.insets.top, f.insets.right, f.insets.left) >= 30
  const iphone = /iPhone/.test(navigator.userAgent)
  const shape = f.installed && short >= 440 && short / long >= 0.6
  // Held sideways the same phone can report no strip at all, so once it has shown
  // one in any posture it is remembered.
  if (shape && (strip || iphone)) write(SEEN_KEY, '1')
  return shape && (strip || iphone || read(SEEN_KEY) === '1')
}

export function foldInUse(f = readFacts()): boolean {
  const c = savedFold()
  return c === 'on' || (c === '' && looksFoldable(f))
}

// applyFold puts the layout on the page: data-fold is "tall" or "wide" (the way the
// screen is held), data-fold-side says which side strip holds the system icons.
export function applyFold() {
  const root = document.documentElement
  const f = readFacts()
  if (foldInUse(f)) {
    root.dataset.fold = f.innerW >= f.innerH ? 'wide' : 'tall'
    // no reported side strip: the system icons are taken to be at the right
    const side = f.insets.left > f.insets.right && f.insets.left >= 24 ? 'left' : 'right'
    root.dataset.foldSide = side
    root.style.setProperty('--fold-strip', `${Math.max(FOLD_STRIP, side === 'left' ? f.insets.left : f.insets.right)}px`)
  } else {
    delete root.dataset.fold
    delete root.dataset.foldSide
  }
  root.style.setProperty('--fold-top', `${savedTop()}px`)
  root.style.setProperty('--fold-side', `${savedSide()}px`)
}

export function setFold(choice: FoldChoice) {
  write(MODE_KEY, choice)
  applyFold()
}

export function setFoldTop(px: number) {
  write(TOP_KEY, px === FOLD_TOP_DEFAULT ? '' : String(Math.round(px)))
  applyFold()
}

export function setFoldSide(px: number) {
  write(SIDE_KEY, px === FOLD_SIDE_DEFAULT ? '' : String(Math.round(px)))
  applyFold()
}

// After the phone is turned or the fold is opened the installed app can be left with the
// old window (a bar at the top the page cannot draw on). Putting the viewport setting back
// makes the phone measure again; it does no harm when nothing is wrong.
let nudgeTimer = 0
function nudge() {
  if (!readFacts().installed) return
  window.clearTimeout(nudgeTimer)
  nudgeTimer = window.setTimeout(() => {
    const m = document.querySelector('meta[name="viewport"]')
    const c = m?.getAttribute('content')
    if (m && c) {
      m.setAttribute('content', c + ',')
      window.requestAnimationFrame(() => m.setAttribute('content', c))
    }
    // A second poke for a window that kept its old size: one pixel more height for a
    // frame makes the page measure itself again.
    const root = document.documentElement
    root.style.minHeight = 'calc(100dvh + 1px)'
    window.requestAnimationFrame(() => {
      root.style.minHeight = ''
      window.scrollTo(window.scrollX, window.scrollY)
    })
  }, 350)
}

// After the phone is turned or folded the safe-area numbers and the window size settle a
// moment later than the event, so the layout is worked out again a few times.
const SETTLE_MS = [0, 150, 500, 1200, 2500]
let settleTimers: number[] = []
function settle(run: () => void) {
  settleTimers.forEach((x) => window.clearTimeout(x))
  settleTimers = SETTLE_MS.map((ms) => window.setTimeout(run, ms))
}

// startFold applies the layout now and again whenever the screen changes shape
// (turning the phone, opening or closing the fold).
export function startFold() {
  const run = () => applyFold()
  if (document.body) run()
  else document.addEventListener('DOMContentLoaded', run, { once: true })
  let wide = window.innerWidth >= window.innerHeight
  window.addEventListener('resize', () => {
    run()
    settle(run)
    // turned, or the fold opened or closed (not just the keyboard coming up)
    const now = window.innerWidth >= window.innerHeight
    if (now !== wide) {
      wide = now
      nudge()
    }
  })
  window.addEventListener('orientationchange', () => {
    run()
    settle(run)
    nudge()
  })
  // the open or closed fold changes the visual viewport even when the window event is missed
  window.visualViewport?.addEventListener('resize', () => settle(run))
  window.addEventListener('pageshow', () => settle(run))
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') settle(run)
  })
}
