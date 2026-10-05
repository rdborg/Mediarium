// Thin typed wrapper around the REST API in internal/api. Session auth is
// a plain HTTP-only cookie, so every call just needs
// credentials: 'include' — no token to manage client-side.

import { noteBusy, noteFine, startRead } from './busyState'
import { outsideService, slowServiceMessage } from './outsideService'
import { signalFor } from './requestSignal'
import type { Diagnostics } from './supportReport'
import type { ProblemCounts, ProblemList } from './problemsView'

const BASE = '/api'

export class ApiError extends Error {
  status: number
  // True when Mediarium said it is busy, or did not answer in time. The page
  // can then tell the person it is slow instead of showing an error.
  busy: boolean
  constructor(status: number, message: string, busy = false) {
    super(message)
    this.status = status
    this.busy = busy
  }
}

// A read that gets no answer at all in this long is given up on.
const READ_TIMEOUT_MS = 30_000
const SLOW_MESSAGE = 'Mediarium is slow to answer right now. Try again in a moment.'
const NO_ANSWER_MESSAGE = 'Could not reach Mediarium. Check that it is running, then try again.'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const isRead = !init?.method || init.method === 'GET'
  // A read that waits on TMDB, Open Library, an indexer and the like says
  // nothing about Mediarium being busy.
  const service = isRead ? outsideService(path) : ''
  const endRead = isRead && !service ? startRead() : undefined
  let res: Response
  let text: string
  try {
    res = await fetch(BASE + path, {
      credentials: 'include',
      cache: 'no-store',
      headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
      ...init,
      signal: signalFor(init?.signal, isRead ? READ_TIMEOUT_MS : undefined),
    })
    text = await res.text()
  } catch (e) {
    // The page cancelled this call itself (for example a search that was replaced): that says nothing about the server.
    if (init?.signal?.aborted) throw e
    if (e instanceof DOMException && (e.name === 'TimeoutError' || e.name === 'AbortError')) {
      if (service) throw new ApiError(0, slowServiceMessage(service))
      noteBusy()
      throw new ApiError(0, SLOW_MESSAGE, true)
    }
    // fetch reports "the server could not be reached" as a bare TypeError ("Failed to fetch").
    if (e instanceof TypeError) throw new ApiError(0, NO_ANSWER_MESSAGE)
    throw e
  } finally {
    endRead?.()
  }
  let data: { error?: string; busy?: boolean } | null = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    // Not JSON (for example a plain "404 page not found"): report the status instead of a parser error.
    if (res.ok) throw new ApiError(res.status, 'The server sent an unexpected reply.')
    throw new ApiError(res.status, res.status === 404 ? 'This is not available in this version yet.' : `Something went wrong on the server (error ${res.status}).`)
  }
  if (res.status === 503 && data?.busy) {
    noteBusy()
    throw new ApiError(503, data.error ?? SLOW_MESSAGE, true)
  }
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `Something went wrong on the server (error ${res.status}).`)
  }
  noteFine()
  return data as T
}

const get = <T>(path: string) => request<T>(path)
const post = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body !== undefined ? JSON.stringify(body) : undefined })
const put = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'PUT', body: body !== undefined ? JSON.stringify(body) : undefined })
const del = <T>(path: string) => request<T>(path, { method: 'DELETE' })

// One removal in the recycle bin (Activity > Recycle bin).
export interface TrashItem {
  // movies, tv or music, with -2, -3 for extra library folders
  kind: string
  id: string
  label: string
  deletedAt: string
  expiresAt?: string
  size: number
  files: number
  paths: string[]
}

export interface SavedBackups {
  auto: boolean
  keep: number
  folder: string
  items: { name: string; size: number; createdAt: string }[]
}

// A video file in the downloads folder that can be imported by hand.
export interface ManualFile {
  path: string
  size: number
  modified: string
  title?: string
  year?: number
  season?: number
  episode?: number
  quality?: string
}

export interface LibraryStats {
  movies: number
  moviesHave: number
  shows: number
  episodes: number
  episodesHave: number
  albums: number
  albumsHave: number
  movieBytes: number
  episodeBytes: number
  quality: { label: string; count: number; bytes?: number }[]
  months: { month: string; completed: number; failed: number; bytes: number }[]
  usenetShare: number
  historyKeptDays: number
}

export interface RenameItem {
  kind: 'movie' | 'episode'
  id: number
  title: string
  from: string
  to: string
}

export interface TrashList {
  days: number
  items: TrashItem[]
}

export interface OnboardingStatus {
  firstRunNeeded: boolean
  // True when the visitor is not on the home network, so creating the first
  // account needs the one-time setup code from the Mediarium log.
  setupCodeRequired?: boolean
}

export type Role = 'admin' | 'member'

export interface User {
  id: number
  username: string
  isAdmin?: boolean
  role?: Role
  name?: string
  email?: string
}

// True for administrators. Older servers sent only isAdmin, and a missing
// role is treated as admin so nothing disappears for a single-account setup.
export function isAdmin(u: User | null | undefined): boolean {
  if (!u) return false
  if (u.role) return u.role === 'admin'
  return u.isAdmin !== false
}

// An account as the administrator's account list shows it.
export interface Account {
  id: number
  username: string
  name: string
  email: string
  role: Role
  isAdmin: boolean
  createdAt: string
  lastLoginAt: string | null
}

export interface NewAccount {
  username: string
  password: string
  name?: string
  email?: string
  role: Role
}

export interface AccountChanges {
  name?: string
  email?: string
  role?: Role
  password?: string
}

// The name to greet someone by: their name, or their username when no name is set.
export function displayName(u: User | null | undefined): string {
  if (!u) return ''
  return (u.name ?? '').trim() || u.username
}

export interface APIKey {
  id: number
  name: string
  createdAt: string
  revokedAt?: string
  key?: string // only ever present in the response right after creation
}

// One line of the log a test message leaves.
export interface NotifyTestStep {
  time: string
  text: string
  ok: boolean
}

export interface Settings {
  moviesPath: string
  tvPath?: string
  downloadsPath: string
  namingPreset: string
  movieNameFormat: string
  hasTmdbApiKey: boolean
  traktClientId?: string
  hasTraktClientId: boolean
  onboardingDone: boolean
  torrentListenPort?: string
  torrentSeedRatioLimit?: string
  torrentSeedTimeLimitH?: string
  automationEnabled?: boolean
  defaultProfileId?: number
  defaultSources?: SourcePref
  qualityLanguage?: string
  hasOpenSubtitlesApiKey?: boolean
  hasOpenSubtitlesAccount?: boolean
  openSubtitlesAccountName?: string
  tmdbKeyBuiltIn?: boolean
  tmdbUsingOwnKey?: boolean
  openSubtitlesUsingOwnKey?: boolean
  traktUsingOwnKey?: boolean
  openSubtitlesKeyBuiltIn?: boolean
  traktClientIdBuiltIn?: boolean
  subtitleLanguages?: string[]
  subtitleAutoDownload?: boolean
  // The master switch for subtitles; off until switched on.
  subtitlesEnabled?: boolean
  illegalCharMode?: string
  illegalCharReplacement?: string
  requireVpnForTorrents?: boolean
  importConflictPolicy?: string
  torrentEnabled?: boolean
  flareSolverrUrl?: string
  // True in the "full" image, where the Cloudflare helper is built in.
  flareSolverrBundled?: boolean
  cleanupAuto?: boolean
  historyRetentionDays?: number
  trashDays?: number
  backupAuto?: boolean
  backupKeep?: number
  speedLimitMB?: number
  speedLimitHours?: string
  minFreeGB?: number
  notifyQuietHours?: string
  // How often, in minutes, the connection watch checks your providers and indexers; 0 is off.
  monitorIntervalMinutes?: number
  publicUrl?: string // the address Mediarium is opened at; links in notification messages use it
  huntIntervalHours?: number // hours between searches for missing items and better versions, 1 to 168
  releaseCheckMinutes?: number // minutes between checks of each indexer's newest releases, 5 to 1440
  downloadsAtOnce?: number // how many downloads run at the same time, 1 to 5 (the rest wait in line)
  legalAcknowledgedAt?: string
  musicEnabled?: boolean
  musicPath?: string
  musicDefaultProfileId?: number
  // Ebooks and audiobooks folders. Those modules are not built yet, so the
  // folders are only remembered (and shown greyed in Settings).
  ebooksPath?: string
  audiobooksPath?: string
  // What the container itself maps (MOVIES_DIR, TV_DIR, DOWNLOADS_DIR, MUSIC_DIR,
  // EBOOKS_DIR, AUDIOBOOKS_DIR). Read-only; the folder boxes start from these.
  containerFolders?: {
    movies: string
    tv: string
    downloads: string
    music: string
    ebooks: string
    audiobooks: string
  }
}

export interface IndexerConfig {
  id: number
  name: string
  definitionId: string
  baseUrl: string
  protocol: 'usenet' | 'torrent'
  enabled: boolean
  kind?: 'torznab' | 'newznab' | 'cardigann'
  lastTestError?: string
  lastTestAt?: string
  // 1 preferred, 2 normal, 3 last resort
  priority?: number
}

export interface IndexerDefinitionSetting {
  name: string
  label?: string
  type: string
  default?: string | boolean | number | null
  options?: { value: string; label: string }[]
}

export interface IndexerDefinition {
  id: string
  name: string
  description?: string
  type: 'public' | 'semi-private' | 'private' | string
  language?: string
  protocol: 'torrent' | 'usenet'
  links?: string[]
  settings?: IndexerDefinitionSetting[]
  supported?: boolean
  problem?: string
}

export interface UsenetServer {
  id: number
  name: string
  host: string
  port: number
  useSsl: boolean
  username?: string
  hasPassword: boolean
  connections: number
  priority: number
  enabled: boolean
}

export interface UsenetServerInput {
  name: string
  host: string
  port: number
  useSsl: boolean
  username?: string
  password?: string
  connections: number
  priority: number
  enabled: boolean
}

export interface DownloadsStatus {
  usenet: { servers: number; enabledServers: number; ready: boolean }
  torrent: {
    state?: 'disabled' | 'blocked' | 'ready'
    enabled?: boolean
    ready: boolean
    listenPort: number
    vpnRequired: boolean
    vpnConnected: boolean
    vpnState?: 'off' | 'connecting' | 'connected' | 'down'
    blockedByVpn: boolean
    // The engine only listens while a torrent is downloading or seeding.
    // incomingSeen is true once another peer has connected to us.
    listening?: boolean
    activePort?: number
    activeTorrents?: number
    incomingSeen?: boolean
    lastIncomingAt?: string
  }
}

export interface SearchResult {
  title: string
  indexerName: string
  protocol: 'usenet' | 'torrent'
  downloadUrl: string
  sizeBytes: number
  publishDate?: string
  resolution?: string
  source?: string
  codec?: string
  group?: string
  seeders?: number
  peers?: number
  season?: number
  episodes?: number[]
  blocklisted?: boolean
  rejections?: string[]
  // Which profile would take this release: the title's own, or one of its fallbacks.
  acceptedBy?: { profileId: number; profileName: string; fallback: boolean }
  // The audio languages the release names ("Italian + English", "Multi") and how
  // they suit the preferred language: ok, mixed (adds another language) or other
  // (clearly another language only, so automation skips it).
  language?: string
  languageFit?: 'ok' | 'mixed' | 'other'
}

export type MediaStatus = 'missing' | 'downloading' | 'downloaded'

export interface PreferredTerm {
  term: string
  score: number
}

export interface QualityProfileInput {
  name: string
  allowed: string[]
  cutoff: string
  upgradeAllowed: boolean
  mustContain: string[]
  mustNotContain: string[]
  preferred: PreferredTerm[]
  // Profiles to try, in order, when this one finds nothing acceptable.
  fallback?: number[]
  // The largest release accepted, in GB (0 = no limit).
  maxSizeGB?: number
}

export interface QualityProfile extends QualityProfileInput {
  id: number
  inUse: number
}

export type SourcePref = '' | 'usenet' | 'torrent' | 'both'

export interface TitleResult {
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterUrl?: string
  inLibrary: boolean
  libraryId?: number
  status?: string
  genres?: string[]
  rating?: number
}

export interface AddMovieOptions {
  profileId?: number
  monitored?: boolean
  sources?: SourcePref
  searchNow?: boolean
  // Leave it alone once it is downloaded. The server treats a missing value as true.
  noUpgrade?: boolean
}

export interface AddSeriesOptions {
  profileId?: number
  monitor?: 'all' | 'future' | 'none'
  sources?: SourcePref
  searchNow?: boolean
  noUpgrade?: boolean
}

export interface Series {
  id: number
  tags?: string[]
  sources?: SourcePref
  profileId?: number
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterUrl?: string
  monitored: boolean
  episodeCount: number
  downloadedCount: number
  genres?: string[]
  noUpgrade?: boolean // Mediarium does not look for better versions of what is there
  detailsState?: 'pending' | 'problem' // an import is still getting the details, or could not
  detailsNote?: string
}

export interface Episode {
  id: number
  season: number
  episode: number
  title?: string
  overview?: string
  airDate?: string
  status: MediaStatus
  quality?: string
  filePath?: string
  monitored: boolean
}

export interface SeriesDetail extends Series {
  episodes: Episode[]
}

export interface WantedItem {
  kind: 'movie' | 'episode'
  id: number
  movieId?: number
  tmdbId?: number
  seriesId?: number
  season?: number
  episode?: number
  title: string
  subtitle?: string
  date?: string
  quality?: string
  cutoff?: string
  profileName?: string
  lastSearch?: string
  lastSearchAt?: string
}

export interface Movie {
  id: number
  tags?: string[]
  sources?: SourcePref
  profileId?: number
  monitored?: boolean
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterUrl?: string
  status: 'missing' | 'downloading' | 'downloaded'
  quality?: string
  filePath?: string
  genres?: string[]
  noUpgrade?: boolean // Mediarium does not look for better versions of what is there
  detailsState?: 'pending' | 'problem' // an import is still getting the details, or could not
  detailsNote?: string
}

export interface QueueItem {
  id: number
  movieId: number
  seriesId?: number
  albumId?: number // an album grab (music module)
  bookId?: number // a book grab (ebooks and audiobooks)
  bookFormat?: 'ebook' | 'audiobook'
  tmdbId?: number
  title: string
  subtitle?: string
  posterUrl?: string
  protocol: 'usenet' | 'torrent'
  addedAt?: string
  completedAt?: string
  releaseTitle: string
  sizeBytes: number
  status: 'queued' | 'downloading' | 'importing' | 'completed' | 'failed' | 'conflict' | 'paused' | 'stopped'
  progressPct: number
  error?: string
  destPath?: string
  interrupted?: boolean // paused, but not by a person: Mediarium was restarted while it downloaded
  pending?: 'pausing' | 'stopping' // still winding down
  keptFiles?: boolean // the partly downloaded files are still in the downloads folder
  queuePosition?: number // waiting in line: 1 is next
}

export interface BlocklistEntry {
  id: number
  releaseTitle: string
  protocol: string
  reason: string
  createdAt?: string
}

export interface ActivityEntry {
  id: number
  eventType: string
  message: string
  createdAt: string
  // The page of the title the line is about, when there is one.
  link?: string
}

export interface DiscoverMovie {
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterUrl?: string
  genres?: string[]
  rating?: number
  voteCount?: number
  mediaType?: 'movie' | 'tv'
  releaseDate?: string // YYYY-MM-DD, release or first air date
}

// Moving from Radarr, Sonarr, Prowlarr and SABnzbd (read-only on their side).
export type ArrApp = 'radarr' | 'sonarr' | 'medusa' | 'sickchill' | 'prowlarr' | 'jackett' | 'nzbhydra' | 'sabnzbd' | 'nzbget' | 'overseerr' | 'ombi' | 'bazarr'
export interface ArrConnection {
  url: string
  apiKey?: string // every app but NZBGet
  username?: string // NZBGet
  password?: string // NZBGet
  torznab?: boolean // NZBHydra2: also add its torrent feed
}
export interface MigrateSummary {
  total: number
  add: number
  exists: number
  skip: number
}
export type MigrateAction = 'add' | 'exists' | 'skip'
export interface MigrateTitle {
  title: string
  year?: number
  tmdbId?: number
  tvdbId?: number
  monitored: boolean
  arrPath: string
  path: string
  folderFound: boolean
  inLibraryFolder: boolean
  files: number
  quality?: string
  unmonitoredSeasons?: number[]
  arrProfile?: string
  profileId: number
  profileName?: string
  action: MigrateAction
  reason?: string
}
export interface MigrateRootFolder {
  path: string
  mappedTo: string
  exists: boolean
  suggested: boolean
  titles: number
  foldersFound: number
}
export interface MigrateProfile {
  app: 'radarr' | 'sonarr'
  arrId: number
  arrName: string
  profileId: number
  profileName: string
  how: string
  titles: number
}
export interface MigrateTitlesPreview {
  ok: boolean
  error?: string
  version?: string
  summary: MigrateSummary
  rootFolders: MigrateRootFolder[]
  profiles: MigrateProfile[]
  items: MigrateTitle[]
}
export interface MigrateIndexer {
  name: string
  implementation: string
  protocol: 'usenet' | 'torrent'
  baseUrl?: string
  definitionId?: string
  enabled: boolean
  categories?: number[]
  sourceId?: string // Jackett's id for the site
  canAddDirectly?: boolean
  direct?: boolean
  action: MigrateAction
  reason?: string
}
export interface MigrateRequestItem {
  title: string
  year?: number
  mediaType: 'movie' | 'tv'
  tmdbId?: number
  tvdbId?: number
  status: string
  requestedBy: string[]
  seasons?: number[]
  action: MigrateAction
  reason?: string
}
export interface MigrateSimple<T> {
  ok: boolean
  error?: string
  version?: string
  summary: MigrateSummary
  items: T[]
}
export interface MigrateBazarr {
  ok: boolean
  error?: string
  version?: string
  summary?: MigrateSummary
  included: boolean
  languages: { name: string; code: string; mapsTo: string; action: MigrateAction; reason?: string }[]
  current?: string[]
  new?: string[]
  providers?: { name: string; equivalent: string; reason?: string }[]
  providersError?: string
}
export interface MigrateServer {
  name: string
  host: string
  port: number
  ssl: boolean
  username?: string
  hasPassword: boolean
  connections: number
  priority: number
  optional: boolean
  enabled: boolean
  action: MigrateAction
  reason?: string
}
export interface MigratePreview {
  radarr?: MigrateTitlesPreview
  sonarr?: MigrateTitlesPreview
  prowlarr?: MigrateSimple<MigrateIndexer>
  jackett?: MigrateSimple<MigrateIndexer>
  nzbhydra?: MigrateSimple<MigrateIndexer>
  sabnzbd?: MigrateSimple<MigrateServer> & { completeDir?: string; categories?: string[] }
  nzbget?: MigrateSimple<MigrateServer> & { completeDir?: string; categories?: string[] }
  medusa?: MigrateTitlesPreview
  sickchill?: MigrateTitlesPreview
  overseerr?: MigrateSimple<MigrateRequestItem>
  ombi?: MigrateSimple<MigrateRequestItem>
  bazarr?: MigrateBazarr
  pathMap: PathMapping[]
  suggestedPathMap: PathMapping[]
  moviesPath: string
  tvPath: string
}
export type MigrateRequest = { [A in ArrApp]?: ArrConnection } & {
  pathMap?: PathMapping[]
  profileMapping?: Record<string, number>
  include?: { movies?: boolean; series?: boolean; indexers?: boolean; usenetServers?: boolean; qualityProfiles?: boolean; requests?: boolean; subtitleLanguages?: boolean }
}
export interface MigrateCounts {
  added: number
  existing: number
  skipped: number
  failed: number
  filesLinked: number
  disabled: number
}
export interface MigrateStatus {
  running: boolean
  step: string
  done: number
  total: number
  startedAt?: string
  finishedAt?: string
  results?: { movies?: MigrateCounts; series?: MigrateCounts; indexers?: MigrateCounts; usenetServers?: MigrateCounts; requests?: MigrateCounts; subtitleLanguages?: MigrateCounts; qualityProfiles?: MigrateProfile[] }
  items?: { kind: string; name: string; outcome: 'added' | 'exists' | 'skipped' | 'failed'; reason?: string }[]
  errors?: string[]
  notes?: string[]
}

// How busy the server is, and the free space for each folder.
export interface SystemStats {
  cpu?: { percent: number; cores: number }
  memory?: { usedBytes: number; totalBytes: number }
  load?: number[]
  uptimeSeconds?: number
  app: { memoryBytes: number; limitBytes?: number; uptimeSeconds: number; goroutines: number; cpuPercent?: number }
  // All the disks Mediarium uses, each counted once.
  storage?: { usedBytes: number; freeBytes: number; totalBytes: number }
  os: string
  arch: string
  disks: { label: string; path: string; freeBytes: number; totalBytes: number; usedBytes: number }[]
}

// What the clean-up would remove from the downloads area.
export interface CleanupReport {
  reclaimableBytes: number
  items: { path: string; sizeBytes: number; reason: string }[]
  lastRunAt?: string
  auto: boolean
  // How many days finished downloads and activity are kept; 0 keeps them forever.
  historyRetentionDays?: number
  trashDays?: number
}

// One entry in a title's activity log.
export interface TitleEvent {
  at: string
  kind: string
  message: string
  level: 'info' | 'warn' | 'error'
}

// A file in a title's folder on disk (paths are relative to that folder).
export interface TitleFile {
  path: string
  size: number
  modified: string
  kind: 'video' | 'audio' | 'subtitle' | 'image' | 'nfo' | 'other'
  main: boolean // one of the files the library tracks
  episodeId?: number
  trackId?: number // an album's track file
}

// A Plex, Jellyfin or Emby server Mediarium tells about new files.
export type MediaServerKind = 'plex' | 'jellyfin' | 'emby' | 'audiobookshelf' | 'kavita'

export interface PathMapping {
  from: string // path as Mediarium sees it, e.g. /movies
  to: string // the same folder as the media server sees it, e.g. /data/movies
}

export interface MediaServer {
  id: number
  name: string
  kind: MediaServerKind
  baseUrl: string
  publicUrl: string
  webUrl: string
  hasToken: boolean
  enabled: boolean
  refreshAfterImport: boolean
  pathMap: PathMapping[]
  machineIdentifier?: string
  lastError?: string
  lastCheckedAt?: string
}

export interface MediaServerInput {
  name?: string
  kind: MediaServerKind
  baseUrl: string
  token?: string
  publicUrl?: string
  enabled?: boolean
  refreshAfterImport?: boolean
  pathMap?: PathMapping[]
}

export interface MediaServerTest {
  ok: boolean
  error?: string
  serverName?: string
  version?: string
  machineIdentifier?: string
  libraries?: { id: string; title: string; type: string; locations: string[] }[]
}

export interface FoundMediaServer {
  kind: MediaServerKind
  name: string
  address: string
  version?: string
  id?: string
  alreadyAdded: boolean
  via: 'broadcast' | 'scan'
}

export interface PlexAccountServer {
  name: string
  machineIdentifier: string
  version?: string
  owned: boolean
  alreadyAdded: boolean
  connections: { uri: string; local: boolean }[]
}

export type PlexPinState = { done: false; expired?: boolean; error?: string } | { done: true; servers: PlexAccountServer[] }
export type QuickConnectState = { done: false; code?: string; expired?: boolean; error?: string } | { done: true; server: MediaServer }

export interface WatchLink {
  serverId: number
  name: string
  kind: MediaServerKind
  url: string
  appUrl?: string
}

export interface Trailer {
  name: string
  site: string
  key: string
  url: string
  type?: string
  official?: boolean
}

export interface CastMember {
  name: string
  character?: string
  profileUrl?: string
}

// One season of a show, as the page of a show you do not have yet lists it.
export interface SeasonInfo {
  number: number
  name: string
  episodes: number
  airDate?: string
}

export interface TitleDetails {
  directors?: string[] // movies
  creators?: string[] // shows
  seasonList?: SeasonInfo[]
  genres?: string[]
  rating?: number
  voteCount?: number
  runtime?: number
  tagline?: string
  certification?: string
  releaseDate?: string
  releaseStatus?: string
  language?: string
  homepage?: string
  imdbId?: string
  cast?: CastMember[]
  trailers?: Trailer[]
}

export interface TVDetail extends TitleDetails {
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterUrl?: string
  backdropUrl?: string
  firstAirDate?: string
  seasons?: number
  episodes?: number
  networks?: string[]
  contentRating?: string
  libraryId?: number
  status?: MediaStatus
}

export interface MovieDetail extends TitleDetails {
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterUrl?: string
  backdropUrl?: string
  libraryId?: number
  status?: 'missing' | 'downloading' | 'downloaded'
  quality?: string
  filePath?: string
}

export interface SubtitleQuota {
  hasKey: boolean
  hasAccount: boolean
  limit: number
  used: number
  remaining: number
  windowHours: number
  resetsAt: string | null
  source: 'reported' | 'estimated'
  missingItems: number
  missingFiles: number
  daysToFinish: number
  message: string
  exceeded: boolean
}

export interface SubtitleWanted {
  dismissed?: boolean
  kind: 'movie' | 'episode'
  id: number
  tmdbId?: number
  seriesId?: number
  title: string
  subtitle?: string
  missing: string[]
}

export interface SubtitleStatus {
  languages: string[]
  present: string[]
}

export interface SubtitleResult {
  score: number
  fileId: number
  language: string
  release: string
  rating: number
}

export interface VPNConfig {
  id: number
  label: string
  provider: string
  peerPublicKey: string
  endpoint: string
  allowedIps?: string[]
  localAddresses: string[]
  dns?: string[]
  active: boolean
}

export interface VPNStatus {
  // connected is true only while the tunnel is up and the VPN server answers.
  connected: boolean
  state?: 'off' | 'connecting' | 'connected' | 'down'
  // label is the connection that is switched on, even while it isn't working.
  label?: string
  reason?: string
  connectedAt?: string
}

export interface NotificationTarget {
  id: number
  name: string
  type: string
  url?: string
  chatId?: string
  enabled: boolean
  events?: string[]
  config?: Record<string, string>
  hasSecrets?: Record<string, boolean>
}

export interface NotifyField {
  name: string
  label: string
  kind: 'text' | 'password' | 'number' | 'select'
  required?: boolean
  placeholder?: string
  default?: string
  options?: { value: string; label: string }[]
  help?: string
}

export interface NotifyType {
  type: string
  label: string
  fields: NotifyField[]
}

export interface MonitorStatus {
  kind: 'usenet' | 'indexer'
  id: number
  name: string
  ok: boolean
  checkedAt: string
  error?: string
  hint?: string
}

export interface ImportCandidate {
  tmdbId: number
  title: string
  year: number
  posterUrl?: string
}

export interface ImportItem {
  key: string
  title: string
  year: number
  fileCount: number
  sizeBytes: number
  samplePath: string
  seasons?: number[]
  quality?: string
  match: 'matched' | 'ambiguous' | 'unmatched'
  candidates: ImportCandidate[]
  inLibrary: boolean
  error?: string
}

export interface ImportJob {
  id: string
  kind: 'movie' | 'tv'
  root: string
  phase: 'scanning' | 'matching' | 'ready' | 'importing' | 'done' | 'failed'
  done: number
  total: number
  items: ImportItem[]
  skipped: string[]
  batchId?: number // set once the review has been confirmed
  error?: string
}

// A scan that is still running or waiting to be reviewed.
export interface ImportActiveJob {
  id: string
  kind: 'movie' | 'tv'
  root: string
  phase: ImportJob['phase']
  done: number
  total: number
}

// One confirmed import and how far its background work has got.
export interface ImportBatch {
  id: number
  kind: 'movie' | 'tv'
  root: string
  createdAt: string
  finishedAt?: string
  elapsedMs: number // how long the import has run, by the server's clock
  running: boolean
  dismissed: boolean
  monitor: boolean // the titles are watched for new episodes and better versions
  noUpgrade: boolean
  monitorMissing: boolean
  total: number // titles being added
  done: number // of those, details finished
  added: number
  already: number
  problems: number
}

export interface ImportBatchItem {
  title: string
  kind: 'movie' | 'series'
  outcome: 'added' | 'already' | 'failed'
  state: 'pending' | 'done' | 'problem'
  note?: string
  imported: number // shows: episodes found on disk and marked as downloaded
  skipped: number // shows: episodes that already were
}

export interface ImportBatchDetail extends ImportBatch {
  items: ImportBatchItem[]
}

export interface ImportActive {
  jobs: ImportActiveJob[]
  batches: ImportBatch[]
}

export interface ImportOptions {
  monitor: boolean
  noUpgrade: boolean
  monitorMissing: boolean
}

export interface FolderCheck {
  path: string
  exists: boolean
  writable: boolean
  freeBytes: number
  totalBytes: number
  mounted: boolean
  mountKnown: boolean
  // True when Mediarium runs in a container (only then do compose lines apply).
  inDocker?: boolean
  warnings: string[]
  // A missing folder that Mediarium can create safely (inside a mapped, writable one).
  canCreate?: boolean
}

export interface HealthItem {
  id: string
  level: 'error' | 'warn' | 'info'
  title: string
  impact: string
  action?: { label: string; path: string }
}

export interface DiskUsage {
  files: number
  bytes: number
  trashDays?: number
}

export interface DashFolder extends FolderCheck {
  key: 'movies' | 'tv' | 'downloads'
  label: string
  libraryBytes: number
  items: number
  itemsLabel: string
  hardlinks?: boolean
}

export interface DashActive {
  id: number
  title: string
  subtitle?: string
  posterUrl?: string
  status: string
  protocol: 'usenet' | 'torrent'
  progressPct: number
  sizeBytes: number
  release: string
}

export interface DashRecent {
  kind: 'movie' | 'series' | 'album' | 'book'
  id: number
  tmdbId?: number
  seriesId?: number
  albumId?: number
  bookId?: number
  title: string
  subtitle?: string
  year?: number
  posterUrl?: string
  quality?: string
  sizeBytes?: number
  at: string
  // Recently added only: downloaded, downloading, partial, missing or unmonitored.
  state?: 'downloaded' | 'downloading' | 'partial' | 'missing' | 'unmonitored'
}

export interface DashboardData {
  library: {
    movies: { total: number; downloaded: number; missing: number; downloading: number }
    series: { total: number; episodes: number; episodesDownloaded: number; episodesMissing: number }
    // What the Wanted page lists under Missing (monitored, no file, already aired).
    wanted: { movies: number; episodes: number }
    // Always present, zeros when there is no music (or the module is off).
    music?: { artists: number; albums: number; downloaded: number; missing: number }
    // Books wanted in each format (zeros when there are none).
    ebooks?: { books: number; downloaded: number; missing: number }
    audiobooks?: { books: number; downloaded: number; missing: number }
    sizeBytes: number
    qualities: { tier: string; count: number }[]
  }
  folders: DashFolder[]
  active: DashActive[]
  recentlyAdded: DashRecent[]
  recentDownloads: DashRecent[]
  upcoming: CalendarEntry[]
  health: HealthItem[]
  setup: { indexers: number; usenetServers: number; vpnConnected: boolean; torrentsReady: boolean; mediaServers: number }
}

export interface CalendarEntry {
  kind: 'movie' | 'episode' | 'album'
  id: number
  movieId?: number
  tmdbId?: number
  seriesId?: number
  albumId?: number
  artistId?: number
  title: string
  subtitle?: string
  releaseDate: string
  status: 'missing' | 'downloading' | 'downloaded'
}

// ---- Books (ebooks and audiobooks) -------------------------------------

export type BookFormat = 'ebook' | 'audiobook'
export interface BookFormatState {
  wanted: boolean
  status: 'missing' | 'downloading' | 'downloaded'
  format?: string // the file format: epub, m4b, mp3...
  path?: string // administrators only
}
export interface Book {
  id: number
  olKey: string
  title: string
  author: string
  authorKey?: string
  year?: number
  coverUrl?: string
  description?: string
  addedAt: string
  ebook: BookFormatState
  audiobook: BookFormatState
}
export interface BookImportResult {
  path: string
  author: string
  title: string
  year?: number
  format: string
  files: number
  kind: BookFormat
  status: 'imported' | 'already' | 'unmatched' | 'failed'
  bookId?: number
  matched?: string
  message?: string
}
export interface BookImportState {
  phase: '' | 'running' | 'done' | 'failed'
  done: number
  total: number
  summary: Record<string, number>
  results: BookImportResult[]
  error?: string
  started?: string
}
export interface BookProgress {
  bookId: number
  format: BookFormat
  position: string // ebook: an EPUB CFI or a page; audiobook: "track:seconds"
  percent: number
  finished: boolean
  updatedAt?: string
}
export interface BookTrack {
  index: number
  name: string
  size: number
}
export interface BookWork extends BookFound {
  description?: string
  subjects: string[]
}
export interface BookFound {
  key: string
  title: string
  author: string
  authorKey?: string
  year?: number
  coverId?: number
  coverUrl?: string
  libraryId?: number
  // Whether an ebook / an audiobook edition has been published (Open Library).
  hasEbook?: boolean
  hasAudio?: boolean
}

// ---- Modules and music -------------------------------------------------

export type ModuleKey = 'movies' | 'tv' | 'music' | 'audiobooks' | 'ebooks'
export interface ModuleState {
  enabled: boolean
  available: boolean // false for a module that is not built yet
}
export type Modules = Record<ModuleKey, ModuleState>
// What GET /modules answers: the media types plus whether subtitles are switched on.
export type ModulesAnswer = Modules & { subtitlesEnabled?: boolean }

export interface MusicArtistResult {
  mbid: string
  name: string
  sortName: string
  disambiguation?: string
  type?: string
  country?: string
  score: number
  artistId: number // library id when already added, else 0
}

export interface MusicProfile {
  id: number
  name: string
  allowed: string[]
  cutoff: string
  upgradeAllowed: boolean
  fallback: number[]
  default: boolean
  inUse: number // artists that use it
}

export interface MusicProfileInput {
  name: string
  allowed: string[]
  cutoff: string
  upgradeAllowed: boolean
  fallback?: number[]
}

export type MusicMonitor = 'all' | 'future' | 'none'

export interface MusicAlbum {
  id: number
  artistId: number
  artistName?: string
  mbid: string
  title: string
  type: 'album' | 'ep' | 'single' | string
  releaseDate: string
  year: number
  monitored: boolean
  status: MediaStatus
  quality?: string
  path?: string
  coverUrl?: string
  coverArchiveUrl?: string
  tracks?: MusicTrack[]
}

// ---- Discover, music --------------------------------------------------
export type MusicList = 'popular' | 'new' | 'upcoming'
export type MusicType = 'all' | 'album' | 'ep' | 'single'
export type MusicRange = 'week' | 'month' | 'year' | 'all_time'

export interface MusicDiscoverItem {
  mbid: string
  title: string
  type: 'album' | 'ep' | 'single' | string
  artistName: string
  artistMbid: string
  releaseDate?: string
  genres?: string[]
  coverUrl?: string
  inLibrary: boolean
  artistId?: number
  albumId?: number
}
export interface MusicDiscoverPage {
  page: number
  totalPages: number
  items: MusicDiscoverItem[]
  // Plain words when a list cannot be shown right now.
  note?: string
}
export interface MusicDiscoverArtist {
  mbid: string
  name: string
  listenCount: number
  inLibrary: boolean
  artistId?: number
  coverUrl?: string // only for artists already in the library
}
export interface MusicDiscoverArtists {
  page?: number
  totalPages?: number
  items: MusicDiscoverArtist[]
  note?: string
}

export interface MusicTrack {
  id: number
  disc: number
  position: number
  title: string
  lengthMs: number
  hasFile: boolean
  filePath?: string
}

export interface MusicArtist {
  id: number
  mbid: string
  name: string
  sortName: string
  disambiguation?: string
  imageUrl?: string
  coverUrl?: string
  genres?: string[]
  monitored: boolean
  monitorNew: boolean
  profileId: number
  profileName: string
  addedAt: string
  albumCount: number
  monitoredCount: number
  downloadedCount: number
  albums?: MusicAlbum[]
}

// A release list with what the indexers said: how many were asked and
// which did not answer.
export interface SearchAnswer<T> {
  results: T[]
  sources: number
  unanswered: { name: string; message: string }[]
}

export interface MusicRelease extends SearchResult {
  artist?: string
  album?: string
  year?: number
  format?: string
  bitrate?: string
  bitDepth?: number
  quality?: string
  discography?: boolean
}

export interface MusicWanted extends MusicAlbum {
  profileName: string
  cutoff?: string
  lastSearch?: string
  lastSearchAt?: string
}

export interface MusicImportResult {
  artist: string
  album: string
  folder: string
  files: number
  status: 'imported' | 'already' | 'unmatched' | 'failed'
  message?: string
  artistId?: number
  albumId?: number
  quality?: string
  tracks?: number
}

export interface MusicImportJob {
  id: string
  root: string
  phase: 'scanning' | 'matching' | 'done' | 'failed'
  done: number
  total: number
  summary: { artists: number; albums: number; imported: number; already: number; unmatched: number; failed: number }
  results: MusicImportResult[]
  error?: string
}

// Changes to many titles at once (the Library page's select mode). Every
// answer says how many were done and, for the rest, why not.
export interface BulkTitle {
  kind: 'movie' | 'tv'
  id: number
}
export interface BulkFailure {
  kind: string
  id: number
  title?: string
  reason: string
}
export interface BulkResult {
  updated: number
  failed: BulkFailure[]
  message: string
}
export interface BulkSearchResult {
  searching: number // titles the search was started for
  leftOut: number // wanted titles over the limit, left for the automatic search
  limit: number
  skipped: number // chosen titles that did not need a search
  failed: BulkFailure[]
  message: string
}

// ---- Updates and restarts (Settings > System, and the notice on the dashboard) ----

export interface UpdateLatest {
  version: string
  name?: string
  notes?: string
  moreNotes?: boolean
  url: string
  prerelease?: boolean
  publishedAt?: string
}

export interface UpdateJob {
  state: 'downloading' | 'checking' | 'installing' | 'restarting' | 'failed'
  version: string
  message: string
  error?: string
  active: boolean
  auto?: boolean
}

// GET /api/system/update/latest and POST /api/system/update/check.
export interface UpdateNotice {
  enabled: boolean // the daily check is on
  running: string
  available: boolean
  latest?: UpdateLatest
  checkedAt?: string
  error?: string
  install: { kind: 'docker' | 'native'; full: boolean }
  canInstall: boolean
  installNote?: string
  job?: UpdateJob
}

export interface SystemOptions {
  updateCheck: boolean
  autoInstall: boolean
  allowPush: boolean
  autoRestartWhenStuck: boolean
}

export interface SystemControl {
  kind: 'docker' | 'systemd' | 'launchd' | 'custom' | 'none'
  canRestart: boolean
  canShutdown: boolean
  note: string
}

// GET /api/system/update.
export interface UpdateState {
  running: string
  image: string
  pushed: { version: string; sha256?: string; installedAt: string; hasPrevious: boolean; previousVersion?: string; running: boolean } | null
  failed?: { version: string; at: string }
  allowPush: boolean
  canInstall: boolean
  platform: string
  options: SystemOptions
  control: SystemControl
  selfRestart?: { at: string; reason: string }
}

export const api = {
  version: () => get<{ version: string }>('/version'),
  onboardingStatus: () => get<OnboardingStatus>('/onboarding/status'),
  createAdmin: (data: { username: string; password: string; name?: string; email?: string; setupCode?: string }) => post<User>('/onboarding/admin', data),
  updateAccount: (data: { username: string; name: string; email: string }) => put<User>('/auth/profile', data),

  login: (username: string, password: string) => post<User>('/auth/login', { username, password }),
  logout: () => post<null>('/auth/logout'),
  me: () => get<User>('/auth/me'),
  changePassword: (currentPassword: string, newPassword: string) =>
    post<null>('/auth/change-password', { currentPassword, newPassword }),
  listAPIKeys: () => get<APIKey[]>('/auth/api-keys'),
  createAPIKey: (name: string) => post<APIKey>('/auth/api-keys', { name }),
  revokeAPIKey: (id: number) => del<null>(`/auth/api-keys/${id}`),
  deleteAPIKey: (id: number) => del<null>(`/auth/api-keys/${id}?remove=true`),

  // Accounts (administrators only).
  listAccounts: () => get<Account[]>('/users'),
  createAccount: (data: NewAccount) => post<Account>('/users', data),
  updateAccountById: (id: number, data: AccountChanges) => put<Account>(`/users/${id}`, data),
  deleteAccount: (id: number) => del<null>(`/users/${id}`),

  getSettings: () => get<Settings>('/settings'),
  putSettings: (partial: Partial<Settings> & { legalAcknowledged?: boolean; tmdbApiKey?: string; openSubtitlesApiKey?: string; openSubtitlesUsername?: string; openSubtitlesPassword?: string }) => put<Settings>('/settings', partial),

  listIndexers: () => get<IndexerConfig[]>('/indexers'),
  indexerDefinitions: (refresh = false) =>
    get<{ updatedAt?: string; source?: string; licence?: string; definitions: IndexerDefinition[] }>(`/indexer-definitions${refresh ? '?refresh=1' : ''}`),
  createIndexer: (data: { name: string; definitionId: string; baseUrl: string; apiKey: string; protocol?: 'usenet' | 'torrent'; settings?: Record<string, string> }) =>
    post<IndexerConfig>('/indexers', data),
  deleteIndexer: (id: number) => del<null>(`/indexers/${id}`),
  testIndexerConfig: (data: { name: string; baseUrl: string; apiKey: string }) =>
    post<{ ok: boolean; message: string }>('/indexers/test', data),
  testIndexer: (id: number) => post<{ ok: boolean; message: string }>(`/indexers/${id}/test`),
  updateIndexer: (id: number, data: { name?: string; baseUrl?: string; apiKey?: string; enabled?: boolean }) => put<IndexerConfig>(`/indexers/${id}`, data),
  setIndexerEnabled: (id: number, enabled: boolean) => put<null>(`/indexers/${id}/enabled`, { enabled }),

  listUsenetServers: () => get<UsenetServer[]>('/usenet-servers'),
  createUsenetServer: (data: UsenetServerInput) => post<UsenetServer>('/usenet-servers', data),
  updateUsenetServer: (id: number, data: UsenetServerInput) => put<UsenetServer>(`/usenet-servers/${id}`, data),
  deleteUsenetServer: (id: number) => del<null>(`/usenet-servers/${id}`),
  testUsenetServerConfig: (data: { id?: number; host: string; port: number; useSsl: boolean; username?: string; password?: string }) =>
    post<{ ok: boolean; message: string }>('/usenet-servers/test', data),
  testUsenetServer: (id: number) => post<{ ok: boolean; message: string }>(`/usenet-servers/${id}/test`),
  downloadsStatus: () => get<DownloadsStatus>('/downloads/status'),

  search: (q: string) => get<SearchResult[]>(`/search?q=${encodeURIComponent(q)}`),

  // The two big lists are asked with 'no-cache': the browser sends the tag it has and the server answers 304 when nothing changed.
  listMovies: () => request<Movie[]>('/movies', { cache: 'no-cache' }),
  getMovie: (id: number) => get<Movie>(`/movies/${id}`),
  addMovie: (tmdbId: number, options: AddMovieOptions = {}) => post<Movie>('/movies', { tmdbId, ...options }),
  discoverSearch: (q: string, signal?: AbortSignal) => request<TitleResult[]>(`/discover/search?q=${encodeURIComponent(q)}`, { signal }),
  setMovieSources: (id: number, sources: SourcePref) => put<null>(`/movies/${id}/sources`, { sources }),
  setSeriesSources: (id: number, sources: SourcePref) => put<null>(`/series/${id}/sources`, { sources }),
  // What "delete everything on disk" would remove for a title, before it is asked.
  movieDiskUsage: (id: number) => get<DiskUsage>(`/movies/${id}/disk-usage`),
  seriesDiskUsage: (id: number) => get<DiskUsage>(`/series/${id}/disk-usage`),
  artistDiskUsage: (id: number) => get<DiskUsage>(`/music/artists/${id}/disk-usage`),
  deleteMovie: (id: number, deleteFiles: boolean) => del<null>(`/movies/${id}?deleteFiles=${deleteFiles}`),
  grab: (movieId: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ queueId: number }>(`/movies/${movieId}/grab`, data),
  searchGrab: (data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ movieId: number; queueId: number }>('/search/grab', data),
  similarMovies: (movieId: number) => get<DiscoverMovie[]>(`/movies/${movieId}/similar`),
  tmdbMovieDetail: (tmdbId: number) => get<MovieDetail>(`/tmdb/movies/${tmdbId}`),
  tmdbTVDetail: (tmdbId: number) => get<TVDetail>(`/tmdb/tv/${tmdbId}`),
  tmdbSimilarMovies: (tmdbId: number) => get<DiscoverMovie[]>(`/tmdb/movies/${tmdbId}/similar`),

  movieSubtitleStatus: (id: number) => get<SubtitleStatus>(`/movies/${id}/subtitles/status`),
  episodeSubtitleStatus: (id: number) => get<SubtitleStatus>(`/episodes/${id}/subtitles/status`),
  searchEpisodeSubtitles: (id: number, lang: string) => get<SubtitleResult[]>(`/episodes/${id}/subtitles?lang=${encodeURIComponent(lang)}`),
  downloadEpisodeSubtitle: (id: number, fileId: number, language: string) =>
    post<{ path: string }>(`/episodes/${id}/subtitles/download`, { fileId, language }),
  subtitlesWanted: (includeDismissed = false) => get<SubtitleWanted[]>(`/subtitles/wanted${includeDismissed ? '?includeDismissed=1' : ''}`),
  subtitleQuota: () => get<SubtitleQuota>('/subtitles/quota'),
  subtitlesGet: (data: { items?: { kind: string; id: number }[]; all?: boolean }) =>
    post<{ downloaded: number; stopped: boolean; skipped: number; message: string; quota: SubtitleQuota }>('/subtitles/get', data),
  dismissSubtitles: (items: { kind: string; id: number }[]) => post<null>('/subtitles/dismiss', { items }),
  undismissSubtitles: (items: { kind: string; id: number }[]) => request<null>('/subtitles/dismiss', { method: 'DELETE', body: JSON.stringify({ items }) }),
  subtitleSweep: () => post<{ downloaded: number; message: string }>('/subtitles/sweep'),
  searchSubtitles: (movieId: number, lang = 'en') => get<SubtitleResult[]>(`/movies/${movieId}/subtitles?lang=${lang}`),
  downloadSubtitle: (movieId: number, fileId: number, language = 'en') =>
    post<{ path: string }>(`/movies/${movieId}/subtitles/download`, { fileId, language }),

  listProfiles: () => get<{ profiles: QualityProfile[]; tiers: string[]; defaultId: number }>('/quality-profiles'),
  createProfile: (data: QualityProfileInput) =>
    post<QualityProfile>('/quality-profiles', data),
  updateProfile: (id: number, data: QualityProfileInput) =>
    put<QualityProfile>(`/quality-profiles/${id}`, data),
  deleteProfile: (id: number) => del<null>(`/quality-profiles/${id}`),
  setMovieProfile: (id: number, profileId: number) => put<null>(`/movies/${id}/profile`, { profileId }),
  setSeriesProfile: (id: number, profileId: number) => put<null>(`/series/${id}/profile`, { profileId }),

  listQueue: () => get<QueueItem[]>('/queue'),
  retryQueueItem: (id: number) => post<{ queueId: number }>(`/queue/${id}/retry`),
  blocklistQueueItem: (id: number) => post<null>(`/queue/${id}/blocklist`),
  deleteQueueItem: (id: number, deleteFiles = false) => del<null>(`/queue/${id}${deleteFiles ? '?deleteFiles=1' : ''}`),
  pauseQueueItem: (id: number) => post<{ status: string; pending?: string }>(`/queue/${id}/pause`),
  resumeQueueItem: (id: number) => post<{ status: string }>(`/queue/${id}/resume`),
  stopQueueItem: (id: number, deleteFiles: boolean) => post<{ status: string; pending?: string }>(`/queue/${id}/stop`, { deleteFiles }),
  pauseAllQueue: () => post<{ paused: number }>('/queue/pause-all'),
  resumeAllQueue: () => post<{ resumed: number; skipped: number; problem?: string }>('/queue/resume-all'),
  clearFinishedQueue: () => del<{ removed: number }>('/queue'),
  resolveConflict: (id: number, overwrite: boolean) => post<null>(`/queue/${id}/resolve-conflict`, { overwrite }),
  getWanted: (kind: 'missing' | 'cutoff') => get<WantedItem[]>(`/wanted?kind=${kind}`),
  searchNowMovie: (id: number) => post<{ grabbed: number; message: string }>(`/movies/${id}/search-now`),
  searchNowSeries: (id: number, scope: { season?: number; episode?: number } = {}) =>
    post<{ grabbed: number; message: string }>(`/series/${id}/search-now`, scope),
  movieSearch: (id: number) => get<SearchAnswer<SearchResult>>(`/movies/${id}/search?detail=1`),
  setMovieMonitored: (id: number, monitored: boolean) => put<null>(`/movies/${id}/monitored`, { monitored }),
  setSeriesMonitored: (id: number, monitored: boolean) => put<null>(`/series/${id}/monitored`, { monitored }),
  setSeasonMonitored: (id: number, season: number, monitored: boolean) =>
    put<null>(`/series/${id}/seasons/${season}/monitored`, { monitored }),
  setEpisodeMonitored: (id: number, monitored: boolean) => put<null>(`/episodes/${id}/monitored`, { monitored }),
  listBlocklist: () => get<BlocklistEntry[]>('/blocklist'),
  removeBlocklistEntry: (id: number) => del<null>(`/blocklist/${id}`),
  clearBlocklist: () => del<null>('/blocklist'),
  savedBackups: () => get<SavedBackups>('/system/backups'),
  saveBackupNow: () => post<{ name: string; size: number }>('/system/backups'),
  setIndexerPriority: (id: number, priority: number) => put<null>(`/indexers/${id}/priority`, { priority }),
  calendarFeed: () => get<{ on: boolean; path?: string }>('/calendar/feed'),
  newCalendarFeed: () => post<{ on: boolean; path?: string }>('/calendar/feed'),
  removeCalendarFeed: () => del<{ on: boolean; path?: string }>('/calendar/feed'),
  manualImportList: () => get<{ folder: string; files: ManualFile[]; more?: boolean }>('/manual-import'),
  manualImport: (body: { path: string; movieId?: number; seriesId?: number; season?: number; episode?: number }) => post<{ path: string }>('/manual-import', body),
  listExclusions: () => get<{ kind: 'movie' | 'tv'; tmdbId: number; title: string; year?: number }[]>('/exclusions'),
  addExclusion: (e: { kind: 'movie' | 'tv'; tmdbId: number; title: string; year?: number }) => post<null>('/exclusions', e),
  removeExclusion: (kind: string, tmdbId: number) => del<null>(`/exclusions/${kind}/${tmdbId}`),
  libraryStats: () => get<LibraryStats>('/stats/library'),
  renamePreview: () => get<RenameItem[]>('/rename'),
  rename: (items: { kind: string; id: number }[]) => post<{ renamed: number; failed: string[] }>('/rename', { items }),
  listTrash: () => get<TrashList>('/trash'),
  restoreTrash: (kind: string, id: string) => post<{ label: string; folder: string }>(`/trash/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/restore`),
  deleteTrash: (kind: string, id: string) => del<null>(`/trash/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`),
  emptyTrash: () => del<{ removed: number; freed: number }>('/trash'),
  listActivity: (opts: { limit?: number; q?: string } = {}) => get<ActivityEntry[]>(`/activity?limit=${opts.limit ?? 100}${opts.q ? `&q=${encodeURIComponent(opts.q)}` : ''}`),

  // Music (only answers while the music module is on).
  musicSearch: (q: string, signal?: AbortSignal) => request<MusicArtistResult[]>(`/music/search?q=${encodeURIComponent(q)}`, { signal }),
  musicDiscover: (q: { list: MusicList; type?: MusicType; range?: MusicRange; page?: number; pageSize?: number; genre?: string; yearFrom?: string; yearTo?: string; sort?: string }) =>
    get<MusicDiscoverPage>(`/music/discover?${new URLSearchParams(Object.entries(q).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => [k, String(v)])).toString()}`),
  musicDiscoverArtists: (q: { range?: MusicRange | ''; page?: number }) =>
    get<MusicDiscoverArtists>(`/music/discover/artists?${new URLSearchParams(Object.entries(q).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => [k, String(v)])).toString()}`),
  musicProfiles: () => get<MusicProfile[]>('/music/profiles'),
  musicTiers: () => get<string[]>('/music/tiers'),
  createMusicProfile: (data: MusicProfileInput) => post<MusicProfile>('/music/profiles', data),
  updateMusicProfile: (id: number, data: MusicProfileInput) => put<MusicProfile>(`/music/profiles/${id}`, data),
  deleteMusicProfile: (id: number) => del<null>(`/music/profiles/${id}`),
  listBooks: () => get<Book[]>('/books'),
  getBook: (id: number) => get<Book>(`/books/${id}`),
  searchBooks: (q: string) => get<BookFound[]>(`/books/search?q=${encodeURIComponent(q)}`),
  bookDiscover: (p: { list: 'trending' | 'browse'; period?: string; subject?: string; from?: string; to?: string; sort?: string; page?: number }) => {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries(p)) if (v !== undefined && v !== '') q.set(k, String(v))
    return get<BookFound[]>(`/books/discover?${q}`)
  },
  bookLinks: (id: number) => get<{ serverId: number; name: string; kind: MediaServerKind; url: string }[]>(`/books/${id}/links`),
  bookTracks: (id: number) => get<{ tracks: BookTrack[]; format: string }>(`/books/${id}/tracks`),
  bookTrackUrl: (id: number, n: number) => `/api/books/${id}/listen/${n}`,
  bookFileUrl: (id: number, download = false) => `/api/books/${id}/read${download ? '?download=1' : ''}`,
  bookProgress: (id: number, format: BookFormat) => get<BookProgress>(`/books/${id}/progress?format=${format}`),
  listBookProgress: () => get<BookProgress[]>('/books/progress'),
  saveBookProgress: (id: number, p: { format: BookFormat; position: string; percent: number; finished?: boolean }, keepalive = false) =>
    fetch(`/api/books/${id}/progress`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p), keepalive, credentials: 'same-origin' }).then(() => undefined),
  authorWorks: (key: string, sort: 'popular' | 'newest' = 'popular') => get<BookFound[]>(`/book-authors/${encodeURIComponent(key)}/works?sort=${sort}`),
  followedAuthors: () => get<{ key: string; name: string; ebook: boolean; audiobook: boolean; followedAt: string }[]>('/book-authors'),
  followAuthor: (key: string, data: { name: string; ebook: boolean; audiobook: boolean }) => put<null>(`/book-authors/${encodeURIComponent(key)}/follow`, data),
  unfollowAuthor: (key: string) => del<null>(`/book-authors/${encodeURIComponent(key)}/follow`),
  bookWork: (key: string) => get<BookWork>(`/book-works/${encodeURIComponent(key)}`),
  bookImportStatus: () => get<BookImportState>('/books/import'),
  startBookImport: (format?: BookFormat) => post<BookImportState>('/books/import', { format: format ?? '' }),
  bookSubjects: () => get<{ label: string; subject: string }[]>('/books/subjects'),
  addBook: (data: { olKey: string; title: string; author: string; authorKey?: string; year?: number; coverId?: number; ebook: boolean; audiobook: boolean; searchNow?: boolean }) =>
    post<Book>('/books', data),
  setBookWanted: (id: number, format: BookFormat, wanted: boolean) => put<Book>(`/books/${id}/want`, { format, wanted }),
  deleteBook: (id: number, deleteFiles: boolean) => del<null>(`/books/${id}?deleteFiles=${deleteFiles}`),
  bookReleases: (id: number, format: BookFormat) => get<SearchResult[]>(`/books/${id}/releases?format=${format}`),
  grabBook: (id: number, data: { format: BookFormat; releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ queueId: number }>(`/books/${id}/grab`, data),
  searchNowBook: (id: number, format: BookFormat) => post<{ grabbed: number; message: string }>(`/books/${id}/search?format=${format}`),
  listArtists: () => get<MusicArtist[]>('/music/artists'),
  getArtist: (id: number) => get<MusicArtist>(`/music/artists/${id}`),
  addArtist: (data: { mbid: string; monitor?: MusicMonitor; profileId?: number; searchNow?: boolean }) => post<MusicArtist>('/music/artists', data),
  updateArtist: (id: number, changes: { monitored?: boolean; profileId?: number }) => put<MusicArtist>(`/music/artists/${id}`, changes),
  deleteArtist: (id: number, deleteFiles: boolean) => del<null>(`/music/artists/${id}?deleteFiles=${deleteFiles}`),
  getAlbum: (id: number) => get<MusicAlbum>(`/music/albums/${id}`),
  setAlbumMonitored: (id: number, monitored: boolean) => put<null>(`/music/albums/${id}/monitored`, { monitored }),
  albumSearch: (id: number) => post<SearchAnswer<MusicRelease>>(`/music/albums/${id}/search?detail=1`),
  grabAlbum: (id: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ queueId: number }>(`/music/albums/${id}/grab`, data),
  searchNowAlbum: (id: number) => post<{ grabbed: number; message: string }>(`/music/albums/${id}/search-now`),
  albumFiles: (id: number) => get<{ folder: string; files: TitleFile[] }>(`/music/albums/${id}/files`),
  albumStreamUrl: (id: number, path: string) => `/api/files/stream?album=${id}&path=${encodeURIComponent(path)}`,
  albumEvents: (id: number) => get<TitleEvent[]>(`/music/albums/${id}/events`),
  musicWanted: (kind: 'missing' | 'cutoff') => get<MusicWanted[]>(`/music/wanted?kind=${kind}`),
  startMusicScan: () => post<{ id: string }>('/music/import/scan'),
  musicScan: (id: string) => get<MusicImportJob>(`/music/import/scan/${id}`),

  searchTV: (q: string) => get<DiscoverMovie[]>(`/tv/search?q=${encodeURIComponent(q)}`),
  listSeries: () => request<Series[]>('/series', { cache: 'no-cache' }),
  getSeries: (id: number) => get<SeriesDetail>(`/series/${id}`),
  addSeries: (tmdbId: number, options: AddSeriesOptions = {}) => post<Series>('/series', { tmdbId, ...options }),
  refreshSeries: (id: number) => post<Series>(`/series/${id}/refresh`),
  deleteSeries: (id: number, deleteFiles = false) => del<null>(`/series/${id}?deleteFiles=${deleteFiles}`),
  seriesSearch: (id: number, season: number, episode?: number) =>
    get<SearchAnswer<SearchResult>>(`/series/${id}/search?detail=1&season=${season}${episode ? `&episode=${episode}` : ''}`),
  grabSeries: (id: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent'; season?: number; episode?: number }) =>
    post<{ queueId: number }>(`/series/${id}/grab`, data),

  discoverTrending: () => get<DiscoverMovie[]>('/discover/trending'),
  discoverPopular: () => get<DiscoverMovie[]>('/discover/popular'),
  discoverTrendingTV: () => get<DiscoverMovie[]>('/discover/tv/trending'),
  discoverPopularTV: () => get<DiscoverMovie[]>('/discover/tv/popular'),
  importList: (listUrl: string) => get<DiscoverMovie[]>(`/discover/import-list?url=${encodeURIComponent(listUrl)}`),
  discoverForYou: () => get<DiscoverMovie[]>('/discover/for-you'),
  listMediaServers: () => get<MediaServer[]>('/media-servers'),
  createMediaServer: (data: MediaServerInput) => post<MediaServer>('/media-servers', data),
  updateMediaServer: (id: number, data: Partial<MediaServerInput>) => put<MediaServer>(`/media-servers/${id}`, data),
  deleteMediaServer: (id: number) => del<null>(`/media-servers/${id}`),
  testMediaServerConfig: (data: MediaServerInput & { id?: number }) => post<MediaServerTest>('/media-servers/test', data),
  testMediaServer: (id: number) => post<MediaServerTest>(`/media-servers/${id}/test`),
  refreshMediaServer: (id: number) => post<{ ok: boolean; error?: string }>(`/media-servers/${id}/refresh`),
  discoverMediaServers: (subnets?: string[]) => post<{ found: FoundMediaServer[]; scanned: string[]; note?: string }>('/media-servers/discover', subnets && subnets.length ? { subnets } : {}),
  plexPin: () => post<{ pinId: number; code: string; authUrl: string; expiresIn: number }>('/media-servers/plex/pin'),
  plexPinStatus: (pinId: number) => get<PlexPinState>(`/media-servers/plex/pin/${pinId}`),
  plexPinAdd: (pinId: number, machineIdentifier: string, uri?: string) => post<MediaServer>(`/media-servers/plex/pin/${pinId}/add`, { machineIdentifier, uri }),
  jellyfinQuickConnect: (baseUrl: string) => post<{ id: string; code: string; expiresIn: number }>('/media-servers/jellyfin/quickconnect', { baseUrl }),
  jellyfinQuickConnectStatus: (id: string) => get<QuickConnectState>(`/media-servers/jellyfin/quickconnect/${id}`),
  mediaServerLogin: (data: { kind: 'jellyfin' | 'emby'; baseUrl: string; username: string; password: string }) => post<MediaServer>('/media-servers/login', data),
  watchLinks: (tmdbId: number, kind: 'movie' | 'tv') => get<WatchLink[]>(`/media-servers/links?tmdbId=${tmdbId}&kind=${kind}`),
  mediaServerHomes: () => get<WatchLink[]>('/media-servers/links'),
  migratePreview: (req: MigrateRequest) => post<MigratePreview>('/migrate/preview', req),
  migrateRun: (req: MigrateRequest) => post<MigrateStatus>('/migrate/run', req),
  migrateStatus: () => get<MigrateStatus>('/migrate/status'),
  systemStats: () => get<SystemStats>('/system/stats'),
  diagnostics: () => get<Diagnostics>('/system/diagnostics'),
  problems: (query = '') => get<ProblemList>(`/system/problems${query}`),
  problemCounts: () => get<{ counts: ProblemCounts }>('/system/problems?summary=1').then((r) => r.counts),
  markProblemsRead: (req: { ids?: number[]; all?: boolean }) => post<{ marked: number; counts: ProblemCounts }>('/system/problems/read', req),
  setNotifyOnProblems: (enabled: boolean) => put<{ notifyOnProblems: boolean }>('/system/problems/notify', { enabled }),
  flareSolverrStatus: () => get<FlareSolverrStatus>('/flaresolverr/status'),
  cleanupReport: () => get<CleanupReport>('/system/cleanup'),
  runCleanup: () => post<{ removedBytes: number; removed?: unknown[]; prunedQueueItems?: number; prunedActivity?: number; errors?: unknown }>('/system/cleanup'),
  updateState: () => get<UpdateState>('/system/update'),
  updateLatest: () => get<UpdateNotice>('/system/update/latest'),
  updateCheck: () => post<UpdateNotice>('/system/update/check'),
  updateInstall: () => post<UpdateNotice>('/system/update/install'),
  updateInstallStatus: () => get<UpdateNotice>('/system/update/install'),
  removeUpdate: (restart = false) => del<{ removed: string; image: string; runningPushed: boolean; restarting: boolean; restartNeeded: boolean }>(`/system/update${restart ? '?restart=true' : ''}`),
  systemOptions: () => get<SystemOptions>('/system/options'),
  putSystemOptions: (o: Partial<SystemOptions>) => put<SystemOptions>('/system/options', o),
  restartApp: (safe = false) => post<{ ok: boolean; restarting: boolean; safe: boolean }>(`/system/restart${safe ? '?safe=true' : ''}`),
  shutdownApp: () => post<{ ok: boolean }>('/system/shutdown'),
  movieEvents: (id: number) => get<TitleEvent[]>(`/movies/${id}/events`),
  seriesEvents: (id: number) => get<TitleEvent[]>(`/series/${id}/events`),
  movieFiles: (id: number) => get<{ folder: string; files: TitleFile[] }>(`/movies/${id}/files`),
  seriesFiles: (id: number) => get<{ folder: string; files: TitleFile[] }>(`/series/${id}/files`),
  streamUrl: (kind: 'movie' | 'series', id: number, path: string) => `/api/files/stream?${kind}=${id}&path=${encodeURIComponent(path)}`,
  discoverList: (kind: 'movie' | 'tv', list: 'trending' | 'popular' | 'upcoming' | 'similar', page = 1, older = false) =>
    get<{ page: number; totalPages: number; totalResults?: number; results: DiscoverMovie[] }>(
      `/discover/list?kind=${kind}&list=${list}&page=${page}${older ? '&older=true' : ''}`,
    ),
  discoverGenres: (kind: 'movie' | 'tv') => get<{ id: number; name: string }[]>(`/discover/genres?kind=${kind}`),
  discoverBrowse: (q: { kind: 'movie' | 'tv'; genre?: string; yearFrom?: string; yearTo?: string; sort?: string; page?: number }) =>
    get<{ page: number; totalPages: number; results: DiscoverMovie[] }>(
      `/discover/browse?${new URLSearchParams(Object.entries(q).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => [k, String(v)])).toString()}`,
    ),

  listVPNConfigs: () => get<VPNConfig[]>('/vpn/configs'),
  createVPNConfig: (data: {
    label: string
    provider?: string
    privateKey: string
    peerPublicKey: string
    presharedKey?: string
    endpoint: string
    allowedIps?: string[]
    localAddresses: string[]
    dns?: string[]
  }) => post<VPNConfig>('/vpn/configs', data),
  deleteVPNConfig: (id: number) => del<null>(`/vpn/configs/${id}`),
  activateVPNConfig: (id: number) => post<null>(`/vpn/configs/${id}/activate`),
  deactivateVPN: () => post<null>('/vpn/deactivate'),
  vpnEgressIp: () => get<{ ip: string }>('/vpn/egress-ip'),
  vpnStatus: () => get<VPNStatus>('/vpn/status'),

  listNotificationTargets: () => get<NotificationTarget[]>('/notifications'),
  createNotificationTarget: (data: Record<string, unknown>) => post<NotificationTarget>('/notifications', data),
  updateNotificationTarget: (id: number, data: Record<string, unknown>) => put<NotificationTarget>(`/notifications/${id}`, data),
  notificationTypes: () => get<NotifyType[]>('/notifications/types'),
  testNotification: (data: Record<string, unknown>) => post<{ sent: boolean; error?: string; steps?: NotifyTestStep[] }>('/notifications/test', data),
  monitorStatus: () => get<MonitorStatus[]>('/monitor/status'),
  runMonitor: () => post<null>('/monitor/run'),
  deleteNotificationTarget: (id: number) => del<null>(`/notifications/${id}`),

  calendar: () => get<CalendarEntry[]>('/calendar'),

  scanLibrary: (path: string, kind: 'movie' | 'tv') => post<{ jobId: string }>('/library/scan', { path, kind }),
  getScan: (id: string) => get<ImportJob>(`/library/scan/${id}`),
  runImport: (jobId: string, selections: { key: string; tmdbId: number; title?: string; year?: number }[], options: ImportOptions) =>
    post<{ jobId: string; batchId: number }>('/library/import', { jobId, selections, ...options }),
  importActive: () => get<ImportActive>('/library/import/active'),
  importBatch: (id: number) => get<ImportBatchDetail>(`/library/import/batches/${id}`),
  dismissImportBatch: (id: number) => post<null>(`/library/import/batches/${id}/dismiss`),
  // Start monitoring exactly the titles one import added (watch), and/or make the missing episodes of its shows wanted (missing).
  watchImport: (id: number, what: { watch?: boolean; missing?: boolean }) =>
    post<{ movies: number; shows: number; message: string }>(`/library/import/batches/${id}/watch`, what),
  setMovieNoUpgrade: (id: number, noUpgrade: boolean) => put<null>(`/movies/${id}/no-upgrade`, { noUpgrade }),
  setSeriesNoUpgrade: (id: number, noUpgrade: boolean) => put<null>(`/series/${id}/no-upgrade`, { noUpgrade }),
  bulkMonitored: (items: BulkTitle[], monitored: boolean) => put<BulkResult>('/library/bulk/monitored', { items, monitored }),
  bulkNoUpgrade: (items: BulkTitle[], noUpgrade: boolean) => put<BulkResult>('/library/bulk/no-upgrade', { items, noUpgrade }),
  listTags: () => get<{ name: string; movies: number; shows: number }[]>('/tags'),
  titleTags: (kind: 'movie' | 'tv', id: number) => get<{ tags?: string[] }>(kind === 'movie' ? `/movies/${id}` : `/series/${id}`).then((t) => t.tags ?? []),
  setTitleTags: (kind: 'movie' | 'tv', id: number, tags: string[]) => put<{ tags: string[] }>(kind === 'movie' ? `/movies/${id}/tags` : `/series/${id}/tags`, { tags }),
  bulkTags: (items: BulkTitle[], add: string[], remove: string[]) => put<BulkResult>('/library/bulk/tags', { items, add, remove }),
  bulkProfile: (items: BulkTitle[], profileId: number) => put<BulkResult>('/library/bulk/profile', { items, profileId }),
  bulkSources: (items: BulkTitle[], sources: SourcePref) => put<BulkResult>('/library/bulk/sources', { items, sources }),
  bulkSearchNow: (items: BulkTitle[]) => post<BulkSearchResult>('/library/bulk/search-now', { items }),
  // deleteFiles is sent every time and is false unless the person ticked it.
  bulkRemove: (items: BulkTitle[], deleteFiles: boolean) => post<BulkResult>('/library/bulk/remove', { items, deleteFiles }),
  bulkArtistFollow: (ids: number[], monitored: boolean) => put<BulkResult>('/music/bulk/follow', { ids, monitored }),
  bulkArtistProfile: (ids: number[], profileId: number) => put<BulkResult>('/music/bulk/profile', { ids, profileId }),
  bulkArtistRemove: (ids: number[], deleteFiles: boolean) => post<BulkResult>('/music/bulk/remove', { ids, deleteFiles }),
  tmdbSearch: (kind: 'movie' | 'tv', q: string) =>
    get<ImportCandidate[]>(`/tmdb/search?kind=${kind}&q=${encodeURIComponent(q)}`),

  testService: (data: { service: 'tmdb' | 'opensubtitles' | 'trakt'; key?: string; username?: string; password?: string }) =>
    post<{ ok: boolean; message: string }>('/settings/test-service', data),
  createFolder: (path: string) => post<FolderCheck>('/settings/folder-create', { path }),
  folderCheck: (path: string) => get<FolderCheck>(`/settings/folder-check?path=${encodeURIComponent(path)}`),
  health: () => get<{ items: HealthItem[] }>('/health'),
  modules: () => get<ModulesAnswer>('/modules'),
  setModules: (changes: Partial<Record<ModuleKey, boolean>>) => put<Modules>('/modules', changes),
  dashboard: () => get<DashboardData>('/dashboard'),

  filesystemCheck: (a: string, b: string) =>
    get<{ sameFilesystem: boolean; supported: boolean; error?: string }>(
      `/settings/filesystem-check?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`,
    ),

  namingPreview: (preset: string, format?: string) =>
    get<{ folder: string; filename: string }>(
      `/settings/naming-preview?preset=${encodeURIComponent(preset)}${format ? `&format=${encodeURIComponent(format)}` : ''}`,
    ),
}

export interface FlareSolverrStatus {
  bundled: boolean
  configured: boolean
  running: boolean
  version?: string
}
