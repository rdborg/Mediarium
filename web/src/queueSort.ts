// Sorting the download queue on the Activity page. "Default" is the order the
// page has always had (see queueView.ts); the other choices arrange the rows
// inside each section (In progress, Failed, Stopped...). The choice is kept in
// this browser. Speed and time left are not in the queue's data: the page works
// them out from how far each download got between two looks (transferRate.ts),
// and the same figures are used for the rows.
import type { QueueItem } from './api'
import { compareTitles } from './titleSort.ts'
import { nextSample, type RateSample } from './transferRate.ts'

export type QueueSortKey = 'default' | 'speed' | 'progress' | 'name' | 'size' | 'eta' | 'added'
export interface QueueSort {
  key: QueueSortKey
  reverse: boolean // the opposite of the key's own order
}

export const QUEUE_SORT_KEYS: readonly QueueSortKey[] = ['default', 'speed', 'progress', 'name', 'size', 'eta', 'added']
export const DEFAULT_QUEUE_SORT: QueueSort = { key: 'default', reverse: false }

export interface Pace {
  rate: number // bytes a second
  etaSec: number
}

// How much of a download is done, in bytes, and how big it is, for working out
// its speed: the download itself, or the archive being unpacked.
export function transferBasis(q: QueueItem): { running: boolean; size: number; done: number } {
  // Unpacking is timed from the bytes of the archive read so far (one archive;
  // with several the figures start over for each).
  const unpacking = q.status === 'importing' && !!q.unpack && q.unpack.totalBytes > 0 && q.unpack.count === 1
  const running = (q.status === 'downloading' && q.sizeBytes > 0) || unpacking
  const size = unpacking ? q.unpack!.totalBytes : q.sizeBytes
  const done = unpacking ? q.unpack!.doneBytes : (q.sizeBytes * q.progressPct) / 100
  return { running, size, done }
}

// paceOf turns a sample into speed and time left, or null while there is none.
export function paceOf(basis: { running: boolean; size: number; done: number }, sample: RateSample | null): Pace | null {
  if (!basis.running || !sample || sample.rate <= 0) return null
  return { rate: sample.rate, etaSec: (basis.size - basis.done) / sample.rate }
}

// nextSamples is the page's memory between two looks at the queue: one sample
// per running download, and none for the rest.
export function nextSamples(prev: ReadonlyMap<number, RateSample>, items: readonly QueueItem[], now: number): Map<number, RateSample> {
  const out = new Map<number, RateSample>()
  for (const q of items) {
    const b = transferBasis(q)
    if (b.running) out.set(q.id, nextSample(prev.get(q.id) ?? null, b.done, now))
  }
  return out
}

const time = (s?: string) => {
  const n = s ? Date.parse(s) : NaN
  return Number.isNaN(n) ? 0 : n
}

// A number to compare by, where a larger one comes first in the key's own
// order; undefined when the download has none (it then goes last, whichever way
// the list is turned).
function value(key: QueueSortKey, q: QueueItem, pace: ReadonlyMap<number, Pace>): number | undefined {
  switch (key) {
    case 'speed':
      return pace.get(q.id)?.rate
    case 'eta': {
      const e = pace.get(q.id)?.etaSec
      return e === undefined ? undefined : -e // the soonest is the largest
    }
    case 'progress':
      return q.progressPct
    case 'size':
      return q.sizeBytes > 0 ? q.sizeBytes : undefined
    case 'added':
      return time(q.addedAt) || undefined
    default:
      return undefined
  }
}

// sortQueueBy arranges a list. 'default' leaves it as it came. Otherwise the
// key's own order is: fastest, most done, A to Z, largest, finishing soonest,
// newest; reverse turns it round. Ties keep the order the list had, and
// downloads with nothing to compare (not running, for speed) come last.
export function sortQueueBy(items: readonly QueueItem[], sort: QueueSort, pace: ReadonlyMap<number, Pace> = new Map()): QueueItem[] {
  if (sort.key === 'default') return [...items]
  const dir = sort.reverse ? -1 : 1
  return [...items].sort((a, b) => {
    if (sort.key === 'name') return dir * compareTitles(a.title, b.title)
    const va = value(sort.key, a, pace)
    const vb = value(sort.key, b, pace)
    if (va === undefined || vb === undefined) return va === vb ? 0 : va === undefined ? 1 : -1
    return dir * (vb - va)
  })
}

const KEY = 'mediarium.queue.sort'

export interface KeyValueStore {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

function browserStore(): KeyValueStore | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

// readQueueSort is the remembered choice, or the default when there is none or
// it can't be read.
export function readQueueSort(store: KeyValueStore | null = browserStore()): QueueSort {
  try {
    const raw = store?.getItem(KEY)
    if (!raw) return DEFAULT_QUEUE_SORT
    const v = JSON.parse(raw) as Partial<QueueSort> | null
    if (v && typeof v.key === 'string' && (QUEUE_SORT_KEYS as readonly string[]).includes(v.key)) return { key: v.key, reverse: v.reverse === true }
  } catch {
    // Not readable: the default is used.
  }
  return DEFAULT_QUEUE_SORT
}

// rememberQueueSort keeps the choice; it is not an error when that isn't possible.
export function rememberQueueSort(sort: QueueSort, store: KeyValueStore | null = browserStore()): void {
  try {
    store?.setItem(KEY, JSON.stringify(sort))
  } catch {
    // Not remembered; the sort still works.
  }
}
