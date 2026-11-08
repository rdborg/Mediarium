// Small pure helpers for the setup wizard: which folders to ask for, the
// docker-compose line to add for a folder that is not mapped, and which file
// naming style matches the media player someone uses. Only the translation
// helper is imported, so the node test runner can load it (see setupHelpers.test.ts).
import { t } from './i18n.ts'

export type FolderKind = 'movies' | 'tv' | 'music' | 'ebooks' | 'audiobooks' | 'downloads'

export interface FolderInfo {
  label: string
  // The container environment variable that sets this folder, and the
  // path used when it is not set.
  envVar: string
  fallback: string
  // A made-up folder on a Synology, only used as the example in a compose line.
  hostExample: string
}

export const FOLDER_INFO: Record<FolderKind, FolderInfo> = {
  movies: { label: t('Movies folder'), envVar: 'MOVIES_DIR', fallback: '/movies', hostExample: '/volume1/Media/Movies' },
  tv: { label: t('TV shows folder'), envVar: 'TV_DIR', fallback: '/tv', hostExample: '/volume1/Media/tv' },
  music: { label: t('Music folder'), envVar: 'MUSIC_DIR', fallback: '/music', hostExample: '/volume1/Media/Music' },
  ebooks: { label: t('Ebooks folder'), envVar: 'EBOOKS_DIR', fallback: '/ebooks', hostExample: '/volume1/Media/Ebooks' },
  audiobooks: { label: t('Audiobooks folder'), envVar: 'AUDIOBOOKS_DIR', fallback: '/audiobooks', hostExample: '/volume1/Media/Audiobooks' },
  downloads: { label: t('Downloads folder'), envVar: 'DOWNLOADS_DIR', fallback: '/downloads', hostExample: '/volume1/Media/downloads' },
}

/** The line to add under `volumes:` in a compose file, for example `- /volume1/Media/Music:/music`. */
export function composeLine(hostPath: string, containerPath: string): string {
  const host = hostPath.trim()
  const inside = containerPath.trim()
  // A space or # would end the value early in a plain YAML line, so quote it.
  const needsQuotes = /[\s#]/.test(host) || /[\s#]/.test(inside)
  return needsQuotes ? `- "${host}:${inside}"` : `- ${host}:${inside}`
}

/** The compose line for a folder with the example NAS path in front of it. */
export function composeLineFor(kind: FolderKind, containerPath: string): string {
  return composeLine(FOLDER_INFO[kind].hostExample, containerPath)
}

function segments(path: string): string[] {
  return path
    .trim()
    .split('/')
    .filter((s) => s !== '' && s !== '.')
}

/**
 * The deepest folder that holds all of the given folders, for example
 * /data for /data/Movies and /data/downloads. Empty when they only share
 * the root ("/movies" and "/downloads"), which means nothing common is mapped.
 */
export function commonParent(paths: string[]): string {
  const lists = paths.filter((p) => p.trim().startsWith('/')).map(segments)
  if (lists.length === 0) return ''
  const first = lists[0]
  let n = 0
  while (n < first.length - 1 && lists.every((l) => l.length - 1 > n && l[n] === first[n])) n++
  return n === 0 ? '' : '/' + first.slice(0, n).join('/')
}

/**
 * A folder name for another kind of media next to a sample folder, matching
 * its style: "Music" beside "Movies", "music" beside "movies".
 */
export function siblingName(kind: FolderKind, samplePath: string): string {
  const name = FOLDER_INFO[kind].fallback.slice(1)
  const last = segments(samplePath).pop() ?? ''
  const capital = last !== '' && last[0] === last[0].toUpperCase() && last[0] !== last[0].toLowerCase()
  return capital ? name[0].toUpperCase() + name.slice(1) : name
}

/** The path for a kind of media inside a parent folder, in the style of a sample folder. */
export function pathInside(parent: string, kind: FolderKind, samplePath: string): string {
  return `${parent.replace(/\/+$/, '')}/${siblingName(kind, samplePath)}`
}

/**
 * The folder that holds movies, TV and downloads together (for example /data)
 * and a sample folder to copy the naming style from. Empty when they are
 * mapped separately.
 */
export function coreParent(values: Partial<Record<FolderKind, string>>): { parent: string; sample: string } {
  const core = [values.movies, values.tv, values.downloads].filter((p): p is string => !!p && p.trim() !== '')
  return { parent: commonParent(core), sample: core[0] ?? '' }
}

// ---- Media types -----------------------------------------------------------

export interface Chosen {
  movies: boolean
  tv: boolean
  music: boolean
  ebooks?: boolean
  audiobooks?: boolean
  // The anime library, part of TV: never a kind of media on its own.
  anime?: boolean
  // The library lifecycle rules: not a kind of media either.
  lifecycle?: boolean
  // The Channel archiver can't be picked here (it needs a yt-dlp file and a ticked warning), so the wizard only points to Settings.
  archiver?: boolean
}

export const nothingChosen = (c: Chosen) => !c.movies && !c.tv && !c.music && !c.ebooks && !c.audiobooks
export const hasVideo = (c: Chosen) => c.movies || c.tv

/** The library folders to ask for, in order, ending with downloads. */
export function foldersToAsk(c: Chosen): FolderKind[] {
  const out: FolderKind[] = []
  if (c.movies) out.push('movies')
  if (c.tv) out.push('tv')
  if (c.music) out.push('music')
  if (c.ebooks) out.push('ebooks')
  if (c.audiobooks) out.push('audiobooks')
  out.push('downloads')
  return out
}

/** "movies", "movies and TV shows", "movies, TV shows and music", "music". */
export function mediaPhrase(c: Chosen): string {
  const parts: string[] = []
  if (c.movies) parts.push(t('movies'))
  if (c.tv) parts.push(t('TV shows'))
  if (c.music) parts.push(t('music'))
  if (c.ebooks) parts.push(t('ebooks'))
  if (c.audiobooks) parts.push(t('audiobooks'))
  if (parts.length <= 1) return parts[0] ?? ''
  return t('{items} and {last}', { items: parts.slice(0, -1).join(', '), last: parts[parts.length - 1] })
}

/** What the default quality applies to: "every movie", "every movie and show". Video only. */
export function qualityPhrase(c: Chosen): string {
  if (c.movies && c.tv) return t('every movie and show')
  if (c.movies) return t('every movie')
  if (c.tv) return t('every show')
  return ''
}

// ---- Media player and file names -------------------------------------------

export type MediaPlayer = 'plex' | 'jellyfin' | 'emby' | 'kodi' | 'other'

export const MEDIA_PLAYERS: { id: MediaPlayer; label: string }[] = [
  { id: 'plex', label: 'Plex' },
  { id: 'jellyfin', label: 'Jellyfin' },
  { id: 'emby', label: 'Emby' },
  { id: 'kodi', label: 'Kodi' },
  { id: 'other', label: t('Something else, or none') },
]

/**
 * The naming style that suits a media player. Jellyfin and Emby share one
 * style. Anything else gets the Plex layout, which nearly every player reads.
 */
export function namingPresetFor(player: MediaPlayer): string {
  switch (player) {
    case 'jellyfin':
    case 'emby':
      return 'jellyfin'
    case 'kodi':
      return 'kodi'
    default:
      return 'plex'
  }
}

/** The player to preselect for a media server already known to the app; undefined when there is none. */
export function playerForServer(kinds: string[]): MediaPlayer | undefined {
  for (const k of kinds) {
    if (k === 'plex' || k === 'jellyfin' || k === 'emby') return k
  }
  return undefined
}

// ---- Saving the folders ----------------------------------------------------

export type FolderValues = Partial<Record<FolderKind, string>>

/**
 * The folders to send to the server: only the ones asked for, filled in, and
 * different from what the app already uses. A folder left at the value the
 * container maps is not saved, so it keeps following the compose file.
 */
export function foldersToSave(asked: FolderKind[], typed: FolderValues, inUse: FolderValues): FolderValues {
  const out: FolderValues = {}
  for (const k of asked) {
    const v = (typed[k] ?? '').trim()
    if (v !== '' && v !== (inUse[k] ?? '').trim()) out[k] = v
  }
  return out
}

/** True when the app is set to a folder other than the one the container maps. */
export function isStale(inUse: string | undefined, mapped: string | undefined): boolean {
  const a = (inUse ?? '').trim()
  const b = (mapped ?? '').trim()
  return a !== '' && b !== '' && a !== b
}
