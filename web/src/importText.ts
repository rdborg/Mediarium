// The words the import banner, the import page and its report use, kept apart
// from the screens so they can be tested. Everything here is plain text for a
// person to read.

// Only the fields these helpers read, so this file needs no other module.
export interface BatchCounts {
  kind: 'movie' | 'tv'
  elapsedMs: number // how long the import has run, by the server's clock
  total: number // titles being added
  done: number // of those, details finished
  added: number
  already: number
  problems: number
  monitor: boolean // the titles are watched for new episodes and better versions
  noUpgrade: boolean
  monitorMissing: boolean
}

export type ImportKind = 'movie' | 'tv'

// "movie", "movies", "show", "shows".
export function unitFor(kind: ImportKind, n: number): string {
  const one = kind === 'tv' ? 'show' : 'movie'
  return n === 1 ? one : one + 's'
}

export function problemWord(n: number): string {
  return n === 1 ? 'problem' : 'problems'
}

// "Importing your library: 4 of 25 shows."
export function progressText(b: BatchCounts): string {
  return `Importing your library: ${b.done} of ${b.total} ${unitFor(b.kind, b.total)}.`
}

// "Import finished. 25 shows added, 0 problems."
export function finishedText(b: BatchCounts): string {
  return `Import finished. ${b.added} ${unitFor(b.kind, b.added)} added, ${b.problems} ${problemWord(b.problems)}.`
}

// "25 shows added, 0 already in library, 0 problems"
export function totalLine(b: BatchCounts): string {
  return `${b.added} ${unitFor(b.kind, b.added)} added, ${b.already} already in library, ${b.problems} ${problemWord(b.problems)}`
}

// A rough time left, worked out from how fast the titles so far went. It says
// nothing until there is enough to go on.
export function etaText(b: BatchCounts): string {
  const elapsed = b.elapsedMs
  if (b.done < 2 || elapsed < 3000 || b.done >= b.total) return ''
  const left = (elapsed / b.done) * (b.total - b.done)
  const seconds = left / 1000
  if (seconds < 10) return 'Almost done.'
  if (seconds < 60) return `About ${Math.max(10, Math.round(seconds / 5) * 5)} seconds left.`
  const minutes = Math.ceil(seconds / 60)
  if (minutes < 90) return `About ${minutes} ${minutes === 1 ? 'minute' : 'minutes'} left.`
  return 'More than an hour left.'
}

// What Mediarium goes on to do once the import is done, for the choices made.
export function afterText(kind: ImportKind, monitor: boolean, monitorMissing: boolean): string {
  if (kind === 'movie') {
    return monitor
      ? "Mediarium may replace movies that are below your quality profile's target with a better version."
      : 'Nothing will be downloaded. Your files stay where they are.'
  }
  if (monitor && monitorMissing) {
    return "Episodes you are missing will be searched for and downloaded, including new ones as they come out. Mediarium may also replace episodes that are below your quality profile's target."
  }
  if (monitorMissing) {
    return 'Episodes you are missing will be searched for and downloaded, including new ones as they come out. The episodes you have are left alone.'
  }
  if (monitor) {
    return "New episodes will be downloaded as they come out, and Mediarium may replace episodes that are below your quality profile's target. Episodes you are missing now are left alone."
  }
  return 'Nothing will be downloaded for these shows. Your files stay where they are.'
}

// The short version for the banner: what starts once the details are in
// (or, for a finished import, what Mediarium does from now on).
export function upNextText(b: BatchCounts, finished = false): string {
  const wanted: string[] = []
  if (b.kind === 'tv' && b.monitorMissing) wanted.push('the episodes you are missing')
  else if (b.kind === 'tv' && b.monitor) wanted.push('new episodes')
  if (b.monitor && !b.noUpgrade) wanted.push('better versions of what you already have')
  if (wanted.length === 0) return finished ? 'They are not monitored, so nothing will be downloaded.' : 'Nothing will be downloaded.'
  const list = wanted.join(' and ')
  return finished ? `From now on, Mediarium looks for ${list}.` : `When it's done, Mediarium starts looking for ${list}.`
}

// What the report says was set on the titles of a finished import.
export function setText(b: BatchCounts): string {
  const these = b.kind === 'tv' ? 'shows' : 'movies'
  if (!b.monitor && !(b.kind === 'tv' && b.monitorMissing)) {
    return `These ${these} were added without monitoring, and Mediarium leaves what you have alone. Nothing will be downloaded until you start monitoring them.`
  }
  return `These ${these} are monitored. ${afterText(b.kind, b.monitor, b.monitorMissing)}`
}
