import { useState } from 'react'
import type { QualityProfile } from '../api'
import Icon from './Icon'
import { profileBlurb } from './qualityBlurb'

// "If nothing is found at this quality, also try…": tick the other profiles
// to fall back to and drag them (or use the arrows) into the order to try.
// Ticked profiles come first, in order; the rest are listed underneath.
type Choice = { id: number; name: string }

// Works for video profiles and music profiles alike; `describe` gives the
// line under each name (video profiles have their own wording by default).
export default function FallbackEditor<P extends Choice = QualityProfile>({
  profiles,
  selfId,
  value,
  onChange,
  describe,
}: {
  profiles: P[]
  selfId?: number
  value: number[]
  onChange: (ids: number[]) => void
  describe?: (p: P) => string
}) {
  const blurb = (p: P) => (describe ? describe(p) : profileBlurb(p as unknown as QualityProfile))
  const [dragging, setDragging] = useState<number | null>(null)
  const others = profiles.filter((p) => p.id !== selfId)
  const chosen = value.map((id) => others.find((p) => p.id === id)).filter((p): p is P => !!p)
  const rest = others.filter((p) => !value.includes(p.id))

  function move(id: number, to: number) {
    const ids = value.filter((x) => x !== id)
    ids.splice(Math.max(0, Math.min(to, ids.length)), 0, id)
    onChange(ids)
  }

  return (
    <div className="fallback">
      {chosen.length === 0 && <p className="fallback-empty">Nothing ticked. It tries again at the next scheduled search.</p>}
      <ol className="fallback-list">
        {chosen.map((p, i) => (
          <li
            key={p.id}
            className={`fallback-row on${dragging === p.id ? ' dragging' : ''}`}
            draggable
            onDragStart={(e) => {
              setDragging(p.id)
              e.dataTransfer.effectAllowed = 'move'
            }}
            onDragEnd={() => setDragging(null)}
            onDragOver={(e) => {
              e.preventDefault()
              if (dragging !== null && dragging !== p.id) move(dragging, i)
            }}
          >
            <span className="fallback-grip" aria-hidden="true">
              <Icon name="menu" size={15} />
            </span>
            <span className="fallback-step">{i + 1}</span>
            <input type="checkbox" checked onChange={() => onChange(value.filter((x) => x !== p.id))} aria-label={`Stop falling back to ${p.name}`} />
            <span className="fallback-name">
              <strong>{p.name}</strong>
              <small>{blurb(p)}</small>
            </span>
            <span className="fallback-arrows">
              <button className="icon-btn" onClick={() => move(p.id, i - 1)} disabled={i === 0} aria-label={`Try ${p.name} earlier`}>
                <Icon name="chevron-up" size={15} />
              </button>
              <button className="icon-btn" onClick={() => move(p.id, i + 1)} disabled={i === chosen.length - 1} aria-label={`Try ${p.name} later`}>
                <Icon name="chevron-down" size={15} />
              </button>
            </span>
          </li>
        ))}
      </ol>
      {rest.length > 0 && (
        <ul className="fallback-list off">
          {rest.map((p) => (
            <li key={p.id} className="fallback-row">
              <span className="fallback-grip" />
              <span className="fallback-step" />
              <input type="checkbox" checked={false} onChange={() => onChange([...value, p.id])} aria-label={`Also try ${p.name}`} />
              <span className="fallback-name">
                <strong>{p.name}</strong>
                <small>{blurb(p)}</small>
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
