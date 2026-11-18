// Whether the demo (made-up data instead of the server's) is switched on in this browser. Kept apart from demo.ts,
// which holds the made-up data and is only fetched when the demo is on.
const KEY = 'mediarium-demo'

export function demoEnabled(): boolean {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

export function setDemo(on: boolean) {
  try {
    if (on) localStorage.setItem(KEY, '1')
    else localStorage.removeItem(KEY)
  } catch {
    // ignore
  }
  window.location.reload()
}
