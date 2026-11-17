import { t } from './i18n.ts'

// What "Add a download by hand" is for: a movie, a show's season or episode, an
// album, or one format of a book. Only pure choices live here; the dialog does
// the loading and the sending.

export type HandKind = 'movie' | 'show' | 'album' | 'book'
export type HandFormat = 'ebook' | 'audiobook'

export interface HandTarget {
  kind: HandKind
  movieId: number
  seriesId: number
  season: string // '' until chosen
  episode: string // '0' is the whole season
  albumId: number
  bookId: number
  format: HandFormat | ''
}

export const emptyTarget = (kind: HandKind): HandTarget => ({ kind, movieId: 0, seriesId: 0, season: '', episode: '0', albumId: 0, bookId: 0, format: '' })

// Lower case, letters and digits only, so "Don't Stop" is found by "dont stop".
export const norm = (s: string) => s.toLowerCase().replace(/['’]/g, '').replace(/[^a-z0-9]+/g, ' ').trim()

// The first `limit` items whose text contains what was typed.
export function matching<T>(items: T[], typed: string, text: (item: T) => string, limit = 200): T[] {
  const q = norm(typed)
  const out: T[] = []
  for (const it of items) {
    if (q && !norm(text(it)).includes(q)) continue
    out.push(it)
    if (out.length >= limit) break
  }
  return out
}

// The formats of a book the box can offer: the ones the book is kept in, in a
// media type that is switched on.
export function offeredFormats(book: { ebook: { wanted: boolean }; audiobook: { wanted: boolean } }, ebooksOn: boolean, audiobooksOn: boolean): HandFormat[] {
  const out: HandFormat[] = []
  if (ebooksOn && book.ebook.wanted) out.push('ebook')
  if (audiobooksOn && book.audiobook.wanted) out.push('audiobook')
  return out
}

// "Album (1999) · EP" for the list of an artist's albums.
export function albumOption(a: { title: string; year?: number; type?: string }): string {
  const bits = [a.title + (a.year ? ` (${a.year})` : '')]
  if (a.type && a.type !== 'album') bits.push(a.type.toUpperCase())
  return bits.join(' · ')
}

// What the server needs to know about the title, as form fields.
export function targetFields(g: HandTarget): [string, string][] {
  switch (g.kind) {
    case 'movie':
      return [['movieId', String(g.movieId)]]
    case 'show':
      return [
        ['seriesId', String(g.seriesId)],
        ['season', g.season],
        ['episode', g.episode],
      ]
    case 'album':
      return [['albumId', String(g.albumId)]]
    case 'book':
      return [
        ['bookId', String(g.bookId)],
        ['format', g.format],
      ]
  }
}

// The reason the title is not complete yet, or '' when it is.
export function targetProblem(g: HandTarget): string {
  switch (g.kind) {
    case 'movie':
      return g.movieId ? '' : t('Choose the movie this download is for.')
    case 'show':
      if (!g.seriesId) return t('Choose the show this download is for.')
      return g.season === '' ? t('Choose the season.') : ''
    case 'album':
      return g.albumId ? '' : t('Choose the album this download is for.')
    case 'book':
      if (!g.bookId) return t('Choose the book this download is for.')
      return g.format ? '' : t('Choose the format: ebook or audiobook.')
  }
}
