import { useEffect, useState } from 'react'
import { api, type FolderCheck } from '../api'
import { formatBytes } from '../format'

// Live check of a folder path as it is typed: does it exist, can Mediarium
// write to it, is it mapped from the host (Docker), and how much room is left.
export default function FolderStatus({ path }: { path: string }) {
  const [r, setR] = useState<FolderCheck | null>(null)

  useEffect(() => {
    if (!path.trim()) {
      setR(null)
      return
    }
    const t = setTimeout(() => {
      api.folderCheck(path.trim()).then(setR).catch(() => setR(null))
    }, 400)
    return () => clearTimeout(t)
  }, [path])

  if (!r) return null
  return (
    <div className="folder-status">
      <div className="folder-badges">
        {!r.exists ? (
          <span className="badge failed">folder not found</span>
        ) : (
          <>
            <span className={`badge ${r.writable ? 'downloaded' : 'failed'}`}>{r.writable ? 'writable' : 'read-only'}</span>
            {r.mountKnown && <span className={`badge ${r.mounted ? 'downloaded' : 'missing'}`}>{r.mounted ? 'mapped from host' : 'not a mapped folder'}</span>}
            {r.totalBytes > 0 && (
              <span className="badge">
                {formatBytes(r.freeBytes)} free of {formatBytes(r.totalBytes)}
              </span>
            )}
          </>
        )}
      </div>
      {r.warnings.map((w) => (
        <p key={w} className="folder-warn">
          {w}
        </p>
      ))}
    </div>
  )
}
