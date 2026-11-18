// What to say above or instead of a list of releases, from what the
// indexers answered. The old hint always blamed a missing indexer, even
// when one was set up and simply did not answer.
import { plural, t } from './i18n.ts'

export interface Unanswered {
  name: string
  message: string
}

export interface ReleaseHint {
  text: string
  // Whether the text ends with a link to Settings > Finding releases > Indexers & Search.
  link: boolean
  // True when there is nothing to list, so the hint stands in for the table.
  empty: boolean
}

// ignoredSamples are names of releases the indexers sent back that were left
// out because they are not this title (another film, or the same name in another
// year). With some and an empty list, "your indexers have nothing" would be
// untrue, so the hint says what came back instead.
export function releaseHint(sources: number, unanswered: Unanswered[], results: number, ignoredSamples: string[] = []): ReleaseHint | null {
  const names = unanswered.map((u) => u.name).join(', ')
  const n = unanswered.length
  if (results === 0) {
    if (sources === 0) {
      return { text: t('No indexer is set up yet. Add one under'), link: true, empty: true }
    }
    if (n >= sources) {
      const text = n === 1 ? t('The indexer did not answer: {names}. Check it under', { names }) : t('The indexers did not answer: {names}. Check them under', { names })
      return { text, link: true, empty: true }
    }
    if (n > 0) {
      return { text: plural(n, 'No matching releases. {n} indexer did not answer: {names}. Check it under', 'No matching releases. {n} indexers did not answer: {names}. Check them under', { names }), link: true, empty: true }
    }
    if (ignoredSamples.length > 0) {
      return { text: t('Your indexers answered with releases, but none of them fits this title and year. They sent, for example: {samples}', { samples: ignoredSamples.join('; ') }), link: false, empty: true }
    }
    return { text: t('No matching releases right now. Your indexers answered, but none of them has this yet.'), link: false, empty: true }
  }
  if (n > 0) {
    return { text: plural(n, '{n} indexer did not answer: {names}. The list may be missing some releases.', '{n} indexers did not answer: {names}. The list may be missing some releases.', { names }), link: false, empty: false }
  }
  return null
}

// skippedHint is the footnote about indexers a book search left out because
// they don't list books or audiobooks (a movie site, say). null when none were.
export function skippedHint(skipped: string[]): string | null {
  if (skipped.length === 0) return null
  return plural(
    skipped.length,
    "{n} indexer that doesn't do books or audiobooks was skipped: {names}.",
    "{n} indexers that don't do books or audiobooks were skipped: {names}.",
    { names: skipped.join(', ') },
  )
}

// bookReleaseHint is releaseHint for a book: when every indexer was left out,
// "no indexer is set up" would be wrong, so it says what is true.
export function bookReleaseHint(sources: number, unanswered: Unanswered[], results: number, skipped: string[]): ReleaseHint | null {
  if (sources === 0 && skipped.length > 0) {
    return { text: t('None of your indexers lists books or audiobooks. Check them under'), link: true, empty: true }
  }
  return releaseHint(sources, unanswered, results)
}
