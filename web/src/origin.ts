import { t } from './i18n.ts'

// Where a title or a download came from (#190, #191): who added it, or which
// list or scheduled job did. Titles from before this was kept have none, and
// nothing is shown for them.

export interface Origin {
  kind: string
  name?: string // the list's name, or the job of a scheduled one (search, rss, announce)
  user?: { id: number; name: string } // the account behind it, when there is one
}

// "Added by Anna (manual)", "Added by Mediarium via list Trending"... An origin
// that is missing or of a kind this version does not know gives ''.
export function originText(o?: Origin | null): string {
  if (!o || !o.kind) return ''
  const user = o.user?.name ?? ''
  const name = o.name ?? ''
  switch (o.kind) {
    case 'manual':
      return user ? t('Added by {user} (manual)', { user }) : t('Added by hand')
    case 'search':
      return user ? t('Added by {user} via automated search', { user }) : t('Added via a search started by hand')
    case 'auto':
      if (name === 'rss') return t('Added by Mediarium via the RSS check')
      if (name === 'announce') return t('Added by Mediarium via an IRC announce channel')
      if (name === 'follow') return t('Added by Mediarium via a followed artist, author or series')
      return t('Added by Mediarium via automated search')
    case 'list':
      return name ? t('Added by Mediarium via list {name}', { name }) : t('Added by Mediarium via an import list')
    case 'import':
      return t('Added by Mediarium from a library folder scan')
    case 'api':
      return t('Added by an app through the API')
    case 'request':
      return user ? t('Added by Mediarium after {user} asked for it', { user }) : t('Added by Mediarium after a request')
    case 'watchlist':
      return user ? t("Added by Mediarium from {user}'s Plex watchlist", { user }) : t('Added by Mediarium from a Plex watchlist')
  }
  return ''
}

// albumOriginText is the line for an album on its artist's page: only when it
// came in another way than the artist did (added later by someone, found by
// following the artist), since most albums simply came with their artist.
export function albumOriginText(album?: Origin | null, artist?: Origin | null): string {
  const text = originText(album)
  return text && text !== originText(artist) ? text : ''
}
