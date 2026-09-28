import type { ReactNode } from 'react'

// Simple marks for each notification service, drawn here in their usual
// brand colours (simplified shapes, not the official artwork).
export default function ServiceIcon({ type, size = 26 }: { type: string; size?: number }) {
  const box = (fill: string) => <rect width="24" height="24" rx="6" fill={fill} />
  let inner: ReactNode
  switch (type) {
    case 'telegram':
      inner = (
        <>
          <circle cx="12" cy="12" r="12" fill="#2AABEE" />
          <polygon points="18.5,6 5,11.2 9.4,12.9 10.6,17.4 13,14.4 16.3,16.9" fill="#fff" />
          <polygon points="18.5,6 9.4,12.9 10,15.6 10.6,14 " fill="#c8daea" />
        </>
      )
      break
    case 'discord':
      inner = (
        <>
          {box('#5865F2')}
          <path d="M18.2 7.3a11 11 0 0 0-2.7-.8l-.4.8a10 10 0 0 0-3 0l-.4-.8a11 11 0 0 0-2.7.8c-1.7 2.5-2.1 4.9-1.9 7.3a11 11 0 0 0 3.3 1.7l.7-1.1c-.4-.1-.8-.3-1.200-.6l.3-.2a7.900 7.900 0 0 0 6.800 0l.3.200c-.4.300-.8.500-1.200.600l.7 1.100a11 11 0 0 0 3.300-1.700c.3-2.800-.4-5.200-1.900-7.300z" fill="#fff" />
          <circle cx="9.500" cy="12.200" r="1.200" fill="#5865F2" />
          <circle cx="14.500" cy="12.200" r="1.200" fill="#5865F2" />
        </>
      )
      break
    case 'slack':
      inner = (
        <>
          {box('#fff')}
          <rect x="4" y="9.800" width="7" height="3" rx="1.500" fill="#36C5F0" />
          <rect x="9.800" y="13" width="3" height="7" rx="1.500" fill="#2EB67D" />
          <rect x="13" y="11.200" width="7" height="3" rx="1.500" fill="#ECB22E" />
          <rect x="11.200" y="4" width="3" height="7" rx="1.500" fill="#E01E5A" />
        </>
      )
      break
    case 'email':
      inner = (
        <>
          {box('#F5B544')}
          <rect x="4.500" y="7" width="15" height="10.500" rx="2" fill="#fff" />
          <path d="M5 8l7 5.500L19 8" fill="none" stroke="#F5B544" strokeWidth="1.600" strokeLinecap="round" strokeLinejoin="round" />
        </>
      )
      break
    case 'ntfy':
      inner = (
        <>
          {box('#338574')}
          <path d="M7 8.500l4 3.500-4 3.500" fill="none" stroke="#fff" strokeWidth="1.800" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M13 16h4.500" stroke="#fff" strokeWidth="1.800" strokeLinecap="round" />
        </>
      )
      break
    case 'gotify':
      inner = (
        <>
          {box('#3A7CC1')}
          <path d="M12 5.500a4.500 4.500 0 0 0-4.500 4.500v3.200L6 15v1h12v-1l-1.500-1.800V10A4.500 4.500 0 0 0 12 5.500z" fill="#fff" />
          <path d="M10.300 17.200a1.800 1.800 0 0 0 3.400 0z" fill="#fff" />
        </>
      )
      break
    case 'pushover':
      inner = (
        <>
          {box('#249DF1')}
          <text x="12" y="17" textAnchor="middle" fontFamily="Sora, sans-serif" fontWeight="700" fontSize="14" fill="#fff">
            P
          </text>
        </>
      )
      break
    default:
      // webhook and anything unknown
      inner = (
        <>
          {box('#7C6BEA')}
          <circle cx="8" cy="15.500" r="2" fill="#fff" />
          <circle cx="16" cy="15.500" r="2" fill="#fff" />
          <circle cx="12" cy="8" r="2" fill="#fff" />
          <path d="M12 10v2.500M11 12.500l-2 1.500M13 12.500l2 1.500" stroke="#fff" strokeWidth="1.400" strokeLinecap="round" fill="none" />
        </>
      )
  }
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ flex: '0 0 auto' }}>
      {inner}
    </svg>
  )
}
