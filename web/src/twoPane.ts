// Tablet layout for the Library: the list on the left and the open title,
// artist or book on the right. Which pages get the second pane, and where the
// list's own settings (tab, filters) ride along in the address, is decided here.
//
// No React in this file, so it can be tested on its own.
import { isLibraryTab, type LibraryTab } from './libraryTabs.ts'

// Wide enough for two panes (a portrait iPad up to a landscape one) and tall
// enough to leave room: a phone on its side is wide but short, and stays single.
export const TWO_PANE_QUERY = '(min-width: 768px) and (max-width: 1180px) and (min-height: 480px)'

// The marker that says "this title was opened from the Library list". Movie
// pages (/title/...) are shared with Discover, so they only get the list when
// it is there; series, books and artists only exist in the library.
const MARK = 'lib'

// The pages that can sit in the right pane, and the Library tab each belongs to
// when the address does not name one.
const DETAIL_PATHS: { prefix: string; tab: LibraryTab; needsMark: boolean }[] = [
  { prefix: '/title/', tab: 'movie', needsMark: true },
  { prefix: '/series/', tab: 'tv', needsMark: false },
  { prefix: '/book/', tab: 'ebook', needsMark: false },
  { prefix: '/music/artist/', tab: 'music', needsMark: false },
]

function detailOf(pathname: string) {
  return DETAIL_PATHS.find((d) => {
    if (!pathname.startsWith(d.prefix)) return false
    const rest = pathname.slice(d.prefix.length).replace(/\/$/, '')
    return rest.length > 0 && !rest.includes('/')
  })
}

// True when this page is shown with the Library list beside it.
export function paneSelected(pathname: string, search: string): boolean {
  const d = detailOf(pathname)
  if (!d) return false
  return !d.needsMark || new URLSearchParams(search).get(MARK) === '1'
}

// The Library tab that belongs to a page when the address does not name one.
export function paneDefaultTab(pathname: string): LibraryTab {
  return detailOf(pathname)?.tab ?? 'movie'
}

// The Library tab the list shows beside this page: the one in the address, else the page's own.
export function paneTab(pathname: string, search: string): LibraryTab {
  const asked = new URLSearchParams(search).get('kind')
  return isLibraryTab(asked) ? asked : paneDefaultTab(pathname)
}

// The address of an item that is opened from the list on a tablet: the list's own
// settings (`search`, as the list has it now) ride along, so refresh and Back bring
// the same list back, and the mark says to show the list beside it.
export function openFromList(path: string, search: string): string {
  const next = new URLSearchParams(search)
  next.set(MARK, '1')
  return `${path}?${next.toString()}`
}

// The address that closes the right pane: the Library on the tab that was beside it, with the same filters.
export function closePaneTarget(pathname: string, search: string): string {
  const tab = paneTab(pathname, search)
  const next = new URLSearchParams(search)
  next.delete(MARK)
  next.delete('kind')
  // the first tab is left out of the address, like the Library does
  if (tab !== 'movie') next.set('kind', tab)
  const q = next.toString()
  return q ? `/library?${q}` : '/library'
}

export interface EscapeInfo {
  key: string
  defaultPrevented: boolean
  // The key was pressed in a text box, a pick list or something else that uses Esc itself.
  inField: boolean
  // A dialog, sheet or menu is open and Esc belongs to it.
  overlayOpen: boolean
}

// Esc closes the right pane, unless something else is using it.
export function escapeClosesPane(e: EscapeInfo): boolean {
  return e.key === 'Escape' && !e.defaultPrevented && !e.inField && !e.overlayOpen
}
