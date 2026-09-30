import type { IconName } from './Icon'
import type { QueueItem, TitleResult } from '../api'

// One vocabulary for "where is this title at" used by every poster, list row
// and search result, so Library, Wanted, Discover and search all say the same
// thing in the same colour.
export type ItemStateKey = 'downloaded' | 'downloading' | 'paused' | 'searching' | 'pending' | 'added' | 'failed' | 'partial' | 'details' | 'nodetails'

export interface ItemState {
  key: ItemStateKey
  label: string
  icon: IconName
  hint: string
}

const META: Record<ItemStateKey, Omit<ItemState, 'key'>> = {
  downloaded: { label: 'Downloaded', icon: 'check', hint: 'The file is in your library.' },
  downloading: { label: 'Downloading', icon: 'download', hint: 'Being downloaded right now.' },
  paused: { label: 'Paused', icon: 'pause', hint: 'The download is paused. Resume it from Activity.' },
  searching: { label: 'Waiting for a release', icon: 'search', hint: 'Monitored. Mediarium checks regularly and downloads a release as soon as it finds one.' },
  pending: { label: 'Pending', icon: 'clock', hint: 'Queued and waiting its turn.' },
  added: { label: 'Not monitored', icon: 'bookmark', hint: 'In your library but not monitored, so nothing is downloaded automatically.' },
  failed: { label: 'Failed', icon: 'warning', hint: 'The last download attempt failed.' },
  partial: { label: 'Partial', icon: 'download', hint: 'Some episodes are downloaded.' },
  details: { label: 'Getting details…', icon: 'refresh', hint: 'Mediarium is still getting the poster, summary and episodes for this title.' },
  nodetails: { label: 'Details missing', icon: 'warning', hint: "Mediarium couldn't get the details for this title yet. It will try again." },
}

export function describeState(key: ItemStateKey, extra?: string): ItemState {
  const m = META[key]
  return { key, label: extra ? `${m.label} ${extra}` : m.label, icon: m.icon, hint: m.hint }
}

// Movie state from the library row plus whatever the queue says about it.
export function movieState(m: { id: number; status: string; monitored?: boolean }, queue: QueueItem[]): ItemState {
  const q = queue.filter((x) => x.movieId === m.id && !x.seriesId)
  const running = q.find((x) => x.status === 'downloading' || x.status === 'importing')
  if (running) return describeState('downloading', `${Math.round(running.progressPct)}%`)
  if (q.some((x) => x.status === 'queued')) return describeState('pending')
  if (q.some((x) => x.status === 'paused')) return describeState('paused')
  if (m.status === 'downloaded') return describeState('downloaded')
  if (m.status === 'downloading') return describeState('downloading')
  const failed = q.find((x) => x.status === 'failed')
  if (failed && m.monitored !== false && !q.some((x) => x.status !== 'failed' && x.status !== 'completed')) return describeState('failed')
  if (m.monitored === false) return describeState('added')
  return describeState('searching')
}

export function seriesState(s: { id: number; episodeCount: number; downloadedCount: number; monitored: boolean }, queue: QueueItem[]): ItemState {
  const q = queue.filter((x) => x.seriesId === s.id)
  const running = q.find((x) => x.status === 'downloading' || x.status === 'importing')
  if (running) return describeState('downloading', `${Math.round(running.progressPct)}%`)
  if (q.some((x) => x.status === 'queued')) return describeState('pending')
  if (q.some((x) => x.status === 'paused')) return describeState('paused')
  if (s.episodeCount > 0 && s.downloadedCount >= s.episodeCount) return describeState('downloaded')
  if (s.downloadedCount > 0) return describeState('partial', `${s.downloadedCount}/${s.episodeCount}`)
  if (!s.monitored) return describeState('added')
  return describeState('searching')
}

// A title an import registered but has not finished getting details for.
// Undefined when it has (or never needed to).
export function detailsState(t: { detailsState?: string; detailsNote?: string }): ItemState | undefined {
  if (t.detailsState === 'pending') return describeState('details')
  if (t.detailsState === 'problem') return { ...describeState('nodetails'), hint: t.detailsNote || describeState('nodetails').hint }
  return undefined
}

export function progressFor(queue: QueueItem[], match: (q: QueueItem) => boolean): QueueItem | undefined {
  return queue.find((q) => match(q) && (q.status === 'downloading' || q.status === 'importing'))
}

// What Discover and search remember about a title you already own.
export interface ItemStateInput {
  id: number
  state: ItemState
}

// State of a search result that is already in the library (undefined when it is new).
export function resultState(r: TitleResult): ItemState | undefined {
  if (!r.inLibrary) return undefined
  if (r.status === 'downloaded') return describeState('downloaded')
  if (r.status === 'downloading') return describeState('downloading')
  return describeState('searching')
}
