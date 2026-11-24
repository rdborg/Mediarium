import { useEffect, useState } from 'react'
import { api, type WatchLink } from '../api'
import { t } from '../i18n'
import { MediaServerMark } from './mediaServerBrand'

// "Watch in Plex / Jellyfin / Emby" buttons for a title your media servers
// already have. Renders nothing when no server has it (or none is set up).
export default function WatchLinks({ tmdbId, kind }: { tmdbId: number; kind: 'movie' | 'tv' }) {
  const [links, setLinks] = useState<WatchLink[]>([])
  useEffect(() => {
    api
      .watchLinks(tmdbId, kind)
      .then(setLinks)
      .catch(() => setLinks([]))
  }, [tmdbId, kind])

  return (
    <>
      {links.map((l) => (
        <a key={l.serverId} className="btn-link primary-look watch-link" href={l.url} target="_blank" rel="noreferrer" title={t('Open in {name}', { name: l.name })}>
          <MediaServerMark kind={l.kind} size={18} /> {t('Watch in {name}', { name: l.name })}
          <span className="sr-only"> {t('(opens in a new tab)')}</span>
        </a>
      ))}
    </>
  )
}
