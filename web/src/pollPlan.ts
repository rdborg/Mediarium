// How often a page asks the server again, and what it asks for. Kept apart from the pages so the choices can be
// tested: a page that polls for something that cannot change is wasted work on the server and on a phone's battery.

// A download in one of these states moves (progress, import, the next in line). Anything else only changes when a
// person or a job does something, so it can be looked at less often.
const MOVING = new Set(['queued', 'downloading', 'importing'])

export function anyMoving(items: ReadonlyArray<{ status: string }>): boolean {
  return items.some((i) => MOVING.has(i.status))
}

// The fast clock while something moves, the slow one when nothing does.
export function pollEvery(moving: boolean, fastMs: number, idleMs: number): number {
  return moving ? fastMs : idleMs
}

export type LibraryTab = 'movie' | 'tv' | 'anime' | 'music' | 'ebook' | 'audiobook'

// What one tick of the Library page reads: only the list of the tab that is shown. The other tabs' numbers come
// from the counts (movies and shows) or from one first read (artists and books), not from a read every few seconds.
export function libraryTick(active: LibraryTab): { movies: boolean; shows: boolean; artists: boolean; books: boolean } {
  return { movies: active === 'movie', shows: active === 'tv' || active === 'anime', artists: active === 'music', books: active === 'ebook' || active === 'audiobook' }
}

// Whether the background reading of file details still has work: a count of files read against the files to read.
// Nothing known yet counts as "still looking" so the first answer is asked for. Nothing to read (or all read) is final.
export function fileDetailsPending(p: { done: number; total: number } | null): boolean {
  if (p === null) return true
  return p.total > 0 && p.done < p.total
}

// Discover's "owned" marks: 30 seconds, 15 while a download of a movie or show moves.
export function ownedPollMs(queue: ReadonlyArray<{ status: string }> | undefined): number {
  return pollEvery(anyMoving(queue ?? []), 15000, 30000)
}
