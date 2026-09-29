import type { MediaServerKind } from '../api'

// Colours and a simple mark for each media server, used on buttons and cards.
export const MEDIA_SERVER_BRAND: Record<MediaServerKind, { label: string; color: string }> = {
  plex: { label: 'Plex', color: '#E5A00D' },
  jellyfin: { label: 'Jellyfin', color: '#AA5CC3' },
  emby: { label: 'Emby', color: '#52B54B' },
}

// A small round badge with the server's first letter in its brand colour
// (the real logos are trademarks we do not ship).
export function MediaServerMark({ kind, size = 22 }: { kind: MediaServerKind; size?: number }) {
  const b = MEDIA_SERVER_BRAND[kind]
  return (
    <span className="ms-mark" style={{ ['--ms' as string]: b.color, width: size, height: size, fontSize: size * 0.5 }} aria-hidden="true">
      {b.label[0]}
    </span>
  )
}
