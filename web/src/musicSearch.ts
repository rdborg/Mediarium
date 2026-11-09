import type { MusicHit, MusicSearchAnswer } from './api'

// The one music search (#143): artists, albums and songs in one list, best
// first. The pieces below are the plain logic of the Search page, kept apart so
// they can be tested: the filters, the answer's shape, and the two guards that
// stop a slow or repeated search from replacing a better answer.

export type MusicFilter = 'all' | 'artist' | 'album' | 'song'

export const MUSIC_FILTERS: MusicFilter[] = ['all', 'artist', 'album', 'song']

// asAnswer reads what /music/search?type=all gave. An older server (or the
// demo) answers a plain list, which is taken as the rows.
export function asAnswer(r: MusicSearchAnswer | MusicHit[] | null | undefined): MusicSearchAnswer {
  if (Array.isArray(r)) return { items: r }
  return { items: r?.items ?? [], note: r?.note }
}

// filterHits keeps the rows of one kind, in the order they came (best first).
export function filterHits(hits: MusicHit[], f: MusicFilter): MusicHit[] {
  return f === 'all' ? hits : hits.filter((h) => h.kind === f)
}

export function countHits(hits: MusicHit[]): Record<MusicFilter, number> {
  const n: Record<MusicFilter, number> = { all: hits.length, artist: 0, album: 0, song: 0 }
  for (const h of hits) n[h.kind]++
  return n
}

// hitKey identifies a row: the same album found as an album and through a song
// are two rows, so the kind and the song are part of it.
export function hitKey(h: MusicHit): string {
  return `${h.kind}:${h.mbid}:${h.song ?? ''}`
}

// sameSearch is whether two typed lines are the same search: case, extra
// spaces and accents do not matter.
export function searchKey(q: string): string {
  return q
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .join(' ')
}

// Gate lets only the newest request apply its answer: an answer that comes
// back after a newer search started (out of order) is dropped.
export class Gate {
  private latest = 0
  next(): number {
    return ++this.latest
  }
  current(id: number): boolean {
    return id === this.latest
  }
}

// AnswerMemo remembers recent answers, so the same words typed again give the
// same list at once instead of asking the music database a second time (which
// answers slower, and sometimes not at all, when asked in quick succession).
export class AnswerMemo {
  private entries = new Map<string, { at: number; answer: MusicSearchAnswer }>()
  private ttlMs: number
  private max: number
  private now: () => number
  constructor(ttlMs = 5 * 60_000, max = 20, now: () => number = () => Date.now()) {
    this.ttlMs = ttlMs
    this.max = max
    this.now = now
  }
  get(q: string): MusicSearchAnswer | undefined {
    const key = searchKey(q)
    const e = this.entries.get(key)
    if (!e) return undefined
    if (this.now() - e.at > this.ttlMs) {
      this.entries.delete(key)
      return undefined
    }
    return e.answer
  }
  // put keeps an answer that is complete: one with a note (a part of the search
  // failed) is not kept, so asking again can fill the gap.
  put(q: string, answer: MusicSearchAnswer): void {
    if (answer.note || answer.items.length === 0) return
    const key = searchKey(q)
    this.entries.delete(key)
    this.entries.set(key, { at: this.now(), answer })
    while (this.entries.size > this.max) {
      const oldest = this.entries.keys().next().value
      if (oldest === undefined) break
      this.entries.delete(oldest)
    }
  }
  clear(): void {
    this.entries.clear()
  }
}
