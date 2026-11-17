import { useEffect, useSyncExternalStore } from 'react'
import { api } from './api'
import { useAuth } from './AuthContext'
import { hideParam, parseServices, reconcileServices, type MyServices } from './streamingServices.ts'

// The streaming services a person picked on Discover and the switch that hides
// them. They are kept with the account, so they follow the person to every
// browser and device. An older version kept them in this browser only: that
// choice is read once, moved into the account and then forgotten here. When
// sign-in is switched off there is no account, and the browser keeps them.
const SERVICES_KEY = 'mediarium.discover.myServices'
const SWITCH_KEY = 'mediarium.discover.hideServices'

function readLocal(): MyServices {
  try {
    return { keys: parseServices(localStorage.getItem(SERVICES_KEY)), on: localStorage.getItem(SWITCH_KEY) === '1' }
  } catch {
    return { keys: [], on: false }
  }
}

function writeLocal(next: MyServices): void {
  try {
    localStorage.setItem(SERVICES_KEY, next.keys.join(','))
    localStorage.setItem(SWITCH_KEY, next.on ? '1' : '0')
  } catch {
    // Private windows can refuse storage: the choice then lasts until the page closes.
  }
}

function forgetLocal(): void {
  try {
    localStorage.removeItem(SERVICES_KEY)
    localStorage.removeItem(SWITCH_KEY)
  } catch {
    // Nothing to forget.
  }
}

let current = readLocal()
// null until the server has answered; false when there is no account to keep it with.
let account: boolean | null = null
let loading = false
// The signed-in person the choice belongs to (null when no one is signed in).
// Signing out and in as someone else does not reload the page, so the store
// starts over when this changes instead of carrying one person's choice to the next.
let owner: number | null | undefined = undefined
// Counts the loads started, so an answer for an earlier person or load is dropped.
let generation = 0
// Set when the person changes something while the saved choice is still being read:
// that change is newer than the answer and must not be undone by it.
let changedWhileLoading = false
const listeners = new Set<() => void>()

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

function publish(next: MyServices): void {
  current = next
  listeners.forEach((fn) => fn())
}

function send(next: MyServices): void {
  api.putDiscoverServices(next.keys, next.on).catch(() => {
    // The choice stays on screen; it is sent again with the next change.
  })
}

// Asks the account for the saved choice, once per signed-in person (again after a failure).
export function loadMyServices(person: number | null = null): void {
  if (person !== owner) {
    owner = person
    account = null
    loading = false
    changedWhileLoading = false
    generation++
    publish(readLocal())
  }
  if (account !== null || loading) return
  loading = true
  changedWhileLoading = false
  const mine = generation
  api
    .discoverServices()
    .then((server) => {
      if (mine !== generation) return
      account = server.account
      if (changedWhileLoading) return // the newer choice on screen was already sent
      const r = reconcileServices(server, readLocal())
      if (r.push) {
        // The old browser choice is only forgotten once the account has it.
        api
          .putDiscoverServices(r.push.keys, r.push.on)
          .then(() => {
            if (r.forgetLocal) forgetLocal()
          })
          .catch(() => {
            // Kept in this browser, so it is offered to the account again next time.
          })
      } else if (r.forgetLocal) {
        forgetLocal()
      }
      publish(r.next)
    })
    .catch(() => {
      // Offline or the server is busy: keep what is shown and ask again next time.
    })
    .finally(() => {
      if (mine === generation) loading = false
    })
}

function save(next: MyServices): void {
  if (loading) changedWhileLoading = true
  if (account !== true) writeLocal(next)
  if (account !== false) send(next)
  publish(next)
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
  const person = useAuth().user?.id ?? null
  useEffect(() => loadMyServices(person), [person])
  const value = useSyncExternalStore(subscribe, () => current)
  return { ...value, hide: hideParam(value.keys, value.on) }
}
