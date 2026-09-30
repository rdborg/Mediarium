// What to say above or instead of a list of releases, from what the
// indexers answered. The old hint always blamed a missing indexer, even
// when one was set up and simply did not answer.
export interface Unanswered {
  name: string
  message: string
}

export interface ReleaseHint {
  text: string
  // Whether the text ends with a link to Settings > Indexers & Search.
  link: boolean
  // True when there is nothing to list, so the hint stands in for the table.
  empty: boolean
}

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

export function releaseHint(sources: number, unanswered: Unanswered[], results: number): ReleaseHint | null {
  const names = unanswered.map((u) => u.name).join(', ')
  const them = unanswered.length === 1 ? 'it' : 'them'
  if (results === 0) {
    if (sources === 0) {
      return { text: 'No indexer is set up yet. Add one under', link: true, empty: true }
    }
    if (unanswered.length >= sources) {
      return { text: `${unanswered.length === 1 ? 'The indexer' : 'The indexers'} did not answer: ${names}. Check ${them} under`, link: true, empty: true }
    }
    if (unanswered.length > 0) {
      return { text: `No matching releases. ${count(unanswered.length, 'indexer', 'indexers')} did not answer: ${names}. Check ${them} under`, link: true, empty: true }
    }
    return { text: 'No matching releases right now. Your indexers answered, but none of them has this yet.', link: false, empty: true }
  }
  if (unanswered.length > 0) {
    return { text: `${count(unanswered.length, 'indexer', 'indexers')} did not answer: ${names}. The list may be missing some releases.`, link: false, empty: false }
  }
  return null
}
