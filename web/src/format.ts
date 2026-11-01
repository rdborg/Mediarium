import { locale, plural, t } from './i18n.ts'

// "just now", "5 min ago", "3 hours ago", "2 days ago", or the date for anything older.
export function timeAgo(iso: string | undefined): string {
  if (!iso) return ''
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const sec = Math.max(0, Math.round((Date.now() - then) / 1000))
  if (sec < 45) return t('just now')
  if (sec < 3600) return plural(Math.floor(sec / 60), '{n} min ago', '{n} min ago')
  if (sec < 86400) return plural(Math.floor(sec / 3600), '{n} hour ago', '{n} hours ago')
  if (sec < 7 * 86400) return plural(Math.floor(sec / 86400), '{n} day ago', '{n} days ago')
  return new Date(then).toLocaleDateString(locale())
}

// A title with what tells it from another show of the same name: "Shameless (UK)".
// The tag comes from the server and is only there when two titles share a name. A tag
// that is the year is left out when the screen shows the year next to the title anyway.
export function withNameTag(title: string, tag?: string, year?: number): string {
  if (!tag || (year && tag === String(year))) return title
  return `${title} (${tag})`
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
  // The decimal mark of the language in use: "1,5 GB" in German.
  return `${n.toLocaleString(locale(), { minimumFractionDigits: 1, maximumFractionDigits: 1, useGrouping: false })} ${units[i]}`
}

// A release name cut into pieces where it may wrap (after dots, dashes, underscores and brackets), so a long name breaks at a separator and not in the middle of a word.
export function nameParts(name: string): string[] {
  return name.match(/[^.\-_()[\]]*[.\-_()[\]]?/g)?.filter((p) => p !== '') ?? [name]
}

// A stored quality for display. A file whose name doesn't say is "Not stated"
// rather than an alarming "Unknown".
export function qualityText(q: string | undefined | null): string {
  if (!q) return '—'
  return q === 'Unknown' ? t('Not stated') : q
}
