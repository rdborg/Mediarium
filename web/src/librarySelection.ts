// Picking many titles on the Library page: which are ticked, what the bar
// above them says, and how a bulk answer is put in words. Kept apart from
// the screen so it can be tested. Everything here is plain text for a person.

// The only thing selecting needs to know about a card or row.
export interface Keyed {
  key: string
}

export function toggleKey(selected: ReadonlySet<string>, key: string): Set<string> {
  const next = new Set(selected)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  return next
}

// Shift-click: ticks everything between the last one you ticked (the anchor)
// and this one, in the order they are listed. Without an anchor, or when the
// anchor is no longer in the list, it is a plain tick.
export function selectRange(selected: ReadonlySet<string>, shown: readonly Keyed[], anchor: string | null, key: string): Set<string> {
  const to = shown.findIndex((i) => i.key === key)
  const from = anchor === null ? -1 : shown.findIndex((i) => i.key === anchor)
  if (to < 0 || from < 0) return toggleKey(selected, key)
  const [a, b] = from < to ? [from, to] : [to, from]
  const next = new Set(selected)
  for (const item of shown.slice(a, b + 1)) next.add(item.key)
  return next
}

export function selectAllKeys(shown: readonly Keyed[]): Set<string> {
  return new Set(shown.map((i) => i.key))
}

// The ticked items that are in the list, in list order. Passing the list the
// person can see (after filters and search) is what keeps a bulk action from
// reaching a title that is ticked but hidden.
export function chosenOf<T extends Keyed>(items: readonly T[], selected: ReadonlySet<string>): T[] {
  return items.filter((i) => selected.has(i.key))
}

export type Noun = 'movie' | 'show' | 'artist'

// "1 movie", "12 shows".
export function count(n: number, noun: Noun): string {
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

export interface SelectionText {
  // "12 selected", or "Nothing selected".
  count: string
  // The button that ticks everything in the view; empty when the view is empty.
  selectAll: string
  allSelected: boolean
  // Says what "select all in this view" reaches, with the numbers: the whole
  // library, or only what the filters let through.
  scope: string
}

// What the bar shows. `shown` is how many are in the view after the filters,
// `total` how many the library has of this kind. The library is not split
// into pages, so the view is all of it, filters aside.
export function selectionText(o: { selected: number; shown: number; total: number; noun: Noun }): SelectionText {
  const filtered = o.shown < o.total
  const allSelected = o.shown > 0 && o.selected >= o.shown
  let scope = ''
  if (o.shown > 0) {
    scope = filtered
      ? `Filters are on: this view has ${o.shown} of the ${count(o.total, o.noun)} in your library.`
      : `Everything in your library is in this view (${count(o.shown, o.noun)}).`
  }
  return {
    count: o.selected === 0 ? 'Nothing selected' : `${o.selected} selected`,
    selectAll: o.shown === 0 ? '' : allSelected ? `All ${o.shown} in this view are selected` : `Select all ${o.shown} in this view`,
    allSelected,
    scope,
  }
}

// "Alpha, Beta, Gamma and 9 more" for a question about removing.
export function nameList(names: readonly string[], max = 5): string {
  if (names.length <= max) {
    if (names.length <= 1) return names.join('')
    return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
  }
  return `${names.slice(0, max).join(', ')} and ${names.length - max} more`
}

export interface Failure {
  kind: string
  id: number
  title?: string
  reason: string
}

// One line for each thing that could not be done: "Alpha: the reason".
// `titleOf` finds a name for a title the server did not name.
export function failureLines(failed: readonly Failure[], titleOf: (kind: string, id: number) => string | undefined, max = 8): string[] {
  const lines = failed.slice(0, max).map((f) => `${f.title || titleOf(f.kind, f.id) || 'A title'}: ${f.reason}`)
  if (failed.length > max) lines.push(`And ${failed.length - max} more.`)
  return lines
}
