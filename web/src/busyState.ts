// Whether Mediarium is being slow to answer. Every read the app makes reports
// here: one that has been waiting longer than SLOW_MS, or an answer saying the
// app is busy, turns the state on, and the banner under the top bar says so.
// It turns off again by itself once the app answers normally.
//
// No React in this file, so it can be tested on its own (see busy.ts for the hook).

// A read that waits this long counts as slow.
export const SLOW_MS = 10_000
// After a "busy" answer the state stays on this long (or until an answer that works).
const HOLD_MS = 20_000

let slowReads = 0
let busyUntil = 0
let expiry: ReturnType<typeof setTimeout> | undefined
const listeners = new Set<() => void>()

function emit() {
  for (const fn of [...listeners]) fn()
}

export function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function isBusy(now: number = Date.now()): boolean {
  return slowReads > 0 || now < busyUntil
}

// Call when a read starts; call the returned function when it ends, however it ends.
export function startRead(): () => void {
  let slow = false
  const timer = setTimeout(() => {
    slow = true
    slowReads++
    emit()
  }, SLOW_MS)
  return () => {
    clearTimeout(timer)
    if (slow) {
      slow = false
      slowReads--
      emit()
    }
  }
}

// The app answered that it is busy (or did not answer in time).
export function noteBusy() {
  busyUntil = Date.now() + HOLD_MS
  clearTimeout(expiry)
  expiry = setTimeout(emit, HOLD_MS + 50)
  emit()
}

// The app gave a normal answer.
export function noteFine() {
  if (busyUntil === 0) return
  busyUntil = 0
  clearTimeout(expiry)
  emit()
}

// For tests: forget everything.
export function resetBusy() {
  slowReads = 0
  busyUntil = 0
  clearTimeout(expiry)
  listeners.clear()
}
