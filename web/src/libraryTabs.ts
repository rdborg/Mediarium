// Which Library tabs there are and which one is open. The Library page shows
// them as tabs and the left menu lists them under "Library", so both ask here.
//
// No React in this file, so it can be tested on its own.

export type LibraryTab = 'movie' | 'tv' | 'anime' | 'music' | 'ebook' | 'audiobook'

// The switches in Settings, Media types, that decide which tabs exist.
export type LibraryModule = 'movies' | 'tv' | 'anime' | 'music' | 'ebooks' | 'audiobooks'

const ALL_TABS: readonly LibraryTab[] = ['movie', 'tv', 'anime', 'music', 'ebook', 'audiobook']

export function isLibraryTab(v: string | null | undefined): v is LibraryTab {
  return !!v && (ALL_TABS as readonly string[]).includes(v)
}

// The tabs that are switched on: movies, TV and music (whichever are on), then
// ebooks and audiobooks. Never empty.
export function libraryTabs(on: (key: LibraryModule) => boolean): LibraryTab[] {
  const video: LibraryTab[] = []
  if (on('movies')) video.push('movie')
  if (on('tv')) video.push('tv')
  // The anime library is part of TV: its own tab, right after TV.
  if (on('tv') && on('anime')) video.push('anime')
  if (on('music')) video.push('music')
  // With nothing on at all, the Library still opens on Movies.
  const videoKinds: LibraryTab[] = video.length > 0 ? video : ['movie']
  const booksOn = on('ebooks') || on('audiobooks')
  return [
    ...(on('movies') || on('tv') || on('music') || !booksOn ? videoKinds : []),
    ...(on('ebooks') ? (['ebook'] as const) : []),
    ...(on('audiobooks') ? (['audiobook'] as const) : []),
  ]
}

// The tab that is open: the one the address asks for (`?kind=tv`), Movies when
// it asks for none, and the first tab that is on when the one asked for is off.
export function activeLibraryTab(tabs: readonly LibraryTab[], kindParam: string | null | undefined): LibraryTab {
  const asked: LibraryTab = isLibraryTab(kindParam) ? kindParam : 'movie'
  return tabs.includes(asked) ? asked : tabs[0]
}

// The address that opens the Library on a tab.
export function libraryTabPath(tab: LibraryTab): string {
  return `/library?kind=${tab}`
}

export interface LibraryEntry {
  tab: LibraryTab
  to: string
  active: boolean
}

// What the left menu lists under "Library": a line for each tab that is on, and
// the one that is open (only while the Library page itself is showing).
export function libraryEntries(on: (key: LibraryModule) => boolean, pathname: string, search: string): LibraryEntry[] {
  const tabs = libraryTabs(on)
  const inLibrary = pathname === '/library' || pathname === '/library/'
  const open = inLibrary ? activeLibraryTab(tabs, new URLSearchParams(search).get('kind')) : null
  return tabs.map((tab) => ({ tab, to: libraryTabPath(tab), active: tab === open }))
}
