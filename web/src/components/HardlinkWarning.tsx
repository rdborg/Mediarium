import { useEffect, useState } from 'react'
import { api } from '../api'

// Surfaces PRD §5.2/§4.8's explicit ask: warn the user up front if their
// downloads and library paths won't support hardlinking (the #1
// misconfiguration in this class of app — it silently doubles storage use
// instead of erroring). Used in both the onboarding wizard and Settings,
// since PRD says every wizard step should be independently reachable and
// re-checkable later, not a one-time-only choice.
export default function HardlinkWarning({ pathA, pathB }: { pathA: string; pathB: string }) {
  const [result, setResult] = useState<{ sameFilesystem: boolean; supported: boolean; error?: string } | null>(null)

  useEffect(() => {
    if (!pathA || !pathB) {
      setResult(null)
      return
    }
    const timeout = setTimeout(() => {
      api.filesystemCheck(pathA, pathB).then(setResult).catch(() => setResult(null))
    }, 400) // debounce while the user is still typing
    return () => clearTimeout(timeout)
  }, [pathA, pathB])

  if (!result) return null

  if (!result.supported) {
    return (
      <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem' }}>
        Couldn't automatically verify whether these paths share a filesystem on this platform — double-check
        manually that hardlinking will work (see the README's troubleshooting section).
      </p>
    )
  }
  if (result.error) {
    return (
      <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem' }}>
        Couldn't check yet ({result.error}) — this is normal if the path doesn't exist inside the container yet.
      </p>
    )
  }
  if (!result.sameFilesystem) {
    return (
      <p className="error-text" style={{ fontSize: '0.85rem' }}>
        ⚠ These paths are on different filesystems — imports will fall back to copying instead of hardlinking,
        which uses roughly double the storage. Mount your downloads and library paths on the same volume if
        possible.
      </p>
    )
  }
  return (
    <p style={{ color: 'var(--success)', fontSize: '0.85rem' }}>✓ Same filesystem — hardlinking will work.</p>
  )
}
