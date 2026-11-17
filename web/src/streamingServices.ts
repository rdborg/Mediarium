import { t } from './i18n.ts'

// "Hide what is on my streaming services" on Discover: the services a person
// already has, and a switch that leaves their titles out of the lists. Both
// are kept with the account (see myServices.ts), and everything on the page
// follows them. The switch starts off. The keys are the ones the server knows
// (internal/metadata/services.go).
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

// What the server keeps for the signed-in account (GET /api/auth/discover-services).
export interface SavedServices {
  services: string[]
  on: boolean
  // False until the person has picked once.
  saved: boolean
  // False when nobody is signed in, so nothing can be kept on the server.
  account: boolean
}

// Decides what to show after the server has answered, given what an older
// version kept in this browser. With an account the server's choice wins; if it
// has none yet the browser's is adopted and goes to the account once (push),
// so nobody loses their pick. Without an account the browser keeps it.
export function reconcileServices(server: SavedServices, local: MyServices): { next: MyServices; push: MyServices | null; forgetLocal: boolean } {
  if (!server.account) return { next: local, push: null, forgetLocal: false }
  if (server.saved) {
    const keys = parseServices(server.services.join(','))
    return { next: { keys, on: server.on && keys.length > 0 }, push: null, forgetLocal: true }
  }
  if (local.keys.length > 0) return { next: local, push: local, forgetLocal: true }
  return { next: { keys: [], on: false }, push: null, forgetLocal: true }
}
