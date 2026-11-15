import { t } from './i18n.ts'

export type ByHandKind = 'book' | 'music'

export interface ByHandForm {
  kind: ByHandKind
  title: string
  by: string // the author of a book, the artist of an album
  year: string
  externalId: string
  ebook: boolean
  audiobook: boolean
}

// The oldest year that is taken as a real one, and how far ahead an announced release may be.
const OLDEST = 1000
const AHEAD = 5

// What is wrong with the form, in words for the person; '' when it can be sent.
// The server checks the same things again.
export function checkByHand(f: ByHandForm, now = new Date()): string {
  if (!f.title.trim()) return f.kind === 'music' ? t('Type the title of the album.') : t('Type the title of the book.')
  if (f.kind === 'music' && !f.by.trim()) return t('Type the name of the artist.')
  const year = f.year.trim()
  if (year) {
    const y = Number(year)
    if (!Number.isInteger(y) || y < OLDEST || y > now.getFullYear() + AHEAD) return t("That year doesn't look right.")
  }
  const id = f.externalId.trim()
  if (id && !/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(id)) return t('An ID can only have letters, digits, dots, dashes and underscores.')
  if (f.kind === 'book' && !f.ebook && !f.audiobook) return t('Choose ebook, audiobook or both.')
  return ''
}
