// The wording and the filters of Settings > System > Logs and errors, kept apart
// from the page so they can be tested. Nothing here talks to the server.

export interface ProblemRow {
  id: number
  level: 'error' | 'warning'
  area: string
  areaLabel: string
  code: string
  title: string
  message: string
  explain?: string
  try?: string
  linkLabel?: string
  linkPath?: string
  detail?: string
  forTitle?: string
  forLink?: string
  downloadId?: number
  count: number
  firstAt: string
  lastAt: string
  read: boolean
  known: boolean
}

export interface ProblemCounts {
  errorsToday: number
  warningsToday: number
  errorsWeek: number
  warningsWeek: number
  unread: number
  unreadErrors24h: number
}

export interface ProblemList {
  items: ProblemRow[]
  total: number
  counts: ProblemCounts
  areas: { id: string; label: string }[]
  notifyOnProblems: boolean
  keepDays: number
}

export type RangePreset = 'any' | 'day' | 'week' | 'month' | 'custom'

export interface ProblemFilters {
  level: 'all' | 'error' | 'warning'
  area: string // '' for every area
  range: RangePreset
  from: string // yyyy-mm-dd, used when range is custom
  to: string
  text: string
  unreadOnly: boolean
}

export const NO_FILTERS: ProblemFilters = { level: 'all', area: '', range: 'any', from: '', to: '', text: '', unreadOnly: false }

export const RANGE_LABELS: Record<RangePreset, string> = {
  any: 'Any time',
  day: 'Last 24 hours',
  week: 'Last 7 days',
  month: 'Last 30 days',
  custom: 'Choose dates',
}

// The from and to values sent to the server for a range. A preset counts back
// from now (an exact time); chosen dates are plain days.
export function rangeBounds(f: Pick<ProblemFilters, 'range' | 'from' | 'to'>, now: Date = new Date()): { from?: string; to?: string } {
  const back = (ms: number) => new Date(now.getTime() - ms).toISOString()
  switch (f.range) {
    case 'day':
      return { from: back(24 * 3600e3) }
    case 'week':
      return { from: back(7 * 24 * 3600e3) }
    case 'month':
      return { from: back(30 * 24 * 3600e3) }
    case 'custom':
      return { from: f.from || undefined, to: f.to || undefined }
    default:
      return {}
  }
}

// The query string for the list and the text file: only what is filled in.
export function problemQuery(f: ProblemFilters, extra: Record<string, string | number> = {}, now: Date = new Date()): string {
  const q = new URLSearchParams()
  if (f.level !== 'all') q.set('level', f.level)
  if (f.area) q.set('area', f.area)
  const { from, to } = rangeBounds(f, now)
  if (from) q.set('from', from)
  if (to) q.set('to', to)
  if (f.text.trim()) q.set('q', f.text.trim())
  if (f.unreadOnly) q.set('unread', '1')
  for (const [k, v] of Object.entries(extra)) q.set(k, String(v))
  const s = q.toString()
  return s ? `?${s}` : ''
}

export function filtersActive(f: ProblemFilters): boolean {
  return f.level !== 'all' || !!f.area || f.range !== 'any' || !!f.text.trim() || f.unreadOnly
}

// "once", "3 times".
export function timesText(n: number): string {
  return n <= 1 ? 'once' : `${n} times`
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

// "just now", "2 minutes ago", "5 hours ago", "3 days ago".
export function agoText(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const sec = Math.max(0, Math.round((now - then) / 1000))
  if (sec < 60) return 'just now'
  if (sec < 3600) return `${plural(Math.round(sec / 60), 'minute')} ago`
  if (sec < 48 * 3600) return `${plural(Math.round(sec / 3600), 'hour')} ago`
  return `${plural(Math.round(sec / 86400), 'day')} ago`
}

// "Happened 14 times, last 2 minutes ago". A problem that happened once says only when.
export function happenedText(row: Pick<ProblemRow, 'count' | 'lastAt'>, now: number = Date.now()): string {
  const ago = agoText(row.lastAt, now)
  return row.count > 1 ? `Happened ${timesText(row.count)}, last ${ago}` : `Happened ${ago}`
}

// The exact time, for a tooltip.
export function exactTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

// The heading counts: "3 errors", "1 warning", "No errors".
export function countText(n: number, word: 'error' | 'warning'): string {
  return n === 0 ? `No ${word}s` : plural(n, word)
}

// The text copied from one problem: what happened, what to try and the detail.
export function problemText(row: ProblemRow, now: number = Date.now()): string {
  const lines = [`${row.level === 'error' ? 'Error' : 'Warning'}: ${row.title} (${row.areaLabel}, ${row.code})`, happenedText(row, now)]
  if (row.message && row.message !== row.title) lines.push(row.message)
  if (row.forTitle) lines.push(`For: ${row.forTitle}`)
  if (row.explain) lines.push('', 'What happened:', row.explain)
  if (row.try) lines.push('', 'What to try:', row.try)
  if (row.detail) lines.push('', 'Technical detail:', row.detail)
  return lines.join('\n')
}

// The line under the cards: how many rows are showing.
export function showingText(shown: number, total: number): string {
  if (total === 0) return ''
  if (shown >= total) return total === 1 ? '1 problem' : `${total} problems`
  return `Showing ${shown} of ${total}`
}

// The message shown when nothing matches. With filters on, it says so.
export function emptyText(filtered: boolean, keepDays: number): string {
  return filtered ? 'Nothing matches these filters.' : `No problems in the last ${keepDays} days.`
}
