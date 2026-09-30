import { useEffect, useState, type CSSProperties, type ReactNode } from 'react'
import { SLOW_MS } from '../busyState'

// The grey placeholder shown while a page's data loads. If the data has not
// arrived after ten seconds it turns into a plain message with a Try again
// button, instead of staying grey for ever. Pass your own placeholder as
// children when it is more than one block (a grid of cards, say).
export default function Loading({
  height = 96,
  radius,
  onRetry,
  children,
}: {
  height?: number
  radius?: number
  onRetry?: () => void
  children?: ReactNode
}) {
  const [slow, setSlow] = useState(false)
  useEffect(() => {
    const t = setTimeout(() => setSlow(true), SLOW_MS)
    return () => clearTimeout(t)
  }, [])

  if (!slow) {
    const style: CSSProperties = { height, ...(radius ? { borderRadius: radius } : {}) }
    return <>{children ?? <div className="skeleton" style={style} />}</>
  }
  return (
    <div className="slow-note" role="status" style={{ minHeight: Math.min(height, 120) }}>
      <span>
        <strong>Taking longer than usual.</strong> Mediarium may be busy with a download or import.
      </span>
      <button type="button" className="btn-sm" onClick={onRetry ?? (() => window.location.reload())}>
        Try again
      </button>
    </div>
  )
}
