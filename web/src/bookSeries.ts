// The line under a book of a series: "Book 2 · 1998", or "Book 4 · out 1 Mar
// 2027" for one that isn't out yet. Plain functions, so they can be tested.

export interface SeriesEntryLike {
  position: string
  year?: number
  releaseDate?: string // "2027-03-01"
}

export function releaseLabel(date: string, locale?: string): string {
  const d = new Date(date + 'T00:00:00')
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(locale, { day: 'numeric', month: 'short', year: 'numeric' })
}

// localDate is a day as YYYY-MM-DD in this browser's time zone (toISOString
// would give the UTC day, a day off in the evening or early morning).
export function localDate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function entryMeta(e: SeriesEntryLike, today = localDate(new Date()), locale?: string): string {
  const ahead = e.releaseDate && e.releaseDate > today ? releaseLabel(e.releaseDate, locale) : ''
  const when = ahead ? `out ${ahead}` : e.year ? String(e.year) : ''
  return [`Book ${e.position}`, when].filter(Boolean).join(' · ')
}

// audioDetails is the line about an audiobook: "Read by Ray Porter · 16 h 10 min".
// More than three narrators (a full cast) are shortened.
export function audioDetails(narrators?: string, runtimeMin?: number): string {
  const names = (narrators ?? '')
    .split(',')
    .map((n) => n.trim())
    .filter(Boolean)
  const who = names.length === 0 ? '' : names.length > 3 ? `Read by ${names.slice(0, 3).join(', ')} and others` : `Read by ${names.join(', ')}`
  let length = ''
  if (runtimeMin && runtimeMin > 0) {
    const h = Math.floor(runtimeMin / 60)
    const m = runtimeMin % 60
    length = h > 0 ? `${h} h${m ? ` ${m} min` : ''}` : `${m} min`
  }
  return [who, length].filter(Boolean).join(' · ')
}
