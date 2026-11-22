import { useId } from 'react'

// The small drawing for each kind of media, the same shapes the posters use:
// a ticket, a TV set, a record, a book with its ribbon, headphones. Drawn in
// the current text colour, so a parent sets the kind's colour.
//
// "line" is the plain outline for lists and chips. "solid" is the dimensional
// version: a body lit from the top with a darker edge and a highlight, for
// cards, the dashboard and anywhere the shape is the hero. The record is the
// one exception to "body in the kind's colour": it is always grey-blue vinyl
// with the colour on its label, the same as the record on music posters.
export type GlyphKind = 'movie' | 'tv' | 'music' | 'book' | 'audiobook' | 'other'

export default function KindGlyph({ kind, size = 28, variant = 'line' }: { kind: GlyphKind; size?: number; variant?: 'line' | 'solid' }) {
  const id = useId().replace(/:/g, '')
  const solid = variant === 'solid'
  const body = solid ? `url(#b${id})` : undefined
  return (
    <svg
      className={`kind-glyph g-${kind}${solid ? ' solid-glyph' : ''}`}
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      stroke="currentColor"
      strokeWidth={solid ? 1.4 : 1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {solid && (
        <defs>
          <linearGradient id={`b${id}`} x1="0" y1="0" x2="0.35" y2="1">
            <stop offset="0" className="g-top" />
            <stop offset="0.55" className="g-mid" />
            <stop offset="1" className="g-low" />
          </linearGradient>
          {kind === 'music' && (
            <linearGradient id={`v${id}`} x1="0" y1="0" x2="0.35" y2="1">
              <stop offset="0" className="v-top" />
              <stop offset="0.55" className="v-mid" />
              <stop offset="1" className="v-low" />
            </linearGradient>
          )}
        </defs>
      )}
      {kind === 'movie' && (
        <>
          <path className="tint" fill={body} d="M5 7h22a2 2 0 0 1 2 2v4a3 3 0 0 0 0 6v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4a3 3 0 0 0 0-6V9a2 2 0 0 1 2-2z" />
          <path className="edge" d="M21 9.5v13" strokeDasharray="1.6 2.4" />
          {solid && <path className="shine" d="M6 9h14" />}
        </>
      )}
      {kind === 'tv' && (
        <>
          <path d="M11.5 3.5L16 7l4.5-3.5" />
          <rect className="tint" fill={body} x="3" y="7" width="26" height="20" rx="4.5" />
          <rect className="screen" x="6.5" y="10.5" width="15" height="13" rx="3" />
          <circle className="solid" cx="25.2" cy="13.5" r="1.4" />
          <circle cx="25.2" cy="19.5" r="1.4" />
          {solid && <path className="shine" d="M8.5 12.5l4-0" />}
        </>
      )}
      {kind === 'music' && (
        <>
          <circle className="tint vinyl" fill={solid ? `url(#v${id})` : undefined} cx="16" cy="16" r="13" />
          {solid && <circle className="groove" cx="16" cy="16" r="10.8" />}
          <circle className="groove" cx="16" cy="16" r="9" opacity="0.45" />
          {solid ? <circle className="label" fill={body} cx="16" cy="16" r="5.6" /> : <circle className="solid label" cx="16" cy="16" r="4" />}
          {solid && <circle className="hole" cx="16" cy="16" r="1.1" />}
          <path className="shine" d="M8.5 9.5a10 10 0 0 1 5-3" opacity="0.7" />
        </>
      )}
      {kind === 'book' && (
        <>
          <path className="tint" fill={body} d="M7 4h17a2 2 0 0 1 2 2v22H9.5A2.5 2.5 0 0 1 7 25.5z" />
          <path className="pages" d="M7 25.5A2.5 2.5 0 0 1 9.5 23H26" />
          <path className="solid" d="M18 4v9.5l2.5-2 2.5 2V4z" />
        </>
      )}
      {kind === 'other' && (
        <>
          <path className="tint" fill={body} d="M5 12h22v13a3 3 0 0 1-3 3H8a3 3 0 0 1-3-3z" />
          <rect x="3" y="5" width="26" height="7" rx="2.4" />
          <path className="solid" d="M13 17.5h6" />
        </>
      )}
      {kind === 'audiobook' && (
        <>
          <path d="M5.5 20v-3.5a10.5 10.5 0 0 1 21 0V20" />
          <rect className="tint" fill={body} x="3" y="18" width="7.5" height="10.5" rx="3.2" />
          <rect className="tint" fill={body} x="21.5" y="18" width="7.5" height="10.5" rx="3.2" />
        </>
      )}
    </svg>
  )
}
