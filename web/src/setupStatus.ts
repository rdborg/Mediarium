// What is still missing after the first-run wizard: used by the last wizard
// step and by the "Getting started" card on the dashboard, so both always
// say the same thing. Pure functions, no React, so they can be tested.

import type { DashboardData } from './api'

export interface SetupFacts {
  indexers: number // enabled indexers
  usenetServers: number
  torrentsReady: boolean // torrent client on and a torrent indexer enabled
  mediaServers: number
  // Folders (of the kinds of media that are switched on) that are missing or
  // that Mediarium cannot write to.
  foldersBroken: string[]
}

export interface SetupGap {
  key: 'sources' | 'download' | 'folders' | 'server'
  title: string
  detail: string
  to: string
  button: string
  optional?: boolean
}

// Reads the facts out of the dashboard answer. kinds says which kinds of media
// are switched on: the folder of a kind that is off does not matter.
export function factsFromDashboard(d: DashboardData, kinds: { movies: boolean; tv: boolean }): SetupFacts {
  const relevant = (key: string) => key === 'downloads' || (key === 'movies' && kinds.movies) || (key === 'tv' && kinds.tv)
  return {
    indexers: d.setup.indexers,
    usenetServers: d.setup.usenetServers,
    torrentsReady: d.setup.torrentsReady === true,
    mediaServers: d.setup.mediaServers ?? 0,
    foldersBroken: d.folders.filter((f) => relevant(f.key) && (!f.exists || !f.writable)).map((f) => f.label),
  }
}

export function setupGaps(f: SetupFacts): SetupGap[] {
  const gaps: SetupGap[] = []
  if (f.indexers === 0) {
    gaps.push({
      key: 'sources',
      title: 'Add an indexer',
      detail: "Indexers are the sites Mediarium searches for releases. Without one it can't find anything.",
      to: '/settings/indexers',
      button: 'Add an indexer',
    })
  }
  if (f.usenetServers === 0 && !f.torrentsReady) {
    gaps.push({
      key: 'download',
      title: 'Add a download provider',
      detail: 'Without a Usenet provider, nothing can be downloaded.',
      to: '/settings/downloads',
      button: 'Add a Usenet provider',
    })
  }
  if (f.foldersBroken.length > 0) {
    gaps.push({
      key: 'folders',
      title: 'Fix your media folders',
      detail: `${joinWords(f.foldersBroken)} ${f.foldersBroken.length === 1 ? 'is' : 'are'} missing or cannot be written to.`,
      to: '/settings/media',
      button: 'Check the folders',
    })
  }
  if (f.mediaServers === 0) {
    gaps.push({
      key: 'server',
      title: 'Connect a media server',
      detail: 'Let Plex, Jellyfin or Emby know when new files arrive.',
      to: '/settings/media-servers',
      button: 'Connect a media server',
      optional: true,
    })
  }
  return gaps
}

// The gaps that stop Mediarium from working. A missing media server is not one.
export function requiredGaps(gaps: SetupGap[]): SetupGap[] {
  return gaps.filter((g) => !g.optional)
}

export function joinWords(items: string[]): string {
  if (items.length <= 1) return items.join('')
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`
}

// The browser remembers a dismissed card per account. Storage can be blocked,
// so every access is guarded and the card simply shows again.
const key = (userId: number | string) => `mediarium-started-dismissed-${userId}`

export function isDismissed(userId: number | string): boolean {
  try {
    return localStorage.getItem(key(userId)) === '1'
  } catch {
    return false
  }
}

export function dismiss(userId: number | string): void {
  try {
    localStorage.setItem(key(userId), '1')
  } catch {
    // best effort only
  }
}
