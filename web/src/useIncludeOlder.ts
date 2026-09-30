import { useSyncExternalStore } from 'react'

// "Include older titles" on Discover's "More like your library": off means
// roughly the last 15 years only. The choice is remembered in this browser,
// and every switch on the page follows it.
const KEY = 'mediarium.discover.olderTitles'

function read(): boolean {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

let current = read()
const listeners = new Set<() => void>()

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function setIncludeOlder(next: boolean): void {
  current = next
  try {
    localStorage.setItem(KEY, next ? '1' : '0')
  } catch {
    // Private windows can refuse storage: the choice then lasts until the page closes.
  }
  listeners.forEach((fn) => fn())
}

export function useIncludeOlder(): [boolean, (next: boolean) => void] {
  const value = useSyncExternalStore(subscribe, () => current)
  return [value, setIncludeOlder]
}
