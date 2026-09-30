import { api, ApiError, type DiscoverMovie } from './api'

export type Kind = 'movie' | 'tv'
export type ListName = 'trending' | 'popular' | 'upcoming' | 'similar'

// Where a Discover rail or page gets its titles: one of TMDB's lists, or a
// filtered browse (genre, years, order).
export interface Source {
  kind: Kind
  list: ListName | 'browse'
  genre?: string
  yearFrom?: string
  yearTo?: string
  sort?: string
  // "More like your library" only: also list titles from before the last 15 years.
  older?: boolean
}

export interface Page {
  results: DiscoverMovie[]
  totalPages: number
  totalResults?: number
}

// The lists older servers already had, used when /discover/list is missing.
async function legacyList(kind: Kind, list: ListName): Promise<DiscoverMovie[]> {
  if (list === 'trending') return kind === 'movie' ? api.discoverTrending() : api.discoverTrendingTV()
  if (list === 'popular') return kind === 'movie' ? api.discoverPopular() : api.discoverPopularTV()
  return []
}

export async function fetchPage(src: Source, page: number): Promise<Page> {
  if (src.list === 'browse') {
    return api.discoverBrowse({ kind: src.kind, genre: src.genre, yearFrom: src.yearFrom, yearTo: src.yearTo, sort: src.sort, page })
  }
  try {
    return await api.discoverList(src.kind, src.list, page, src.older)
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      return { results: page === 1 ? await legacyList(src.kind, src.list) : [], totalPages: 1 }
    }
    throw e
  }
}

export function sourceToQuery(src: Source): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(src)) if (v) q.set(k, String(v))
  return q.toString()
}

export function sourceFromQuery(q: URLSearchParams): Source {
  const kind = q.get('kind') === 'tv' ? 'tv' : 'movie'
  const list = (['trending', 'popular', 'upcoming', 'similar', 'browse'] as const).find((l) => l === q.get('list')) ?? 'popular'
  return {
    kind,
    list,
    genre: q.get('genre') ?? undefined,
    yearFrom: q.get('yearFrom') ?? undefined,
    yearTo: q.get('yearTo') ?? undefined,
    sort: q.get('sort') ?? undefined,
    older: q.get('older') === 'true' || undefined,
  }
}

const LIST_TITLE: Record<ListName, [string, string]> = {
  trending: ['Trending movies', 'Trending shows'],
  popular: ['Popular movies', 'Popular shows'],
  upcoming: ['Coming soon: movies', 'Coming soon: shows'],
  similar: ['More movies like your library', 'More shows like your library'],
}

export function sourceTitle(src: Source, genreName?: string): string {
  if (src.list !== 'browse') return LIST_TITLE[src.list][src.kind === 'movie' ? 0 : 1]
  const what = src.kind === 'movie' ? 'movies' : 'shows'
  const years = src.yearFrom && src.yearTo ? ` from ${src.yearFrom} to ${src.yearTo}` : src.yearFrom ? ` from ${src.yearFrom}` : src.yearTo ? ` up to ${src.yearTo}` : ''
  return `${genreName ? `${genreName} ${what}` : src.kind === 'movie' ? 'Movies' : 'Shows'}${years}`
}

// "Coming 12 Oct" for a date still ahead, otherwise nothing.
export function comingLabel(date?: string): string | undefined {
  if (!date) return undefined
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) return undefined
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return `Coming ${d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', ...(sameYear ? {} : { year: 'numeric' }) })}`
}
