// Sorting and filtering for the lists of releases and subtitles: pure
// functions, so the table components only draw what they return.

export type ReleaseSortKey = 'release' | 'quality' | 'language' | 'score' | 'indexer' | 'size' | 'age' | 'seeders'
export type SortDir = 1 | -1

// The direction a column starts in when first clicked: biggest, best or
// highest first for numbers, A to Z for names.
export const FIRST_DIRECTION: Record<ReleaseSortKey, SortDir> = {
  release: 1,
  quality: -1,
  language: 1,
  score: -1,
  indexer: 1,
  size: -1,
  age: -1, // newest first
  seeders: -1,
}

export interface SortableRelease {
  title: string
  indexerName: string
  protocol: string
  sizeBytes: number
  resolution?: string
  source?: string
  seeders?: number
  // Percent of the download the tracker does not count (100 = freeleech).
  discount?: number
  formatScore?: number
  // The whole score the pick uses (formats + preferred terms + fix bonus);
  // lists from before it existed only have formatScore.
  score?: number
  best?: boolean
  language?: string
  publishDate?: string
}

// scoreOf is the score a release is shown and sorted by, undefined when the
// profile scores nothing.
export function scoreOf(r: { score?: number; formatScore?: number }): number | undefined {
  return r.score ?? r.formatScore
}

// publishedAt is when the release was posted, 0 when the indexer didn't say.
export function publishedAt(r: { publishDate?: string }): number {
  const t = r.publishDate ? Date.parse(r.publishDate) : NaN
  // The zero date some indexers send for "unknown" counts as none.
  return Number.isFinite(t) && t > 86_400_000 ? t : 0
}

// ageOf is how old a release is in a few characters ("5h", "3d", "2mo", "1y"), '' when unknown.
export function ageOf(r: { publishDate?: string }, now = Date.now()): string {
  const at = publishedAt(r)
  if (!at) return ''
  const minutes = Math.max(0, Math.floor((now - at) / 60_000))
  if (minutes < 60) return '<1h'
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h`
  const days = Math.floor(hours / 24)
  if (days < 60) return `${days}d`
  if (days < 730) return `${Math.floor(days / 30)}mo`
  return `${Math.floor(days / 365)}y`
}

// resolutionOf is the number in "1080p" (2160 for 4K), 0 when there is none.
export function resolutionOf(r: { resolution?: string }): number {
  const res = (r.resolution ?? '').toLowerCase()
  if (res === '4k') return 2160
  const n = parseInt(res, 10)
  return Number.isFinite(n) ? n : 0
}

const text = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: 'base', numeric: true })

function compare(a: SortableRelease, b: SortableRelease, key: ReleaseSortKey): number {
  switch (key) {
    case 'release':
      return text(a.title, b.title)
    case 'quality':
      return resolutionOf(a) - resolutionOf(b)
    case 'language':
      return text(a.language ?? '', b.language ?? '')
    case 'score':
      return (scoreOf(a) ?? 0) - (scoreOf(b) ?? 0)
    case 'indexer':
      return text(a.indexerName, b.indexerName)
    case 'size':
      return a.sizeBytes - b.sizeBytes
    case 'age':
      // A larger value is newer; releases with no date sort as oldest.
      return publishedAt(a) - publishedAt(b)
    case 'seeders':
      // Usenet has no seeders: those sort as none at all.
      return (a.protocol === 'torrent' ? (a.seeders ?? 0) : -1) - (b.protocol === 'torrent' ? (b.seeders ?? 0) : -1)
  }
}

// sortReleases returns a new list ordered by key. Equal rows keep the order
// they came in (the best match first, as the server sent them).
export function sortReleases<T extends SortableRelease>(list: T[], key: ReleaseSortKey | null, dir: SortDir): T[] {
  if (!key) return list
  return list
    .map((item, i) => ({ item, i }))
    .sort((x, y) => compare(x.item, y.item, key) * dir || x.i - y.i)
    .map((x) => x.item)
}

// defaultOrder is how a list of releases starts, before a column is clicked:
// the release automatic search would take first (the server marks it), then,
// when the title's profile scores custom formats, the highest score first.
// Equal rows keep the order the server sent them. Without scores and without a
// marked release the list is left as it is.
export function defaultOrder<T extends SortableRelease>(list: T[]): T[] {
  const scored = list.some((r) => scoreOf(r) !== undefined)
  if (!scored && !list.some((r) => r.best)) return list
  return list
    .map((item, i) => ({ item, i }))
    .sort((x, y) => Number(!!y.item.best) - Number(!!x.item.best) || (scored ? (scoreOf(y.item) ?? 0) - (scoreOf(x.item) ?? 0) : 0) || x.i - y.i)
    .map((x) => x.item)
}

export interface ReleaseFilter {
  text: string
  indexer: string // '' for any
  quality: string // '' for any, 'other' for no resolution, else '1080p' and so on
  freeleech?: boolean // only releases the tracker counts as free to download
}

// isFreeleech is true for a release the tracker does not count against the
// ratio at all.
export const isFreeleech = (r: { discount?: number }): boolean => (r.discount ?? 0) >= 100

export const qualityOf = (r: { resolution?: string }): string => (resolutionOf(r) > 0 ? `${resolutionOf(r)}p` : 'other')

export function filterReleases<T extends SortableRelease>(list: T[], f: ReleaseFilter): T[] {
  const words = f.text.toLowerCase().split(/\s+/).filter(Boolean)
  return list.filter((r) => {
    if (f.indexer && r.indexerName !== f.indexer) return false
    if (f.quality && qualityOf(r) !== f.quality) return false
    if (f.freeleech && !isFreeleech(r)) return false
    const title = r.title.toLowerCase()
    return words.every((w) => title.includes(w))
  })
}

// The distinct values to offer in a filter, in a sensible order.
export function indexersIn<T extends SortableRelease>(list: T[]): string[] {
  return [...new Set(list.map((r) => r.indexerName))].sort(text)
}

export function qualitiesIn<T extends SortableRelease>(list: T[]): string[] {
  const found = new Set(list.map(qualityOf))
  const ordered = [...found].filter((q) => q !== 'other').sort((a, b) => parseInt(b, 10) - parseInt(a, 10))
  return found.has('other') ? [...ordered, 'other'] : ordered
}

// Subtitles: rating and fit are numbers, the rest are names.
export type SubtitleSortKey = 'release' | 'source' | 'rating' | 'fit'

export interface SortableSubtitle {
  release: string
  source?: string
  sourceName?: string // an add-on's own name
  rating: number
  score: number
}

// The site a subtitle comes from: its own name for an add-on. Empty when an
// add-on result has no name, so the caller can show a translated fallback.
export function subtitleSite(r: SortableSubtitle): string {
  if (r.source === 'subdl') return 'SubDL'
  if (r.source === 'addon') return r.sourceName ?? ''
  return 'OpenSubtitles'
}

// Rating and fit are only written when the source gave something.
export function subtitleRating(r: SortableSubtitle): string {
  return r.rating > 0 ? String(r.rating) : ''
}

export function subtitleFit(r: SortableSubtitle): string {
  const n = Math.round(r.score)
  return n > 0 ? String(n) : ''
}

export const SUBTITLE_FIRST_DIRECTION: Record<SubtitleSortKey, SortDir> = { release: 1, source: 1, rating: -1, fit: -1 }

export function sortSubtitles<T extends SortableSubtitle>(list: T[], key: SubtitleSortKey | null, dir: SortDir): T[] {
  if (!key) return list
  const cmp = (a: T, b: T) => {
    switch (key) {
      case 'release':
        return text(a.release, b.release)
      case 'source':
        return text(subtitleSite(a), subtitleSite(b))
      case 'rating':
        return a.rating - b.rating
      case 'fit':
        return a.score - b.score
    }
  }
  return list
    .map((item, i) => ({ item, i }))
    .sort((x, y) => cmp(x.item, y.item) * dir || x.i - y.i)
    .map((x) => x.item)
}
