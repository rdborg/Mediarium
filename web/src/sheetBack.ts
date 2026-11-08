// On a phone the Back button (or the back swipe) closes an open pop-up or
// the menu instead of leaving the page under it (V4 B2). When a pop-up opens, one
// history entry for the same address is added; Back takes it away and the
// pop-up is closed the way Esc closes it. A pop-up closed any other way takes
// its entry back out, so Back afterwards goes where it always went.
//
// No React and no DOM in the logic, so it can be tested on its own; watch()
// at the bottom ties it to the page.

export interface SheetEnv {
  isPhone(): boolean
  openCount(): number // pop-ups open now
  push(): void // add the pop-up's history entry
  back(): void // go one entry back
  currentIsSheet(): boolean // the entry showing is a pop-up's
  close(): void // close the top pop-up, as Esc does
}

export function sheetBack(env: SheetEnv) {
  let pushed = false
  let ignorePop = false
  return {
    // Something on the page changed: a pop-up may have opened or closed.
    changed() {
      const open = env.openCount()
      if (open > 0 && !pushed && env.isPhone()) {
        env.push()
        pushed = true
      } else if (open === 0 && pushed) {
        pushed = false
        // Only when the pop-up's entry is still the one showing: a pop-up that
        // closed because a link was followed has a new page on top of it.
        if (env.currentIsSheet()) {
          ignorePop = true
          env.back()
        }
      }
    },
    // The history moved back or forward.
    popped() {
      if (ignorePop) {
        ignorePop = false
        return
      }
      if (pushed) {
        pushed = false
        if (env.openCount() > 0) env.close()
        return
      }
      // Back from a page into the entry of a pop-up that is long gone: step over it.
      if (env.currentIsSheet() && env.openCount() === 0) {
        ignorePop = true
        env.back()
      }
    },
  }
}

const KEY = 'mediariumSheet'
// Pop-ups, and the menu drawer that More opens.
const OPEN = '.modal-backdrop:not([hidden]), .sidebar.open'

// watch ties sheetBack to the page; it returns the function that stops it.
export function watch(): () => void {
  const phone = window.matchMedia('(max-width: 700px)')
  const sb = sheetBack({
    isPhone: () => phone.matches,
    openCount: () => document.querySelectorAll(OPEN).length,
    push: () => window.history.pushState({ ...(window.history.state ?? {}), [KEY]: true }, ''),
    back: () => window.history.back(),
    currentIsSheet: () => !!(window.history.state as Record<string, unknown> | null)?.[KEY],
    close: () => {
      const all = document.querySelectorAll<HTMLElement>(OPEN)
      const top = all[all.length - 1]
      const target = top?.querySelector<HTMLElement>('.modal') ?? top
      target?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    },
  })
  let frame = 0
  const changed = () => {
    if (frame) return
    frame = window.requestAnimationFrame(() => {
      frame = 0
      sb.changed()
    })
  }
  const watcher = new MutationObserver(changed)
  // Pop-ups come and go; the menu opens by its class.
  watcher.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'hidden'] })
  const popped = () => sb.popped()
  window.addEventListener('popstate', popped)
  return () => {
    watcher.disconnect()
    window.removeEventListener('popstate', popped)
    if (frame) window.cancelAnimationFrame(frame)
  }
}
