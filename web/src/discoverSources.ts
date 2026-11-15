import { api, ApiError, type DiscoverMovie } from './api'
import { locale, t } from './i18n.ts'
import { browseTitle, languageName } from './discoverFilters.ts'

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
  // Original language (a code like "ko"), lowest rating and runtime bucket; movies and shows only.
  language?: string
  minRating?: string
  runtime?: string
  // Streaming service keys, separated by commas, whose titles are left out (not for "more like your library").
  hide?: string
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
    return api.discoverBrowse({ kind: src.kind, genre: src.genre, yearFrom: src.yearFrom, yearTo: src.yearTo, sort: src.sort, language: src.language, minRating: src.minRating, runtime: src.runtime, hide: src.hide, page })
  }
  try {
    return await api.discoverList(src.kind, src.list, page, src.older, src.hide)
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
    language: q.get('language') ?? undefined,
    minRating: q.get('minRating') ?? undefined,
    runtime: q.get('runtime') ?? undefined,
    hide: q.get('hide') ?? undefined,
    older: q.get('older') === 'true' || undefined,
  }
}

const LIST_TITLE: Record<ListName, [string, string]> = {
  trending: [t('Trending movies'), t('Trending shows')],
  popular: [t('Popular movies'), t('Popular shows')],
  upcoming: [t('Coming soon: movies'), t('Coming soon: shows')],
  similar: [t('More movies like your library'), t('More shows like your library')],
}

export function sourceTitle(src: Source, genreName?: string): string {
  if (src.list !== 'browse') return LIST_TITLE[src.list][src.kind === 'movie' ? 0 : 1]
  const movie = src.kind === 'movie'
  const what = genreName ? (movie ? t('{genre} movies', { genre: genreName }) : t('{genre} shows', { genre: genreName })) : movie ? t('Movies') : t('Shows')
  return browseTitle(what, src, src.language ? languageName(src.language) : undefined)
}

// "Coming 12 Oct" for a date still ahead, otherwise nothing.
export function comingLabel(date?: string): string | undefined {
  if (!date) return undefined
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) return undefined
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return t('Coming {date}', { date: d.toLocaleDateString(locale(), { day: 'numeric', month: 'short', ...(sameYear ? {} : { year: 'numeric' }) }) })
}
