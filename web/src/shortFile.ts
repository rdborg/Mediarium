import type { ShortFileNote } from './api.ts'
import { plural, t } from './i18n.ts'

// Files in the library that are far shorter than the movie or episode should
// be (a fake, a sample or a cut-off download). The server finds them from the
// length it read out of each file; this only words what it says and works out
// what the "Delete incomplete files and search again" action reaches.

// The chip on a movie or episode page: "Only 1 min long", or "Not a video
// (RAR archive)" for a release that was never unpacked.
export function shortFileText(note: ShortFileNote): string {
  if (note.archive) return t('Not a video ({kind} archive)', { kind: note.archive.toUpperCase() })
  return t('Only {n} min long', { n: note.minutes })
}

// What the chip's hover text says.
export function shortFileTip(note: ShortFileNote): string {
  if (note.archive) return t('This file is a {kind} archive that was never unpacked, saved under a video name. No player can open it.', { kind: note.archive.toUpperCase() })
  if (note.expected) return t('This file is {n} min long, but it should be about {expected} min. It may be a fake or a cut-off download.', { n: note.minutes, expected: note.expected })
  return t('This file is much shorter than a real episode or movie. It may be a fake or a cut-off download.')
}

// A title of the Library list, as far as this goes.
export interface RedoCandidate {
  kind: 'movie' | 'tv'
  id: number
  shortFiles: number // files of the title that look incomplete (a movie has at most one)
}

export interface RedoRequest {
  movieIds: number[]
  seriesIds: number[]
  files: number // how many files that is
}

// The titles in the list that have a file that looks incomplete, and how many
// files that is. Only the list it is given is looked at, so a filtered
// Library reaches no more than it shows.
export function redoRequest(items: RedoCandidate[]): RedoRequest {
  const out: RedoRequest = { movieIds: [], seriesIds: [], files: 0 }
  for (const i of items) {
    if (i.shortFiles <= 0) continue
    out.files += i.shortFiles
    if (i.kind === 'movie') out.movieIds.push(i.id)
    else out.seriesIds.push(i.id)
  }
  return out
}

// What was done, in a sentence for a toast.
export function redoAnswerText(a: { files: number; titles: number; searching: number; skipped: number }): string {
  if (a.titles === 0) return t('Nothing was deleted. The files no longer look incomplete.')
  const done = plural(a.titles, 'Deleted the file of {n} title. It is wanted again.', 'Deleted the files of {n} titles. They are wanted again.')
  if (a.searching === 0) return done
  return `${done} ${plural(a.searching, 'Searching for {n} title now.', 'Searching for {n} titles now.')}`
}
