// After a restore from Settings > System > Backup and restore the app restarts on the restored
// database, which doesn't hold this browser's session, so the next thing the
// person sees is the sign-in page. This remembers, for this tab only, that a
// restore was just started, so the sign-in page can say it worked.

const KEY = 'mediarium-restored-at'

// A note older than this belongs to some earlier restore and is not shown.
export const RESTORE_NOTICE_MS = 15 * 60 * 1000

type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

function session(): Store | null {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage
  } catch {
    return null
  }
}

// markRestored notes that the server accepted a backup and is restarting.
export function markRestored(now = Date.now(), store: Store | null = session()): void {
  try {
    store?.setItem(KEY, String(now))
  } catch {
    // storage blocked: the sign-in page just won't mention it
  }
}

// forgetRestored drops the note, for a restore that didn't come back.
export function forgetRestored(store: Store | null = session()): void {
  try {
    store?.removeItem(KEY)
  } catch {
    // nothing to do
  }
}

// takeRestored reports whether a restore was started recently in this tab and
// drops the note, so the notice is shown once.
export function takeRestored(now = Date.now(), store: Store | null = session()): boolean {
  try {
    const raw = store?.getItem(KEY)
    if (raw == null) return false
    store?.removeItem(KEY)
    const at = Number(raw)
    return Number.isFinite(at) && now >= at && now - at < RESTORE_NOTICE_MS
  } catch {
    return false
  }
}
