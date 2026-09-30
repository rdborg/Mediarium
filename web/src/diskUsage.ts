// The "Also delete everything on disk" choice when a title is removed. It is
// off unless the person ticks it, and it says how much would go.
import type { DiskUsage } from './api'
import type { ConfirmOptions } from './components/ConfirmProvider'
import { formatBytes } from './format.ts'

// "3 files, 11.0 GB", or a plain line when there is nothing on disk.
export function diskSummary(u: DiskUsage | null): string {
  if (!u) return ''
  if (u.files === 0) return 'no files'
  return `${u.files} ${u.files === 1 ? 'file' : 'files'}, ${formatBytes(u.bytes)}`
}

// The tick box for the remove question. noun is "movie", "show" or "artist".
// It starts unticked, says how much would go, and shows a red warning while it
// is ticked.
export function removeOption(noun: string, u: DiskUsage | null): NonNullable<ConfirmOptions['option']> {
  const what = noun === 'artist' ? "The artist's albums in your music folder" : 'The files in your library'
  let hint: string
  if (u && u.files === 0) hint = `There are no files in your library for this ${noun}.`
  else if (u) hint = `${what}: ${diskSummary(u)}. This cannot be undone.`
  else hint = `${what}. This cannot be undone.`
  return {
    label: 'Also delete everything on disk',
    hint,
    defaultChecked: false,
    warning: `The files of this ${noun} will be deleted for good. There is no undo.`,
  }
}
