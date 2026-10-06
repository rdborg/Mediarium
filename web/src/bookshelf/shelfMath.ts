// Small sums for the Books app: where a listener is in an audiobook, how far
// through it that is, and how times read.

export interface AudioPosition {
  track: number
  seconds: number
}

// An audiobook position is stored as "track:seconds" ("2:93.5").
export function parseAudioPosition(s: string | undefined): AudioPosition {
  const m = /^(\d+):(\d+(?:\.\d+)?)$/.exec((s ?? '').trim())
  if (!m) return { track: 0, seconds: 0 }
  return { track: Number(m[1]), seconds: Number(m[2]) }
}

export function formatAudioPosition(p: AudioPosition): string {
  return `${Math.max(0, Math.floor(p.track))}:${Math.max(0, Math.round(p.seconds * 10) / 10)}`
}

// How far through the whole book a place in one track is, in percent. Track
// lengths aren't known until each one loads, so file sizes stand in for them
// (good enough for audio at a steady bit rate).
export function overallPercent(sizes: number[], track: number, fraction: number): number {
  const total = sizes.reduce((a, b) => a + b, 0)
  if (total <= 0 || track < 0 || track >= sizes.length) return 0
  const before = sizes.slice(0, track).reduce((a, b) => a + b, 0)
  const f = Math.min(1, Math.max(0, Number.isFinite(fraction) ? fraction : 0))
  return Math.round(((before + sizes[track] * f) / total) * 1000) / 10
}

// 75 -> "1:15", 3725 -> "1:02:05".
export function formatClock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) seconds = 0
  const s = Math.floor(seconds)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = String(s % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`
}

export const SPEEDS = [0.8, 1, 1.2, 1.5, 1.75, 2]

// The next playback speed after cur, round to the start.
export function nextSpeed(cur: number): number {
  const i = SPEEDS.findIndex((s) => s > cur + 0.001)
  return i === -1 ? SPEEDS[0] : SPEEDS[i]
}

// "Tracks" named like "01 - Chapter One" read as "Chapter One"; a bare
// number reads as "Part 3".
export function trackLabel(name: string, index: number): string {
  const cleaned = name.replace(/^\s*\d+\s*[-–._)]\s*/, '').trim()
  if (cleaned === '' || /^\d+$/.test(cleaned)) return `Part ${index + 1}`
  return cleaned
}

// A chapter of an audiobook: a whole file, or a part of one when the file
// (an M4B) carries its own chapter marks. end is undefined when the chapter
// runs to the end of its file.
export interface Chapter {
  track: number
  start: number
  end?: number
  title: string
}

// buildChapters lists the book's chapters in order: the marks inside a file
// when it has two or more, otherwise one chapter per file.
export function buildChapters(tracks: { name: string; chapters?: { title: string; start: number }[] }[]): Chapter[] {
  const out: Chapter[] = []
  tracks.forEach((t, i) => {
    const marks = (t.chapters ?? []).filter((c) => Number.isFinite(c.start) && c.start >= 0).sort((a, b) => a.start - b.start)
    if (marks.length < 2) {
      out.push({ track: i, start: 0, title: trackLabel(t.name, i) })
      return
    }
    marks.forEach((c, j) => {
      out.push({ track: i, start: j === 0 ? 0 : c.start, end: marks[j + 1]?.start, title: c.title.trim() || `Chapter ${out.length + 1}` })
    })
  })
  return out
}

// chapterAt is the chapter playing at a place in a file.
export function chapterAt(chapters: Chapter[], track: number, seconds: number): number {
  let found = -1
  chapters.forEach((c, i) => {
    if (c.track === track && (found === -1 || c.start <= seconds + 0.25)) found = i
  })
  return Math.max(0, found)
}
