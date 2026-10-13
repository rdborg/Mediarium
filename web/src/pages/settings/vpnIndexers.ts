// Whether a FlareSolverr is in use, for the note under "Send indexer traffic
// through the VPN". Kept apart from the page so it can be tested.
import type { Settings } from '../../api.ts'

export function flareSolverrSetUp(s: Pick<Settings, 'flareSolverrUrl' | 'flareSolverrBundled'>): boolean {
  return !!s.flareSolverrUrl?.trim() || s.flareSolverrBundled === true
}
