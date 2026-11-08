import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import BrandMark from './BrandMark'
import Icon, { type IconName } from './Icon'
import type { ItemState } from './state'
import type { OutsideScores } from '../api'
import { t } from '../i18n'
import { coverThumb, isMusicCover, THUMB_SIDE } from '../coverThumb'

// Shown when a title has no poster (or its image fails to load): the Mediarium
// mark on a soft gradient, with no text, so every card stays the same shape.
export function PosterFallback() {
  return (
    <div className="poster-fallback" aria-hidden="true">
      <BrandMark className="" />
    </div>
  )
}

// The same for a record sleeve: a record on the soft gradient.
export function MusicFallback() {
  return (
    <div className="poster-fallback music-fallback" aria-hidden="true">
      <span className="vinyl-mini" />
    </div>
  )
}

export type CardKind = 'movie' | 'tv' | 'music' | 'book' | 'audiobook'

// The states that are still on the way get a dashed outline in the card's
// colour (solid means it's in, dashed means it's wanted), and a download gets
// a live line along the bottom of the picture.
const WANTED = new Set(['searching', 'pending', 'failed'])

// Each kind of media has its own shape: a movie is a ticket (notches and a
// tear line, drawn in CSS), a show is a TV set (all CSS), an album has
// its record peeking out of the sleeve, a book has a ribbon and an audiobook
// wears headphones. These are the extra pieces the CSS can't draw alone.
function ShapeParts({ kind }: { kind?: CardKind }) {
  if (kind === 'music') return <span className="pcard-disc" aria-hidden="true" />
  if (kind === 'book') return <span className="pcard-bookmark" aria-hidden="true" />
  if (kind === 'audiobook')
    return (
      <span className="pcard-phones" aria-hidden="true">
        <svg viewBox="0 0 100 50" preserveAspectRatio="none">
          <path className="band" d="M8 50C8 6 92 6 92 50" />
          <path className="band-mid" d="M8 50C8 6 92 6 92 50" />
          <path className="band-line" d="M8 50C8 6 92 6 92 50" />
        </svg>
        <span className="cup left" />
        <span className="cup right" />
      </span>
    )
  return null
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
// On a long page of lists (Discover) the posters far down are fetched in the background a moment after the
// page opens, at low priority, instead of only when they are about to scroll into view, so they are there
// when you arrive. A connection set to save data keeps the usual wait-until-near behaviour.
const EagerImages = createContext(false)
export function EagerImagesProvider({ children, afterMs = 1500 }: { children: ReactNode; afterMs?: number }) {
  const [on, setOn] = useState(false)
  useEffect(() => {
    const saver = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData
    if (saver) return
    const timer = window.setTimeout(() => setOn(true), afterMs)
    return () => window.clearTimeout(timer)
  }, [afterMs])
  return <EagerImages.Provider value={on}>{children}</EagerImages.Provider>
}

// A TMDB poster comes in several sizes. A card is a couple of hundred pixels wide, so the
// browser gets the 342 wide one, and the 500 wide one only on a sharp screen.
// The address is TMDB's own, or this server's copy of it (/api/img/tmdb/w500/...).
const W500 = /\/(t\/p|api\/img\/tmdb)\/w500\//
function cardPoster(url: string, width: number): string {
  return url.replace(W500, `/$1/w${width}/`)
}
function cardSrcSet(url: string): string | undefined {
  return W500.test(url) ? `${cardPoster(url, 342)} 342w, ${url} 500w` : undefined
}

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
  scores,
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
  // IMDb, Rotten Tomatoes and Metacritic, when kept (#169): the card shows Rotten Tomatoes, or IMDb without it.
  scores?: OutsideScores
  kind?: CardKind
  ribbon?: CardRibbon
  progress?: { pct: number; label?: string }
}) {
  // Remember which address failed, so a poster that changes later (an import finishes) gets its own try.
  const [brokenSrc, setBrokenSrc] = useState<string | undefined>()
  const eager = useContext(EagerImages)
  // The card whose page is open beside the list (the tablet layout) is marked.
  const here = useLocation().pathname
  const current = !!to && !selection && to.split('?')[0] === here
  // A music cover on this server comes as a small thumbnail (the server keeps it), always at low priority so a
  // grid of them never holds up a click.
  const thumb = isMusicCover(poster)
  const image =
    poster && brokenSrc !== poster ? (
      thumb ? (
        <img src={coverThumb(poster)} width={THUMB_SIDE} height={THUMB_SIDE} alt="" loading={eager ? 'eager' : 'lazy'} fetchPriority="low" decoding="async" onError={() => setBrokenSrc(poster)} />
      ) : (
        <img src={cardPoster(poster, 342)} srcSet={cardSrcSet(poster)} sizes="(max-width: 700px) 45vw, 240px" alt="" loading={eager ? 'eager' : 'lazy'} fetchPriority={eager ? 'low' : undefined} decoding="async" onError={() => setBrokenSrc(poster)} />
      )
    ) : kind === 'music' || kind === 'audiobook' ? <MusicFallback /> : <PosterFallback />
  const wanted = !!state && WANTED.has(state.key)
  const live = state?.key === 'downloading'
  const pct = progress?.pct ?? (live ? Number(/(\d+)%/.exec(state.label)?.[1] ?? NaN) : NaN)
  const cls = ['pcard', dim && 'dim', selection?.selected && 'selected', current && 'pcard-current', kind && `kind-${kind}`, state && `st-${state.key}`, wanted && 'is-wanted', live && 'is-live'].filter(Boolean).join(' ')
  return (
    <article className={cls}>
      {wanted && (
        <svg className="pcard-dash" aria-hidden="true">
          <rect width="100%" height="100%" rx="12" />
        </svg>
      )}
      <div className="pcard-art">
        <ShapeParts kind={kind} />
        {selection ? (
          <button className="pcard-link pcard-select" onClick={(e) => selection.onToggle({ shiftKey: e.shiftKey })} aria-pressed={selection.selected} aria-label={t('Select {title}', { title })}>
            {image}
            <span className={`pcard-check${selection.selected ? ' on' : ''}`} aria-hidden="true">
              {selection.selected && <Icon name="check" size={16} />}
            </span>
          </button>
        ) : to ? (
          <Link to={to} className="pcard-link" aria-label={title} aria-current={current ? 'page' : undefined}>
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
          <span className="pcard-rating" title={t('Rating')}>
            <Icon name="star" size={11} /> {rating.toFixed(1)}
            {scores?.rottenTomatoes ? (
              <span className="pcard-score" title={t('Rotten Tomatoes, critics')}>
                RT {scores.rottenTomatoes}%
              </span>
            ) : scores?.imdb ? (
              <span className="pcard-score" title="IMDb">
                IMDb {scores.imdb.toFixed(1)}
              </span>
            ) : null}
          </span>
        ) : null}
        {ribbon && (
          <span className={`pcard-ribbon tone-${ribbon.tone}`} aria-label={ribbon.label}>
            {ribbon.label}
          </span>
        )}
        {live && <span className={`pcard-live${Number.isNaN(pct) ? ' unknown' : ''}`} style={Number.isNaN(pct) ? undefined : { width: `${Math.max(4, pct)}%` }} aria-hidden="true" />}
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
      <div className="pcard-body" data-admit={kind === 'movie' ? t('Admit one') : undefined}>
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
