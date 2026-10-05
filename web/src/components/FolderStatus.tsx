import { useEffect, useState } from 'react'
import { api, type FolderCheck } from '../api'
import { formatBytes } from '../format'

// Live check of a folder path as it is typed: does it exist, can Mediarium
// write to it, is it mapped to the device (Docker), and how much room is left.
// A missing folder can be created when it would sit inside a mapped one, and
// `suggest` offers a better place next to the other library folders (for
// example /data/Ebooks when everything else is in /data).
export default function FolderStatus({ path, suggest, onUse }: { path: string; suggest?: string; onUse?: (path: string) => void }) {
  const [r, setR] = useState<FolderCheck | null>(null)
  const [alt, setAlt] = useState<FolderCheck | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!path.trim()) {
      setR(null)
      return
    }
    let stale = false
    const t = setTimeout(() => {
      api
        .folderCheck(path.trim())
        .then((c) => !stale && setR(c))
        .catch(() => !stale && setR(null))
    }, 400)
    return () => {
      stale = true
      clearTimeout(t)
    }
  }, [path])

  // The suggested place is only offered when it exists already or can be made.
  const offer = r && !r.exists && suggest && suggest !== path.trim() ? suggest : ''
  useEffect(() => {
    setAlt(null)
    if (!offer) return
    let stale = false
    api
      .folderCheck(offer)
      .then((c) => !stale && setAlt(c))
      .catch(() => undefined)
    return () => {
      stale = true
    }
  }, [offer])

  async function create(target: string) {
    setBusy(true)
    setError('')
    try {
      const c = await api.createFolder(target)
      if (target === path.trim()) setR(c)
      else onUse?.(target)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!r) return null
  const useAlt = offer && alt && (alt.exists || alt.canCreate) && onUse
  return (
    <div className="folder-status">
      <div className="folder-badges">
        {!r.exists ? (
          <span className="badge failed">folder not found</span>
        ) : (
          <>
            <span className={`badge ${r.writable ? 'downloaded' : 'failed'}`}>{r.writable ? 'writable' : 'read-only'}</span>
            {r.mountKnown && <span className={`badge ${r.mounted ? 'downloaded' : 'missing'}`}>{r.mounted ? 'Mapped to your device' : 'not a mapped folder'}</span>}
            {r.totalBytes > 0 && (
              <span className="badge">
                {formatBytes(r.freeBytes)} free of {formatBytes(r.totalBytes)}
              </span>
            )}
          </>
        )}
      </div>
      {useAlt ? (
        <p className="folder-warn">
          This folder isn&apos;t mapped to your device. Your other folders are in {offer.slice(0, offer.lastIndexOf('/')) || '/'}, so {offer} works without changing your compose file.{' '}
          <button className="btn-sm primary" disabled={busy} onClick={() => (alt.exists ? onUse(offer) : void create(offer))}>
            {busy ? 'Creating…' : `Use ${offer}`}
          </button>
        </p>
      ) : (
        r.warnings.map((w) => (
          <p key={w} className="folder-warn">
            {w}
          </p>
        ))
      )}
      {!r.exists && r.canCreate && !useAlt && (
        <div>
          <button className="btn-sm primary" disabled={busy} onClick={() => void create(path.trim())}>
            {busy ? 'Creating…' : 'Create this folder'}
          </button>
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
