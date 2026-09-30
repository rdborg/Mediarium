// "just now", "5 min ago", "3 hours ago", "2 days ago", or the date for anything older.
export function timeAgo(iso: string | undefined): string {
  if (!iso) return ''
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const sec = Math.max(0, Math.round((Date.now() - then) / 1000))
  if (sec < 45) return 'just now'
  if (sec < 3600) return `${Math.round(sec / 60)} min ago`
  if (sec < 86400) {
    const h = Math.round(sec / 3600)
    return `${h} ${h === 1 ? 'hour' : 'hours'} ago`
  }
  if (sec < 7 * 86400) {
    const d = Math.round(sec / 86400)
    return `${d} ${d === 1 ? 'day' : 'days'} ago`
  }
  return new Date(then).toLocaleDateString()
}

export function formatBytes(bytes: number): string {
  if (!bytes) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(1)} ${units[i]}`
}

// A stored quality for display. A file whose name doesn't say is "Not stated"
// rather than an alarming "Unknown".
export function qualityText(q: string | undefined | null): string {
  if (!q) return '—'
  return q === 'Unknown' ? 'Not stated' : q
}
