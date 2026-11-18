// The checks and wording for the file and folder permission boxes in
// Settings > Library and files > Folders. The server checks the same
// rules (internal/fileperms); this only gives the answer while typing.
import { t } from './i18n.ts'

export type ModeCheck = {
  /** A plain message for the box, or null when the text is fine (or empty). */
  problem: string | null
  /** True when the mode lets every user on the machine write. */
  worldWritable: boolean
  /** The three digits as a number, or null when empty or not valid. */
  value: number | null
}

/** Reads a mode such as 644, 0644 or 0o755. Empty text is "not set". */
export function checkMode(text: string, folder: boolean): ModeCheck {
  let v = text.trim()
  if (v === '') return { problem: null, worldWritable: false, value: null }
  v = v.replace(/^0o/i, '')
  if (v.length === 4 && v.startsWith('0')) v = v.slice(1)
  if (!/^[0-7]{3}$/.test(v)) {
    return { problem: t('Use three digits from 0 to 7, like 644 or 755.'), worldWritable: false, value: null }
  }
  const value = parseInt(v, 8)
  if (folder && (value & 0o700) !== 0o700) {
    return { problem: t('The first digit has to be 7, or Mediarium could not manage its own folders.'), worldWritable: false, value: null }
  }
  if (!folder && (value & 0o600) !== 0o600) {
    return { problem: t('The first digit has to be 6 or 7, or Mediarium could not manage its own files.'), worldWritable: false, value: null }
  }
  return { problem: null, worldWritable: (value & 0o002) !== 0, value }
}

/** 0o644 as rw-r--r-- (the way ls shows it). */
export function symbolic(value: number): string {
  const bits = 'rwxrwxrwx'
  let out = ''
  for (let i = 0; i < 9; i++) out += value & (1 << (8 - i)) ? bits[i] : '-'
  return out
}

/** What the umask in a container takes away from 666 (files) or 777 (folders), as a mode. */
export function modeFromUmask(umask: string, folder: boolean): number | null {
  const v = umask.trim()
  if (!/^[0-7]{1,4}$/.test(v)) return null
  return (folder ? 0o777 : 0o666) & ~parseInt(v, 8)
}
