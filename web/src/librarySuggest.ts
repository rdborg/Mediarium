// The library titles the command palette offers as the person types (the
// server answers from its full-text index, GET /api/library/suggest). This
// file turns an answer into rows for the palette, with no React so it can be
// tested on its own.
import { t } from './i18n.ts'
import type { Command } from './commands.ts'

export type LibraryHitKind = 'movie' | 'tv' | 'book' | 'artist' | 'album'

export interface LibraryHit {
  kind: LibraryHitKind
  id: number
  tmdbId?: number // movies and shows
  title: string
  subtitle?: string // the author of a book, the artist of an album
  year?: number
}

// hitPath is the page of the item, or '' when the answer has no way to open it.
export function hitPath(h: LibraryHit): string {
  switch (h.kind) {
    case 'movie':
      return h.tmdbId ? `/title/${h.tmdbId}` : ''
    case 'tv':
      return `/series/${h.id}`
    case 'book':
      return `/book/${h.id}`
    case 'artist':
      return `/music/artist/${h.id}`
    case 'album':
      return `/music/album/${h.id}`
  }
  return ''
}

function kindLabel(kind: LibraryHitKind): string {
  switch (kind) {
    case 'movie':
      return t('Movie')
    case 'tv':
      return t('TV show')
    case 'book':
      return t('Book')
    case 'artist':
      return t('Artist')
    case 'album':
      return t('Album')
  }
  return ''
}

// hitHint is the dim text on the right: what it is and the year or the author.
export function hitHint(h: LibraryHit): string {
  const extra = h.subtitle || (h.year ? String(h.year) : '')
  return extra ? `${kindLabel(h.kind)} · ${extra}` : kindLabel(h.kind)
}

// hitCommands makes palette rows from an answer. Items that cannot be opened
// are left out.
export function hitCommands(hits: LibraryHit[]): Command[] {
  const out: Command[] = []
  for (const h of hits) {
    const to = hitPath(h)
    if (!to || !h.title) continue
    out.push({ id: `lib-${h.kind}-${h.id}`, label: h.title, hint: hitHint(h), to })
  }
  return out
}
