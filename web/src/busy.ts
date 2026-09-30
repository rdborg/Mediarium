import { useSyncExternalStore } from 'react'
import { isBusy, subscribe } from './busyState'

// True while Mediarium is slow to answer (see busyState.ts).
export function useBusy(): boolean {
  return useSyncExternalStore(subscribe, () => isBusy())
}
