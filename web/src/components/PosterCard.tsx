import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import BrandMark from './BrandMark'
import Icon, { type IconName } from './Icon'
import type { ItemState } from './state'

// Shown when a title has no poster (or its image fails to load): the Mediarium
// mark on a soft gradient, with no text, so every card stays the same shape.
export function PosterFallback() {
  return (
    <div className="poster-fallback" aria-hidden="true">
      <BrandMark className="" />
    </div>
  )
}

// The same for a record sleeve: a music note on the soft gradient.
export function MusicFallback() {
  return (
    <div className="poster-fallback music-fallback" aria-hidden="true">
      <Icon name="music" size={48} />
    </div>
  )
}

export interface CardAction {
  icon: IconName
  label: string
  onClick: () => void
  active?: boolean
  danger?: boolean
  disabled?: boolean
  // The main thing to do with the card (Add): shown with its label, and lit
  // up while the pointer is over the card.
  primary?: boolean
}

// A banner across the corner of the picture ("eBook + Audio").
export interface CardRibbon {
  label: string
  tone: string // a class suffix: ebook, audio, both
}

// One poster in a grid: the picture and title open the item, a status pill sits
// on the image, and a row of quick actions (search, monitor, remove...) is
// always one tap away (on a mouse they appear over the poster on hover).
export default function PosterCard({
  to,
  poster,
  title,
  meta,
  status,
  statusLabel,
  dim,
  actions,
  footer,
  selection,
  state,
  genres,
  rating,
  kind,
  progress,
  ribbon,
}: {
  to?: string
  poster?: string
  title: string
  meta?: ReactNode
  status?: string
  statusLabel?: string
  dim?: boolean
  actions?: CardAction[]
  footer?: ReactNode
  // When set, the card is in "select" mode: clicking the picture ticks it
  // instead of opening it.
  selection?: { selected: boolean; onToggle: (e: { shiftKey: boolean }) => void }
  state?: ItemState
  genres?: string[]
  rating?: number
  kind?: 'movie' | 'tv' | 'music'
  ribbon?: CardRibbon
  progress?: { pct: number; label?: string }
}) {
  const [broken, setBroken] = useState(false)
  const image = poster && !broken ? <img src={poster} alt="" loading="lazy" onError={() => setBroken(true)} /> : kind === 'music' ? <MusicFallback /> : <PosterFallback />
  return (
    <article className={`pcard${dim ? ' dim' : ''}${selection?.selected ? ' selected' : ''}${kind ? ` kind-${kind}` : ''}`}>
      <div className="pcard-art">
        {selection ? (
          <button className="pcard-link pcard-select" onClick={(e) => selection.onToggle({ shiftKey: e.shiftKey })} aria-pressed={selection.selected} aria-label={`Select ${title}`}>
            {image}
            <span className={`pcard-check${selection.selected ? ' on' : ''}`} aria-hidden="true">
              {selection.selected && <Icon name="check" size={16} />}
            </span>
          </button>
        ) : to ? (
          <Link to={to} className="pcard-link" aria-label={title}>
            {image}
          </Link>
        ) : (
          image
        )}
        {state ? (
          <span className={`pill pill-${state.key}`} title={state.hint}>
            <Icon name={state.icon} size={12} /> {state.label}
          </span>
        ) : (
          status && <span className={`pill pill-${status}`}>{statusLabel ?? status}</span>
        )}
        {rating ? (
          <span className="pcard-rating" title="Rating">
            <Icon name="star" size={11} /> {rating.toFixed(1)}
          </span>
        ) : null}
        {ribbon && (
          <span className={`pcard-ribbon tone-${ribbon.tone}`} aria-label={ribbon.label}>
            {ribbon.label}
          </span>
        )}
        {kind && <span className={`pcard-kind kind-${kind}`} title={kind === 'movie' ? 'Movie' : kind === 'music' ? 'Music' : 'TV show'} />}
        {actions && actions.length > 0 && !selection && (
          <div className="pcard-actions">
            {actions.map((a) => (
              <button
                key={a.label}
                className={`icon-btn${a.active ? ' on' : ''}${a.danger ? ' danger' : ''}${a.primary ? ' primary-act' : ''}`}
                title={a.label}
                aria-label={a.label}
                aria-pressed={a.active === undefined ? undefined : a.active}
                disabled={a.disabled}
                onClick={a.onClick}
              >
                <Icon name={a.icon} size={17} />
                {a.primary && <span>{a.label}</span>}
              </button>
            ))}
          </div>
        )}
      </div>
      <div className="pcard-body">
        {to ? (
          <Link to={to} className="pcard-title">
            {title}
          </Link>
        ) : (
          <span className="pcard-title">{title}</span>
        )}
        {genres && genres.length > 0 && <span className="pcard-genres">{genres.slice(0, 2).join(' · ')}</span>}
        {meta && <span className="pcard-meta">{meta}</span>}
        {progress && (
          <span className="pcard-progress" title={progress.label}>
            <span className="bar active">
              <span style={{ width: `${Math.max(3, progress.pct)}%` }} />
            </span>
            <small>{progress.label ?? `${Math.round(progress.pct)}%`}</small>
          </span>
        )}
        {footer}
      </div>
    </article>
  )
}
