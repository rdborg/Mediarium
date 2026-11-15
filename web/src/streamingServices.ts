import { useSyncExternalStore } from 'react'
import { t } from './i18n.ts'

// "Hide what is on my streaming services" on Discover: the services a person
// already has, and a switch that leaves their titles out of the lists. Both
// are remembered in this browser, like "Hide owned", and everything on the
// page follows them. The switch starts off. The keys are the ones the server
// knows (internal/metadata/services.go).
export const SERVICES = [
  { key: 'netflix', name: 'Netflix' },
  { key: 'prime', name: 'Prime Video' },
  { key: 'disney', name: 'Disney+' },
  { key: 'max', name: 'Max (HBO)' },
  { key: 'apple', name: 'Apple TV+' },
  { key: 'paramount', name: 'Paramount+' },
  { key: 'hulu', name: 'Hulu' },
  { key: 'peacock', name: 'Peacock' },
] as const

const KEYS = new Set<string>(SERVICES.map((s) => s.key))
const SERVICES_KEY = 'mediarium.discover.myServices'
const SWITCH_KEY = 'mediarium.discover.hideServices'

// The saved list, as service keys in the order of SERVICES; anything unknown (an old or edited value) is dropped.
export function parseServices(raw: string | null | undefined): string[] {
  const picked = new Set((raw ?? '').split(',').map((k) => k.trim()))
  return SERVICES.map((s) => s.key as string).filter((k) => picked.has(k))
}

// What goes to the server as ?hide=: the chosen services while the switch is on, nothing otherwise.
export function hideParam(keys: readonly string[], on: boolean): string {
  return on ? keys.filter((k) => KEYS.has(k)).join(',') : ''
}

// "Hiding Netflix, Disney+" for the summary line, or nothing while the switch is off.
export function hidingSummary(keys: readonly string[], on: boolean): string {
  if (!on || keys.length === 0) return ''
  const names = SERVICES.filter((s) => keys.includes(s.key)).map((s) => s.name)
  return t('Hiding {services}', { services: names.join(', ') })
}

export interface MyServices {
  keys: string[]
  on: boolean
}

function read(): MyServices {
  try {
    return { keys: parseServices(localStorage.getItem(SERVICES_KEY)), on: localStorage.getItem(SWITCH_KEY) === '1' }
  } catch {
    return { keys: [], on: false }
  }
}

let current = read()
const listeners = new Set<() => void>()

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

function save(next: MyServices): void {
  current = next
  try {
    localStorage.setItem(SERVICES_KEY, next.keys.join(','))
    localStorage.setItem(SWITCH_KEY, next.on ? '1' : '0')
  } catch {
    // Private windows can refuse storage: the choice then lasts until the page closes.
  }
  listeners.forEach((fn) => fn())
}

export function toggleService(key: string): void {
  const has = current.keys.includes(key)
  const keys = parseServices(has ? current.keys.filter((k) => k !== key).join(',') : [...current.keys, key].join(','))
  // With no service left there is nothing to hide, so the switch goes off too.
  save({ keys, on: keys.length === 0 ? false : current.on })
}

export function setHideServices(on: boolean): void {
  save({ keys: current.keys, on: on && current.keys.length > 0 })
}

export function useMyServices(): MyServices & { hide: string } {
  const value = useSyncExternalStore(subscribe, () => current)
  return { ...value, hide: hideParam(value.keys, value.on) }
}
