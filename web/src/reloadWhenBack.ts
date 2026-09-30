import { waitForApp } from './updateSteps'

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

// The running version, or null while Mediarium does not answer (restarting).
async function fetchVersion(): Promise<string | null> {
  try {
    const r = await fetch('/api/version', { cache: 'no-store' })
    if (!r.ok) return null
    const body = (await r.json()) as { version?: string }
    return body.version ?? null
  } catch {
    return null
  }
}

// Waits for Mediarium to come back after a restart, then reloads the page so
// it shows the version that is running now. The reload also picks up the new
// interface, since the pages are part of the program. Resolves false when it
// did not come back in time.
export async function reloadWhenBack(expectVersion?: string): Promise<boolean> {
  const result = await waitForApp({ expectVersion, fetchVersion, sleep, now: Date.now })
  if (result === 'back') {
    window.location.reload()
    return true
  }
  return false
}
