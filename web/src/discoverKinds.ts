import type { IconName } from './components/Icon'
import type { MediaKind } from './ModulesContext'

// The kinds of media Discover is split into, in the order they appear. A new
// kind (ebooks, audiobooks) adds one entry here, its rails, and its options
// below; the tabs, the section headings and the filter row follow.
export interface DiscoverKind {
  label: string
  icon: IconName
  color: string
  // The choices of the Order drop-down for this kind (the first is the default).
  orders: { value: string; label: string }[]
}

const ORDER_POPULAR = { value: 'popular', label: 'Most popular' }
const ORDER_NEWEST = { value: 'newest', label: 'Newest first' }
const ORDER_OLDEST = { value: 'oldest', label: 'Oldest first' }

export const DISCOVER_KINDS: Record<MediaKind | 'book', DiscoverKind> = {
  movie: { label: 'Movies', icon: 'film', color: 'var(--c-movie)', orders: [ORDER_POPULAR, { value: 'rating', label: 'Highest rated' }, ORDER_NEWEST, ORDER_OLDEST] },
  tv: { label: 'TV shows', icon: 'tv', color: 'var(--c-tv)', orders: [ORDER_POPULAR, { value: 'rating', label: 'Highest rated' }, ORDER_NEWEST, ORDER_OLDEST] },
  music: { label: 'Music', icon: 'music', color: 'var(--c-music)', orders: [ORDER_POPULAR, ORDER_NEWEST, ORDER_OLDEST] },
  book: { label: 'eBooks & Audiobooks', icon: 'book', color: 'var(--c-book)', orders: [ORDER_POPULAR, { value: 'rating', label: 'Highest rated' }, ORDER_NEWEST, ORDER_OLDEST] },
}

// Orders every kind understands, for the All tab.
export const SHARED_ORDERS = [ORDER_POPULAR, ORDER_NEWEST, ORDER_OLDEST]
