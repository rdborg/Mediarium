import type { ReactNode } from 'react'
import { BRAND_ICONS } from './brandIcons'

// The mark for each notification service, centred on a rounded square in the
// service's colour. Discord, Telegram, Slack and ntfy use their real logos
// (Simple Icons); email, Gotify, Pushover and webhooks use plain symbols.
export default function ServiceIcon({ type, size = 26 }: { type: string; size?: number }) {
  const brand = BRAND_ICONS[type]
  let bg = '#7C6BEA'
  let glyph: ReactNode
  if (brand) {
    bg = brand.color
    // Simple Icons paths are drawn on a 24x24 grid; shrink to 60% and centre.
    glyph = <path d={brand.path} fill="#fff" transform="translate(4.8 4.8) scale(0.6)" />
  } else if (type === 'email') {
    bg = '#E8A33D'
    glyph = (
      <>
        <rect x="5" y="7" width="14" height="10" rx="2" fill="#fff" />
        <path d="M5.8 8.2 12 12.8l6.2-4.6" fill="none" stroke={bg} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      </>
    )
  } else if (type === 'gotify') {
    bg = '#3A7CC1'
    glyph = (
      <>
        <path d="M12 5.5a4.3 4.3 0 0 0-4.3 4.3v3.1L6.3 15v.9h11.4V15l-1.4-2.1V9.8A4.3 4.3 0 0 0 12 5.5z" fill="#fff" />
        <path d="M10.3 17a1.8 1.8 0 0 0 3.4 0z" fill="#fff" />
      </>
    )
  } else if (type === 'pushover') {
    bg = '#249DF1'
    glyph = (
      <path d="M9.5 6h3.6c2.4 0 4 1.5 4 3.7 0 2.3-1.7 3.8-4.1 3.8h-1.4L10.5 18H8.3zm2.4 5.6h1.2c1.1 0 1.9-.7 1.9-1.8 0-1-.7-1.7-1.8-1.7h-1.1z" fill="#fff" />
    )
  } else {
    glyph = (
      <>
        <circle cx="8" cy="15.5" r="2" fill="#fff" />
        <circle cx="16" cy="15.5" r="2" fill="#fff" />
        <circle cx="12" cy="8" r="2" fill="#fff" />
        <path d="M12 10v2.5M11 12.5l-2 1.5M13 12.5l2 1.5" stroke="#fff" strokeWidth="1.4" strokeLinecap="round" fill="none" />
      </>
    )
  }
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ flex: '0 0 auto', display: 'block' }}>
      <rect width="24" height="24" rx="6" fill={bg} />
      {glyph}
    </svg>
  )
}
