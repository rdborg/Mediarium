// Thin typed wrapper around the REST API in internal/api. Session auth is
// a plain HTTP-only cookie, so every call just needs
// credentials: 'include' — no token to manage client-side.

import { noteBusy, noteFine, startRead } from './busyState'
import { outsideService, slowServiceMessage } from './outsideService'
import { signalFor } from './requestSignal'
import { st, t } from './i18n.ts'
import type { AlbumRescanReport } from './albumRescan'
import type { MusicFilter } from './musicFilter'
import type { Searched } from './searched'
import type { Diagnostics } from './supportReport'
import type { ProfileScoreChange } from './formatChanges'
import type { ProblemCounts, ProblemList } from './problemsView'
import { BASE_PATH, rebasePictures, withBase } from './base'
import { noteEndReason, noteSessionEnded, sessionEnded } from './sessionEnd'
import { wantedPagePath } from './wantedPage'
import { namingPreviewPath } from './namingPreview'
import type { BlockingDownload } from './grabBlocked.ts'

const BASE = withBase('/api')

export class ApiError extends Error {
  status: number
  // True when Mediarium said it is busy, or did not answer in time. The page
  // can then tell the person it is slow instead of showing an error.
  busy: boolean
  // A grab refused because another download is in the way: which one.
  blocking?: BlockingDownload
  // A stable word for the few errors the page does more with than show, such
  // as "torrents_off" (a torrent grab refused because torrents are switched off).
  code?: string
  constructor(status: number, message: string, busy = false, blocking?: BlockingDownload, code?: string) {
    super(message)
    this.status = status
    this.busy = busy
    this.blocking = blocking
    this.code = code
  }
}

// A read that gets no answer at all in this long is given up on.
const READ_TIMEOUT_MS = 30_000
const SLOW_MESSAGE = t('Mediarium is slow to answer right now. Try again in a moment.')
const NO_ANSWER_MESSAGE = t('Could not reach Mediarium. Check that it is running, then try again.')

// What a read answered last time, for the lists that are asked for again and
// again (see `revalidate` below): its tag and the data that came with it.
const lastAnswer = new Map<string, { etag: string; data: unknown }>()

// A read that is already on its way, so two parts of a page asking for the same
// list at the same moment share one request.
const inFlight = new Map<string, Promise<unknown>>()

type ReadOptions = RequestInit & {
  // For a list the server tags (ETag): the tag from the last answer is sent
  // back, and when nothing changed the server answers "304" with no body, so
  // the same data comes back at once, nothing is read again, and a page can see
  // from the identical object that nothing needs redrawing.
  revalidate?: boolean
}

function request<T>(path: string, init?: ReadOptions): Promise<T> {
  if (!init?.revalidate || init.signal) return send<T>(path, init)
  const running = inFlight.get(path) as Promise<T> | undefined
  if (running) return running
  const call = send<T>(path, init).finally(() => inFlight.delete(path))
  inFlight.set(path, call)
  return call
}

async function send<T>(path: string, init?: ReadOptions): Promise<T> {
  const { revalidate, ...rest } = init ?? {}
  const before = revalidate ? lastAnswer.get(path) : undefined
  const isRead = !rest.method || rest.method === 'GET'
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
      headers: { ...(rest.body ? { 'Content-Type': 'application/json' } : {}), ...(before ? { 'If-None-Match': before.etag } : {}) },
      ...rest,
      signal: signalFor(rest.signal, isRead ? READ_TIMEOUT_MS : undefined),
    })
    if (res.status === 304 && before) {
      noteFine()
      return before.data as T
    }
    text = await res.text()
  } catch (e) {
    // The page cancelled this call itself (for example a search that was replaced): that says nothing about the server.
    if (rest.signal?.aborted) throw e
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
  noteEndReason(res.status, text)
  if (sessionEnded(path, res.status, text)) noteSessionEnded()
  let data: { error?: string; busy?: boolean; blocking?: BlockingDownload; code?: string } | null = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    // Not JSON (for example a plain "404 page not found"): report the status instead of a parser error.
    if (res.ok) throw new ApiError(res.status, t('The server sent an unexpected reply.'))
    throw new ApiError(res.status, res.status === 404 ? t('This is not available in this version yet.') : t('Something went wrong on the server (error {status}).', { status: String(res.status) }))
  }
  if (res.status === 503 && data?.busy) {
    noteBusy()
    throw new ApiError(503, st(data.error) || SLOW_MESSAGE, true)
  }
  if (!res.ok) {
    throw new ApiError(res.status, st(data?.error) || t('Something went wrong on the server (error {status}).', { status: String(res.status) }), false, data?.blocking, typeof data?.code === 'string' ? data.code : undefined)
  }
  noteFine()
  // Covers Mediarium serves itself come as "/api/...": under a URL base they
  // need the base in front, like every other address the page uses.
  rebasePictures(BASE_PATH, data)
  if (revalidate && res.ok) {
    const etag = res.headers.get('ETag')
    if (etag) lastAnswer.set(path, { etag, data })
    else lastAnswer.delete(path)
  }
  return data as T
}

const get =<T>(path: string) => request<T>(path)
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

// What adding a download by hand answers with.
export interface ManualDownloadResult {
  queueId: number
  releaseTitle: string
  protocol: 'usenet' | 'torrent'
  sizeBytes: number
}

// A video file in a folder that can be imported by hand.
export interface ManualFile {
  path: string
  size: number
  modified: string
  title?: string
  year?: number
  season?: number
  episode?: number
  quality?: string
  incomplete?: boolean
}

// What a look in a folder for files to import by hand found.
export interface ManualList {
  folder: string
  downloadsFolder: string
  files: ManualFile[]
  more?: boolean
  hiddenIncomplete?: number
}

// How a file comes in by hand: a hardlink (a copy on another drive, like every
// import), a copy of its own, or a move (the original is removed).
export type ManualMode = 'link' | 'copy' | 'move'

// The folders inside a folder, for choosing where to look.
export interface ManualFolders {
  path: string
  parent?: string
  folders: { name: string; path: string }[]
  shortcuts?: { name: string; path: string }[]
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
  // Every download counted since the statistics began or were last reset; clearing the history does not change them.
  totals: { grabs: number; completed: number; failed: number; bytes: number }
  historyKeptDays: number
  watched?: WatchStats // only while reading what's been watched is on
}

export interface WatchedStatTitle {
  kind: 'movie' | 'series'
  id: number
  tmdbId?: number
  title: string
  plays: number
  episodes?: number
  lastPlayed?: string
}

export interface WatchStats {
  lastSync?: string
  moviesWatched: number
  moviesUnwatched: number
  episodesWatched: number
  episodesUnwatched: number
  unwatchedBytes: number
  plays: number
  top: WatchedStatTitle[]
  recent: WatchedStatTitle[]
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

// A viewer can look but never change anything.
export type Role = 'admin' | 'member' | 'viewer'

// Where an account's sign-in lives: a password here, or single sign-on / the directory.
export type AccountSource = 'local' | 'oidc' | 'ldap'

// How release names number a show's episodes.
export type SeriesType = 'standard' | 'anime' | 'daily'

// A show's AniList / AniDB / MyAnimeList ids and what the numbering services say about it.
export interface SeriesNumberingEntry {
  anidb: number
  anilist?: number
  mal?: number
  type?: string // TV, ONA, OVA, SPECIAL
  season: number // the season of the show it sits in (0: specials, -1: not placed)
  offset?: number // added to its episode numbers to get the episode in that season
  name?: string
}

export interface CustomNumberingRule {
  librarySeason: number
  releaseSeason: number
  offset?: number
  from?: number
  to?: number
}
export interface CustomNumberingBody {
  rules: CustomNumberingRule[]
  names: string[]
}

export interface SeriesNumbering {
  lookup: boolean // anime numbering is read on its own (Settings)
  anilist?: number
  anidb?: number
  mal?: number
  anilistUrl?: string
  anidbUrl?: string
  malUrl?: string
  sceneState: '' | 'mapped' | 'none' | 'offline' // TheXEM; '' means never looked up
  sceneRows: number
  sceneDiffers: number // how many of those are numbered differently from the show
  listsState: '' | 'found' | 'none' | 'offline' // the anime lists
  entries: SeriesNumberingEntry[]
  names: string[] // other names the show is known by
  note?: string
  checkedAt?: string
  refreshing?: boolean
}

// What a basic account may do (Settings > Account & Access). Administrators may do everything.
export interface Permissions {
  addDirect: boolean // add titles straight away; off = ask an administrator
  movies: boolean
  tv: boolean
  music: boolean
  books: boolean
  releases: boolean // search indexers and pick releases
  manage: boolean // search now, monitoring, tags, following
  retry: boolean
  subtitles: boolean
  play: boolean // play, preview, read and listen
}
export type Permission = keyof Permissions

export const DEFAULT_PERMISSIONS: Permissions = { addDirect: true, movies: true, tv: true, music: true, books: true, releases: true, manage: true, retry: true, subtitles: true, play: true }

export interface User {
  id: number
  username: string
  isAdmin?: boolean
  role?: Role
  source?: AccountSource
  oidcLinked?: boolean // tied to a single sign-on account
  name?: string
  email?: string
  permissions?: Permissions
}

// can reports whether the signed-in account may do something. Administrators
// may do everything; an older server that sends no permissions allows all.
export function can(u: User | null | undefined, p: Permission): boolean {
  if (!u) return false
  if (isAdmin(u)) return true
  return u.permissions ? u.permissions[p] !== false : true
}

// True for administrators. Older servers sent only isAdmin, and a missing
// role is treated as admin so nothing disappears for a single-account setup.
export function isAdmin(u: User | null | undefined): boolean {
  if (!u) return false
  if (u.role) return u.role === 'admin'
  return u.isAdmin !== false
}

// True for a viewer: an account that can look but never change anything.
export function isViewer(u: User | null | undefined): boolean {
  return !!u && u.role === 'viewer'
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
  permissions?: Permissions
  twoStep?: boolean // two-step sign-in is on
  disabled?: boolean // turned off: cannot sign in
  source?: AccountSource
}

export interface NewAccount {
  username: string
  password: string
  name?: string
  email?: string
  role: Role
  permissions?: Permissions
}

export interface AccountChanges {
  name?: string
  email?: string
  role?: Role
  disabled?: boolean
  password?: string
  permissions?: Permissions
}

// What an add answers for an account that has to ask (Settings > Account & Access).
export interface Requested {
  requested: true
  message: string
}
export function isRequested(x: unknown): x is Requested {
  return typeof x === 'object' && x !== null && (x as { requested?: unknown }).requested === true
}

// A title a basic account asked for (Activity > Requests).
export interface TitleRequest {
  id: number
  kind: 'movie' | 'tv' | 'music' | 'book'
  title: string
  year?: number
  poster?: string
  requestedBy?: number
  requester?: string
  status: 'pending' | 'approved' | 'declined'
  note?: string
  createdAt: string
  decidedAt?: string
  // Asked for the 4K second copy as well (Settings > Account & Access > All accounts).
  fourK?: boolean
}

// How many requests a person has left (limit 0 = no limit).
export interface RequestQuota {
  limit: number
  used: number
  left: number
  period: 'day' | 'week'
  resetsAt?: string
}

// What the request pages need to know about the signed-in account.
export interface RequestStatus {
  quota: RequestQuota
  fourK: { movie: boolean; tv: boolean }
  fourKLabel: string
  fourKProfileId: number
  reports: boolean
  notifyOn: boolean
  asksToAdd: boolean
}

export interface RequestOptions {
  notifyRequester: boolean
  quotaCount: number
  quotaPeriod: 'day' | 'week'
  fourK: boolean
  fourKProfileId: number
  reports: boolean
}

export interface RequestAccount {
  id: number
  name: string
  admin: boolean
  mustAsk: boolean
  quota: number // -1 follows the default, 0 is no limit
}

export interface RequestSettings {
  options: RequestOptions
  accounts: RequestAccount[]
  folders: { movie: boolean; tv: boolean }
}

export type IssueCategory = 'wrong-file' | 'no-sound' | 'bad-subtitles' | 'bad-picture' | 'other'

// A reported problem with a title (Activity > Reports).
export interface Issue {
  id: number
  kind: 'movie' | 'tv'
  itemId: number
  tmdbId: number
  title: string
  year?: number
  poster?: string
  category: IssueCategory
  episode?: string
  message?: string
  reportedBy?: number
  reporter?: string
  status: 'open' | 'resolved'
  comment?: string
  createdAt: string
  resolvedAt?: string
}

export interface RequestNotices {
  enabled: boolean
  serverOn: boolean
  hasEmail: boolean
  emailReady: boolean
  devices: { id: number; name: string; createdAt: string }[]
}

// The connected Simkl account (the sign-in itself never leaves the server).
export interface SimklStatus {
  connected: boolean
  unreadable: boolean
  hasClientId: boolean
}

// Scheduled import lists (Settings > Info, lists and subtitles > Import lists).
export interface ImportListConfig {
  url?: string
  chart?: string
  count?: number
  genre?: number
  language?: string
  csv?: string
  // Simkl: which of the account's lists to read (plantowatch, watching, completed, hold or dropped)
  status?: string
}

export interface ImportList {
  id: number
  name: string
  source: string
  media: 'movie' | 'tv' | 'both'
  config: ImportListConfig
  hasCsv: boolean
  hasKey: boolean
  enabled: boolean
  intervalHours: number
  profileId: number
  movieRoot: string
  tvRoot: string
  monitored: boolean
  searchNow: boolean
  tags: string[]
  minRating: number
  minYear: number
  maxPerRun: number
  startFresh: boolean
  createdAt: string
  lastRunAt?: string
  lastStatus?: 'ok' | 'error'
  lastMessage?: string
  lastFound: number
  lastAdded: number
  lastSkipped: number
  totalAdded: number
  nextRunAt?: string
  running: boolean
}

export interface ImportListInput {
  name: string
  source: string
  media: 'movie' | 'tv' | 'both'
  config: ImportListConfig
  key?: string
  enabled: boolean
  intervalHours: number
  profileId: number
  movieRoot: string
  tvRoot: string
  monitored: boolean
  searchNow: boolean
  tags: string[]
  minRating: number
  minYear: number
  maxPerRun: number
  startFresh: boolean
}

export interface ImportListSource {
  id: string
  label: string
  media: 'both' | 'movie'
  help: string
}

// A Plex watchlist an account connected, shown next to the lists.
export interface PlexWatchlistRow {
  userId: number
  account: string
  plexName?: string
  autoAdd: boolean
  checkedAt?: string
  lastError?: string
  added: number
  requested: number
}

export interface ImportListsAnswer {
  lists: ImportList[]
  sources: ImportListSource[]
  plexWatchlists: PlexWatchlistRow[]
}

export interface ImportListPreview {
  found: number
  movies: number
  shows: number
  unknown: number
  sample: string[]
}

// The name to greet someone by: their name, or their username when no name is set.
export function displayName(u: User | null | undefined): string {
  if (!u) return ''
  return (u.name ?? '').trim() || u.username
}

export interface APIKey {
  id: number
  name: string
  readOnly?: boolean // may only be used for GET requests
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

// The tokens a custom file name can use and the ready-made formats to start
// from (GET /api/settings/naming-tokens).
export interface NamingTokens {
  tokens: { token: string; kind: 'movie' | 'tv' | 'both'; group: string }[]
  schemes: { id: string; name: string; movie: string; episode: string }[]
  // Ready-made folder formats for a new movie and a new show.
  folderSchemes?: { id: string; name: string; movie: string; series: string }[]
}

// Another program that runs next to Mediarium, with a link to it (Settings > Add-ons).
export interface AppLink {
  id: string
  name: string
  url: string
  mode: 'tab' | 'frame'
  icon: string
  enabled: boolean
}

// "Quiet for a while": whether the heavy background work is held, and until when (RFC 3339).
export interface QuietState {
  active: boolean
  until?: string
}

// One line of the speed schedule: on these days (0 is Sunday; none = every day), from one hour to another (the same hour twice = all day), limit to this many MB/s (0 = none).
export interface SpeedRule {
  days: number[]
  from: number
  to: number
  limitMB: number
}

export interface Settings {
  moviesPath: string
  tvPath?: string
  downloadsPath: string
  // Where the unfinished part of downloads is worked on; '' = in the downloads folder.
  downloadsTempPath?: string
  namingPreset: string
  movieNameFormat: string
  episodeNameFormat?: string
  // The folder formats for a new movie and a new show (Custom style); '' = the usual "Title (Year)".
  movieFolderFormat?: string
  seriesFolderFormat?: string
  // Imported videos keep the file name they came with (folders are named as usual).
  keepOriginalNames?: boolean
  checkDuration?: boolean // turn away a download whose video is far shorter than its episode or movie
  hasTmdbApiKey: boolean
  traktClientId?: string
  hasTraktClientId: boolean
  simklClientId?: string
  hasSimklClientId?: boolean
  onboardingDone: boolean
  torrentListenPort?: string
  torrentSeedRatioLimit?: string
  torrentSeedTimeLimitH?: string
  automationEnabled?: boolean
  defaultProfileId?: number
  defaultTvProfileId?: number // the profile shows use when they have none of their own; 0 = the same as movies
  defaultSources?: SourcePref
  qualityLanguage?: string
  qualityLanguageOnly?: boolean // only take releases that say they are in one of those languages
  infoLanguage?: string // the TMDB language titles are read in, like "de-DE"
  infoLanguages?: { tag: string; name: string }[]
  releaseCountry?: string // the country release dates are read for, like "IN"; "" = the country of the info language
  releaseCountries?: string[]
  releaseCountryEffective?: string // the country really used
  hasOpenSubtitlesApiKey?: boolean
  movieCopiesPath?: string // where second copies of movies go; "" = off
  tvCopiesPath?: string
  subdlApiKey?: string // write only; "" removes it
  hasSubdlApiKey?: boolean
  hasSubtitleAddons?: boolean // at least one subtitle add-on is added
  // The on/off switch of each subtitle source (on unless switched off). A source can only be switched on once it has a key (add-ons: one added).
  openSubtitlesEnabled?: boolean
  subdlEnabled?: boolean
  subtitleAddonsEnabled?: boolean
  // Read only: where each source stands right now.
  subtitleSources?: SubtitleSourceStatus[]
  subtitleCountEmbedded?: boolean // subtitle tracks inside the video count as there (on by default)
  subtitleWizdom?: boolean // also look for Hebrew subtitles on Wizdom (off by default)
  showAdvanced?: boolean // show custom formats and format scores on the Quality page
  trashFormatsAuto?: boolean // read the TRaSH Guides on a schedule and update the formats that came from them
  trashFormatsEveryDays?: number // 1 (every day, the default) or 7 (every week)
  trailerBackground?: boolean // play the trailer behind movie and show pages for everyone (on by default)
  trailerSound?: boolean // start that trailer with its sound on (off by default)
  unmonitorDownloaded?: boolean // stop monitoring a movie once it is downloaded (off by default)
  unmonitorFinished?: boolean // stop monitoring a show once it has ended and every episode is in (off by default)
  foldersInInfoLanguage?: boolean // name new movie and show folders and files with the title in the info language (off by default)
  seasonPacks?: boolean // look for a season pack once a season has ended (off by default)
  tvmazeAirTimes?: boolean // look up the time episodes air on TVmaze (on by default; off = TVmaze is never contacted)
  hasOpenSubtitlesAccount?: boolean
  openSubtitlesAccountName?: string
  tmdbKeyBuiltIn?: boolean
  tmdbUsingOwnKey?: boolean
  openSubtitlesUsingOwnKey?: boolean
  traktUsingOwnKey?: boolean
  simklUsingOwnKey?: boolean
  openSubtitlesKeyBuiltIn?: boolean
  traktClientIdBuiltIn?: boolean
  simklClientIdBuiltIn?: boolean
  subtitleLanguages?: string[]
  // Which kind of subtitle to look for, per language code. A language that is not listed wants a normal or SDH one.
  subtitleProfiles?: Record<string, SubtitleProfile>
  // Look up scene and anime numbering for anime on its own (on by default)
  numberingLookup?: boolean
  subtitleAutoDownload?: boolean
  notifyTrashSyncAlways?: boolean // also send the TRaSH Guides message when a check changed nothing
  subtitleUpgrade?: boolean // later swap a subtitle for one made for the exact file (with automatic downloading)
  // The master switch for subtitles; off until switched on.
  subtitlesEnabled?: boolean
  illegalCharMode?: string
  illegalCharReplacement?: string
  requireVpnForTorrents?: boolean
  // Send indexer traffic through the VPN; off until switched on.
  routeIndexersThroughVpn?: boolean
  // UDP trackers and DHT through the built-in VPN; off until switched on.
  discoverThroughTunnel?: boolean
  // The person said their torrent client is already behind a VPN: no "Torrenting without a VPN" notice.
  vpnNoticeAcknowledged?: boolean
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
  newestFirst?: boolean // among the automatic downloads, newer releases start first
  resumeInterrupted?: boolean // downloads a restart interrupted carry on by themselves
  speedLimitHours?: string
  speedSchedule?: SpeedRule[] // lines of the speed schedule; the first that fits the moment sets the limit
  downloadHours?: string // when new downloads may start, "1-7"; "" = any time
  minFreeGB?: number
  moviesExtraPaths?: string[] // more movie folders next to the main one
  tvExtraPaths?: string[] // more TV folders next to the main one
  // Where a title added without a folder choice goes: "" the main folder, an
  // extra folder's path, or "most-room" for whichever has the most free space.
  moviesNewFolder?: string
  tvNewFolder?: string
  torrentStallHours?: number // hours a torrent may get no data before it is given up on; 0 = never; -1 or absent = each client's own default
  notifyQuietHours?: string
  digestDay?: number // the day of the weekly summary, 0 is Sunday; -1 = none
  digestHour?: number
  // How often, in minutes, the connection watch checks your providers and indexers; 0 is off.
  monitorIntervalMinutes?: number
  publicUrl?: string // the address Mediarium is opened at; links in notification messages use it
  huntIntervalHours?: number // hours between searches for missing items and better versions, 1 to 168
  releaseCheckMinutes?: number // minutes between checks of each indexer's newest releases, 5 to 1440
  downloadsAtOnce?: number // how many downloads run at the same time, 1 to 500 (the rest wait in line)
  usenetRetentionDays?: number // Usenet posts older than this many days are not taken; 0 = no limit
  rejectExecutables?: boolean // refuse releases that hold programs or scripts; on unless switched off
  skipDuplicates?: boolean // do not download the same release twice; off unless switched on
  usenetPrecheck?: boolean // ask the news servers whether the articles exist before downloading; off unless switched on
  unsafeExtensions?: string[] // the file endings rejectExecutables looks for (read-only)
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
  // A site from the list that runs on the person's own network: its address can be changed.
  localSite?: boolean
  lastTestError?: string
  lastTestAt?: string
  // 1 to 50, lower is tried first (default 25)
  priority?: number
  // The fewest seeders a torrent from this indexer needs to be grabbed by itself (0 = off)
  minSeeders?: number
  // Extra category numbers searched besides the standard ones, per kind of media
  extraCategories?: Record<string, number[]> | null
  // Set after repeated failures: searches skip the indexer until pausedUntil.
  failures?: number
  pausedUntil?: string
  pausedNote?: string
  pausedWhy?: string
  // Daily limit: the number a person set (0 = none), the limit in use (set or
  // learned from the indexer, 0 = none known) and the requests made today.
  requestsPerDay?: number
  dailyLimit?: number
  limitLearned?: boolean
  requestsToday?: number
  // Set while automatic searches leave the indexer alone, in plain words.
  limitNote?: string
  // How often the check for new releases reads this indexer when its daily limit
  // makes that less often than the general setting, and the same in plain words.
  rssEveryMinutes?: number
  rssNote?: string
}

// The delay profile (Settings > Languages and automation): how long automatic
// searches wait for the protocol you like better. 0 minutes = no wait.
export interface DelayProfile {
  preferredProtocol: 'usenet' | 'torrent'
  usenetMinutes: number
  torrentMinutes: number
  bypassIfHighestQuality: boolean
}

// A release an automatic search is holding back for now.
export interface WaitingRelease {
  movieId?: number
  seriesId?: number
  season?: number
  episode?: number // 0 = a whole season
  release: string
  protocol: 'usenet' | 'torrent'
  until: string
  note: string
}

// One quality's size limits in MB per minute of runtime; max 0 = no upper limit.
export interface SizeRow {
  quality: string
  min: number
  max: number
  defaultMin: number
  defaultMax: number
}
// The sizes the TRaSH Guides recommend, mapped onto the size table (max 0 = no limit).
export interface TrashSizes {
  list: string
  // The size table the list is for.
  kind?: 'movie' | 'tv'
  rows: { quality: string; min: number; max: number }[]
  skipped: string[]
}

export interface SizeTable {
  enabled: boolean
  rows: SizeRow[]
}

// An indexer's searches over the last days, for the Statistics page.
export interface IndexerStat {
  id: number
  name: string
  protocol: 'usenet' | 'torrent'
  enabled: boolean
  queries: number
  failures: number
  results: number
  grabs: number
  successRate: number // 0 to 1, -1 when it was not searched
  pausedUntil?: string
}

export interface IndexerDefinitionSetting {
  name: string
  label?: string
  type: string
  default?: string | boolean | number | null
  options?: { value: string; label: string }[]
}

// The debrid service as saved. canCheck: the service can say which releases it
// already has (TorBox, Premiumize). preferCached asks it and ranks a cached release ahead of
// an equal one; cachedLowerScore lets the cached one win even with a lower score.
export interface DebridSettings {
  service: '' | 'realdebrid' | 'torbox' | 'premiumize'
  hasKey: boolean
  canCheck: boolean
  preferCached: boolean
  cachedLowerScore: boolean
}

// The debrid service's account, as Test reports it.
export interface DebridAccount {
  name: string
  premium: boolean
  expires?: string
  // How much of the monthly fair-use limit is used, 0 to 1 (Premiumize).
  fairUse?: number
}

// An external download client torrent releases go to (Settings > Downloading).
export type DownloadClientKind = '' | 'qbittorrent' | 'transmission' | 'rdtclient' | 'decypharr'
export interface DownloadClientSettings {
  kind: DownloadClientKind
  url: string
  username: string
  hasPassword: boolean
  category: string
  categoryMovies: string
  categoryTv: string
  categoryMusic: string
  categoryBooks: string
  remotePath: string
  localPath: string
  removeAfterImport: boolean
  removeOnStop: boolean
  linkRoot: string
}
export type DownloadClientInput = Omit<DownloadClientSettings, 'hasPassword'> & { password: string }

// An external Usenet downloader Usenet releases can go to (Settings > Downloading > Usenet).
export type UsenetClientKind = '' | 'sabnzbd' | 'nzbget'
export type UsenetClientPriority = 'low' | 'normal' | 'high' | 'force'
export interface UsenetClientSettings {
  kind: UsenetClientKind
  use: boolean
  url: string
  hasApiKey: boolean
  username: string
  hasPassword: boolean
  category: string
  categoryMovies: string
  categoryTv: string
  categoryMusic: string
  categoryBooks: string
  priority: UsenetClientPriority
  sendUrl: boolean
  remotePath: string
  localPath: string
  removeAfterImport: boolean
  removeOnStop: boolean
}
export type UsenetClientInput = Omit<UsenetClientSettings, 'hasApiKey' | 'hasPassword'> & { apiKey: string; password: string }
// One download in SABnzbd or NZBGet, as the Activity page shows it.
export interface UsenetClientJob {
  id: string
  name: string
  stage: 'downloading' | 'queued' | 'paused' | 'processing' | 'done' | 'problem'
  stageText: string
  progress: number
  sizeBytes: number
  downSpeed: number
  etaSeconds: number
  category?: string
  mine: boolean
  queueId?: number
  canPause: boolean
  canResume: boolean
  addedAt?: string
  failMessage?: string
}
export interface UsenetClientJobsAnswer {
  configured: boolean
  active: boolean
  kind?: string
  label?: string
  category?: string
  error?: string
  jobs: UsenetClientJob[]
}
// Sent on window when the place torrent releases go to changes (the debrid
// card or the download client card), so the other card shows it was turned off.
export const TORRENT_ROUTE_EVENT = 'mediarium:torrent-route'

// A connected Plex watchlist (Your profile).
export interface PlexWatchlistStatus {
  connected: boolean
  name?: string
  autoAdd: boolean
  checkedAt?: string
  lastError?: string
  added: number
  requested: number
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
  // A definition file an administrator added themselves.
  custom?: boolean
  // A site that runs on the person's own network (every address it lists is local): they type the address of their own server.
  local?: boolean
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
  // secretError says the saved password can't be read (a secret.key that doesn't belong to this database).
  secretError?: string
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
  usenet: { servers: number; enabledServers: number; ready: boolean; external?: 'sabnzbd' | 'nzbget'; externalLabel?: string }
  torrent: {
    state?: 'disabled' | 'blocked' | 'ready'
    via?: 'builtin' | 'client' | 'debrid' // where torrents go
    viaLabel?: string // the client or service, when it isn't the built-in one
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

// The built-in torrent client's controls.
export interface TorrentFile {
  index: number
  path: string // inside the torrent, with forward slashes
  size: number
  done: number
  wanted: boolean
}

export interface TorrentDetail {
  live: boolean // the built-in client is running this torrent now
  files: TorrentFile[]
  sequential: 'default' | 'on' | 'off'
  sequentialDefault: boolean
  downLimitKB: number // KB/s for this torrent, 0 = none of its own
  upLimitKB: number
  category?: string
  filesOpen: boolean // files can still be ticked on and off
}

export interface TorrentCategory {
  name: string
  subfolder: string
}

export type PortMapState = 'off' | 'tunnel' | 'idle' | 'working' | 'mapped' | 'none'

export interface TorrentOptions {
  sequentialDefault: boolean
  skipJunk: boolean
  packOnlyWanted: boolean
  uploadLimitKB: number | null // KB/s; null = no limit, 0 = no uploading at all
  portMapping: { enabled: boolean; state: PortMapState; method?: string; port?: number; externalIp?: string }
  allowLocalAddresses: boolean // trackers, web seeds and magnet metadata may be on the home network (read when the client starts)
  categories: TorrentCategory[]
  categoryByKind: Partial<Record<'movie' | 'tv' | 'music' | 'book', string>>
}

// A torrent indexer's own seeding rule. null follows the global limit; 0 means don't seed; -1 means no limit, even when the global one has a limit.
export interface SeedRule {
  ratio: number | null
  hours: number | null
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
  // The debrid service already has this torrent, so it starts at once. Only
  // set when it is known (TorBox, Premiumize).
  cached?: boolean
  // A torrent with fewer seeders than its indexer's minimum: automatic downloads
  // skip it, grabbing it by hand is allowed. minSeeders is that minimum.
  belowMinSeeders?: boolean
  minSeeders?: number
  season?: number
  episodes?: number[]
  blocklisted?: boolean
  rejections?: string[]
  // The custom-format score in the title's profile and the formats matched
  // (only when the profile scores formats).
  formatScore?: number
  // The whole score the pick compares: the custom formats, the preferred terms
  // and fixScore added up. The Score column shows it. Only when the profile
  // scores something.
  score?: number
  // The bonus of a PROPER or REPACK when the profile skips bad releases.
  fixScore?: number
  // The matched formats with the score the profile gives each, the profile's
  // minimum score and the score it stops upgrading at (0 when not set), and
  // everything that makes an automatic search skip the release, by kind.
  formats?: { name: string; score: number }[]
  // The preferred terms of the profile the release matched, with their scores.
  terms?: { term: string; score: number }[]
  minScore?: number
  upgradeUntil?: number
  reasons?: { kind: string; text: string }[]
  // The release automatic search would take (set on a search for a title in the library).
  best?: boolean
  // Which profile would take this release: the title's own, or one of its fallbacks.
  acceptedBy?: { profileId: number; profileName: string; fallback: boolean }
  // The audio languages the release names ("Italian + English", "Multi") and how
  // they suit the preferred language: ok, mixed (adds another language) or other
  // (clearly another language only, so automation skips it).
  language?: string
  languageFit?: 'ok' | 'mixed' | 'other'
  // The other title of the movie or show the release is named by ("Der Herr der
  // Ringe"), when it is not the main one. Only set for a search on a library item.
  matchedTitle?: string
  // The indexer's category numbers of the release. Only set for a book search.
  categories?: number[]
}

export type MediaStatus = 'missing' | 'downloading' | 'downloaded'

export interface PreferredTerm {
  term: string
  score: number
}

// One rank of a profile's own quality order: a quality, or a named group of them.
export interface QualityItem {
  name?: string
  tiers: string[]
}

export interface QualityProfileInput {
  name: string
  allowed: string[]
  cutoff: string
  // The profile's own order of qualities and groups, best first. [] is the
  // built-in order; left out on a save, the saved order is kept. When it is
  // set it decides which qualities are allowed.
  qualityOrder?: QualityItem[]
  upgradeAllowed: boolean
  mustContain: string[]
  mustNotContain: string[]
  preferred: PreferredTerm[]
  // Profiles to try, in order, when this one finds nothing acceptable.
  fallback?: number[]
  // The largest release accepted, in GB (0 = no limit).
  maxSizeGB?: number
  // Custom formats: a score per format, the lowest total a release needs,
  // and the total at which better-scoring releases stop being looked for (0 = never).
  formatScores?: FormatScore[]
  minFormatScore?: number
  cutoffFormatScore?: number
  // Whole-season releases are never downloaded.
  noSeasonPacks?: boolean
  // Leaves out whole discs, 3D, upscales, burned-in subtitles and known
  // low-quality re-encoders (on unless switched off).
  skipBadReleases?: boolean
}

export interface StatsDay {
  day: string
  movies: number
  moviesHave: number
  shows: number
  episodesHave: number
  albumsHave: number
  libraryBytes: number // -1 before snapshots began
  completed: number
  failed: number
  downloadBytes: number
  estimated?: boolean
}

export interface ProxySettings {
  type: '' | 'socks5' | 'http' // '' = no proxy
  host: string
  port: number
  username: string
  hasPassword: boolean
  torrents: boolean
  indexers: boolean
  usenet: boolean
}

export type ProxyInput = Omit<ProxySettings, 'hasPassword'> & { password?: string }

export interface FormatScore {
  formatId: number
  score: number
}

export interface FormatCondition {
  name: string
  implementation: string
  negate: boolean
  required: boolean
  value?: string
  min?: number
  max?: number
}

export interface CustomFormat {
  id: number
  name: string
  app: 'radarr' | 'sonarr'
  trashId?: string
  conditions: FormatCondition[]
  problems?: string[]
  usedBy: string[]
}

export interface FormatImportResult {
  added: number
  updated: number
  formats: CustomFormat[]
  missing?: string[]
  // What happened to the scores in the chosen profile (when one was chosen).
  profile?: ProfileScoreChange
}

export interface FormatAssignResult {
  profiles: number
  scored: number
  unscored: string[]
  changes: ProfileScoreChange[]
}

export interface TrashFormatChange {
  id: number
  name: string
  conditions: boolean
  scoreChanged: boolean
  scoreFrom: number
  scoreTo: number
  profiles: string[]
}

// What one look at the TRaSH Guides found (the button, or the daily or weekly check).
export interface TrashCheck {
  at: string
  error?: string
  updated: number
  upToDate: number
  scores: number
  profiles: number
  names: string[]
  daily: boolean
}

export interface TrashUpdateResult {
  changes: TrashFormatChange[]
  checked: number
  check?: TrashCheck
}

export interface TrashUpdateStatus {
  last: TrashCheck | null
  next?: string
}

export interface TrashFormat {
  trashId: string
  name: string
  defaultScore: number
  importedId: number
}

export interface TrashProfile {
  trashId: string
  name: string
  description: string
  qualities: string[]
  cutoff: string
  formats: number
  minFormatScore: number
  cutoffFormatScore: number
}

export interface FormatTestResult {
  matched: { id: number; name: string; score?: number }[]
  quality: string
  score?: number
  accepted?: boolean
  reason?: string
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

// A torrent in the download client (Activity > Download clients).
export interface ClientTorrent {
  hash: string
  name: string
  stage: 'downloading' | 'waiting' | 'queued' | 'checking' | 'paused' | 'seeding' | 'done' | 'problem'
  stageText: string
  progress: number
  sizeBytes: number
  downSpeed: number
  upSpeed: number
  seeds: number
  peers: number
  swarmSeeds: number
  swarmPeers: number
  etaSeconds: number
  ratio: number
  addedAt?: string
  idleSeconds: number
  category?: string
  mine: boolean
  queueId?: number
  canPause: boolean
  canResume: boolean
  downloadedBytes: number
  uploadedBytes: number
  savePath?: string
}
export interface ClientTorrentsAnswer {
  configured: boolean
  kind?: string
  label?: string
  category?: string
  error?: string
  torrents: ClientTorrent[]
}
export interface DebridTorrent {
  id: string
  name: string
  stage: 'downloading' | 'waiting' | 'ready' | 'problem'
  stageText: string
  progress: number
  sizeBytes: number
  downSpeed: number
  seeders: number
  addedAt?: string
}
export interface DebridTorrentsAnswer {
  configured: boolean
  label?: string
  error?: string
  torrents: DebridTorrent[]
}

export interface AddMovieOptions {
  profileId?: number
  monitored?: boolean
  sources?: SourcePref
  searchNow?: boolean
  // Leave it alone once it is downloaded. The server treats a missing value as true.
  noUpgrade?: boolean
  // The library folder to keep it in, when there is more than one (left out = the main one).
  rootPath?: string
  // Only for an account that has to ask: also ask for the 4K second copy.
  fourK?: boolean
}

export interface AddSeriesOptions {
  profileId?: number
  monitor?: 'all' | 'future' | 'none'
  sources?: SourcePref
  searchNow?: boolean
  noUpgrade?: boolean
  // The library folder to keep it in, when there is more than one (left out = the main one).
  rootPath?: string
  fourK?: boolean
}

/** A show's own choice about taking a season pack once a season has ended: "" follows the setting. */
export type SeasonPackChoice = '' | 'on' | 'off'

export interface Series {
  id: number
  rootPath?: string // the library folder chosen for it (left out = the main one)
  folderPath?: string // the full folder of its files, as Mediarium sees it (single show only)
  copyOf?: number // the main show's id when this is a second copy
  copies?: number // how many second copies a main show has
  tags?: string[]
  sources?: SourcePref
  seasonPack?: SeasonPackChoice // the show's own choice about season packs once a season has ended
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
  seriesType?: SeriesType // how releases number the episodes
  firstAirDate?: string // "2022-02-18", for sorting
  sizeBytes?: number // the downloaded episodes on disk
  showStatus?: 'ended' | 'canceled' | 'continuing' | 'upcoming' // where the show stands; left out until it has been refreshed once
  originCountry?: string[] // where it comes from ("US", "GB"); empty until the show is refreshed
  nameTag?: string // tells it from another show of the same name in the library ("US", "UK" or the year)
  media?: MediaSummary // what the downloaded episodes' files hold, once they have been read
  shortFiles?: number // how many of its files are far shorter than their episodes should be
}

// What a video file holds, read from its headers (see fileInfo.ts).
export interface FileVideo {
  codec: string // "H.264", "HEVC", "AV1"...
  width?: number
  height?: number
  bitDepth?: number
  hdr?: string // "SDR", "HDR10", "HDR10+", "Dolby Vision", "HLG"
  dvProfile?: number
  fps?: number
}
export interface FileAudio {
  codec: string // "AAC", "AC-3", "TrueHD"...
  channels?: number
  lang?: string // "en"
  title?: string
  default?: boolean
  forced?: boolean
  atmos?: boolean
}
export interface FileSubtitle {
  codec: string
  lang?: string
  title?: string
  default?: boolean
  forced?: boolean
}
// A file far shorter than its movie or episode should be: its length and the one it should have.
export interface ShortFileNote {
  minutes: number
  expected?: number
}
export interface FileInfo {
  container: string
  duration?: number // seconds
  video?: FileVideo
  audio?: FileAudio[]
  subtitles?: FileSubtitle[]
  resolution?: string // "2160p"
  scannedAt?: string
}
// The part of a title's files the Library filters on: the different values
// across its files (a show has many).
export interface MediaSummary {
  videoCodecs?: string[]
  hdr?: string[]
  resolutions?: string[]
  audioCodecs?: string[]
  audioLangs?: string[]
  subLangs?: string[]
}

export interface Episode {
  id: number
  season: number
  episode: number
  title?: string
  overview?: string
  airDate?: string
  airAt?: string // the exact air time (RFC 3339, UTC) once TVmaze has one
  status: MediaStatus
  quality?: string
  filePath?: string
  monitored: boolean
  sizeBytes?: number // the file on disk (downloaded episodes)
  fileInfo?: FileInfo // what the file holds, once it has been read
  shortFile?: ShortFileNote // the file is far shorter than the episode should be
}

// A second copy of a movie or show, kept in another quality in the copies folder.
export interface TitleCopy {
  id: number
  profileId: number
  profileName: string
  monitored: boolean
  status?: string // movies
  quality?: string
  sizeBytes?: number
  episodeCount?: number // shows
  downloadedCount?: number
}

export interface TitleCopies {
  folder: string // "" while no copies folder is set
  mainProfileId: number
  items: TitleCopy[]
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
  year?: number
  posterUrl?: string
  copy?: boolean
}

// A row of the grouped Wanted list: a movie, an episode (the only one of its
// show), or a show with how many of its episodes are listed.
export type WantedRow = Omit<WantedItem, 'kind'> & { kind: WantedItem['kind'] | 'show'; episodes?: number }

// One page of the grouped Wanted list, with the totals of the whole list.
export interface WantedPage {
  rows: WantedRow[]
  total: number // rows on all pages
  movies: number
  episodes: number
  offset: number
  limit: number
}

export interface WantedPageQuery {
  limit?: number // 0: the totals only
  offset?: number
  media?: 'movie' | 'tv'
  q?: string
}

export interface Movie {
  id: number
  rootPath?: string // the library folder chosen for it (left out = the main one)
  copyOf?: number // the main movie's id when this is a second copy
  copies?: number // how many second copies a main movie has
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
  releaseDate?: string // "2024-03-01", for sorting
  sizeBytes?: number // the file on disk
  media?: MediaSummary // what the file holds, once it has been read
  shortFile?: ShortFileNote // the file is far shorter than the movie should be
}

// What GET /api/library/owned answers (see useOwned).
export interface OwnedAnswer {
  movieCount: number
  showCount: number
  movies: { id: number; tmdbId: number; status: string; monitored: boolean }[]
  shows: { id: number; tmdbId: number; monitored: boolean; episodeCount: number; downloadedCount: number }[]
  queue: { movieId: number; seriesId: number; status: QueueItem['status']; progressPct: number }[]
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
  keptFile?: string // a finished download that was not imported because this file was kept
  keptData?: 'deleted' | 'seeding' | 'client' // what became of its downloaded files
  interrupted?: boolean // paused, but not by a person: Mediarium was restarted while it downloaded
  pending?: 'pausing' | 'stopping' // still winding down
  waiting?: boolean // an album whose files are downloaded and whose import waits for MusicBrainz
  keptFiles?: boolean // the partly downloaded files are still in the downloads folder
  queuePosition?: number // waiting in line: 1 is next
  indexerName?: string
  peers?: number // peers the built-in torrent client is connected to
  seeds?: number // seeds among them // where the release came from, when that indexer is still set up
  category?: string // the torrent category the built-in client filed it under
  resolution?: string // what the release name says: 1080p, 2160p...
  source?: string // WEB-DL, BluRay...
  codec?: string // x264, x265...
  language?: string // the audio languages the release names
  note?: string // a sentence from the server the status alone does not say (low disk space, a held-back release)
  unpack?: QueueUnpack // while the archives of a download are being unpacked
}

// How far unpacking has come: archive `index` of `count`, and the bytes done out
// of the total of that archive (0 when the total is not known).
export interface QueueUnpack {
  doneBytes: number
  totalBytes: number
  index: number
  count: number
}

// One folder of the file picker: the folders and video files inside the
// library folders. At the top (path "") the entries are the library folders.
export interface BrowseAnswer {
  path: string
  parent: string
  entries: { name: string; path: string; dir: boolean; size?: number }[]
}

// What changing a show's folder found.
export interface SeriesFolderReport {
  folder: string
  found: number
  total: number
  missing: number
  notPlaced: number
  message: string
}

// What looking at a title's files again found.
export interface RescanReport {
  found: number
  renamed: number
  missing: number
  skipped: number
  // For a movie that had no file: the file it was given, the folder that was
  // looked in when there was none to give, and whether a download in progress
  // made Mediarium leave it alone.
  added?: string
  looked?: string
  busy?: boolean
  // For a show: episodes that took a file put in its folder by hand, and the
  // names (at most ten of notMatchedTotal) of the video files that matched none.
  addedEpisodes?: number
  notMatched?: string[]
  notMatchedTotal?: number
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
  nameTag?: string // tells a show from another of the same name in the results ("US", "UK" or the year)
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
export type MediaServerKind = 'plex' | 'jellyfin' | 'emby' | 'silo' | 'audiobookshelf' | 'kavita' | 'webhook'

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
  // secretError says the saved token can't be read (a secret.key that doesn't belong to this database).
  secretError?: string
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
  id?: number // TMDB person id; opens the person's page
  name: string
  character?: string
  profileUrl?: string
}

// A director, writer or creator of a title.
export interface CrewPerson {
  id?: number
  name: string
  job: 'Director' | 'Writer' | 'Creator'
  profileUrl?: string
}

// One title on a person's page: a Discover card plus what they did on it.
export interface PersonCredit extends DiscoverMovie {
  roles: WorkKind[]
  character?: string
}

export type WorkKind = 'acting' | 'directing' | 'writing' | 'other'

export interface PersonPage {
  id: number
  name: string
  biography?: string
  birthday?: string // YYYY-MM-DD
  deathday?: string
  placeOfBirth?: string
  photoUrl?: string
  imdbId?: string
  knownFor?: string
  workKinds: Exclude<WorkKind, 'other'>[] // the kinds of work they have credits in
  movies: PersonCredit[]
  shows: PersonCredit[]
}

// One season of a show, as the page of a show you do not have yet lists it.
export interface SeasonInfo {
  number: number
  name: string
  episodes: number
  airDate?: string
}

// The cinema and digital dates of a movie ("YYYY-MM-DD") in the person's country.
// cinemaRegion / digitalRegion name the country a date came from when it was not that one.
export interface MovieReleaseDates {
  cinema?: string
  digital?: string
  region?: string
  cinemaRegion?: string
  digitalRegion?: string
}

export interface TitleDetails {
  directors?: string[] // movies
  creators?: string[] // shows
  crew?: CrewPerson[] // directors and writers, or the creators, with ids
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
  folderPath?: string // the full folder of its files, as Mediarium sees it
  fileInfo?: FileInfo // what the file holds, once it has been read
  shortFile?: ShortFileNote // the file is far shorter than the movie should be
  // The series of films this one is part of, when it is.
  collection?: CollectionRef
  releaseDates?: MovieReleaseDates
}

// One subtitle source and where it stands: "on", "off" (set up but switched off),
// "needs_key" (nothing to work with yet) or "limit" (on, but today's downloads are used up).
export type SubtitleSourceId = 'opensubtitles' | 'subdl' | 'addons'
export type SubtitleSourceState = 'on' | 'off' | 'needs_key' | 'limit'
export interface SubtitleSourceStatus {
  id: SubtitleSourceId
  enabled: boolean
  ready: boolean
  state: SubtitleSourceState
  count?: number // how many add-ons are added (the add-ons row)
}

export interface SubtitleQuota {
  hasKey: boolean // OpenSubtitles is switched on and has a key, so its daily limit applies
  hasSource?: boolean // at least one subtitle source is switched on
  sourcesSetUp?: boolean // at least one is set up, switched on or not
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

// What a language wants: its full subtitle ('either' is normal or SDH, the default; 'none' is forced only)
// and whether it also wants a forced track for the foreign-language parts.
export interface SubtitleProfile {
  full: 'either' | 'normal' | 'sdh' | 'none'
  forced: boolean
}

export interface SubtitleSlot {
  language: string
  kind: 'full' | 'forced'
  present: boolean
  embedded?: boolean // it is a track inside the video file
}

export interface SubtitleStatus {
  languages: string[]
  present: string[] // languages whose subtitles are all there
  embedded?: string[] // of those, the ones inside the video file
  slots?: SubtitleSlot[] // each subtitle wanted: one per language and kind
}

export interface SubtitleResult {
  score: number
  fileId: number
  source?: 'subdl' | 'addon' // empty for OpenSubtitles
  sourceName?: string // an add-on's own name
  link?: string // SubDL's download path
  language: string
  release: string
  rating: number
  hashMatch?: boolean // made for this exact video file
  variant?: 'sdh' | 'forced' // for the hard of hearing / only the foreign-language lines; empty is the plain full subtitle
}

// What one lookup at one subtitle site asked and what came back; an empty
// search shows these so it is clear why nothing was found.
export interface SubtitleDiagnostic {
  source: string
  lookup: 'tmdb' | 'imdb' | 'title' | ''
  value?: string
  bare?: boolean // the second try, with fewer filters
  count: number // results kept
  found: number // what the site sent back, before anything was dropped
  note?: 'no-match' | 'wrong-language' | 'wrong-episode' | 'bad-link' | 'error'
  detail?: string
}

export interface SubtitleSearch {
  results: SubtitleResult[]
  diagnostics: SubtitleDiagnostic[]
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
  // secretError says the saved keys can't be read; the config can only be removed.
  secretError?: string
}

export interface VPNStatus {
  // connected is true only while the tunnel is up and the VPN server answers.
  connected: boolean
  state?: 'off' | 'connecting' | 'connected' | 'down'
  // label is the connection that is switched on, even while it isn't working.
  label?: string
  reason?: string
  connectedAt?: string
  // The tunnel exists, whether or not its server answers.
  tunnelUp?: boolean
  // When the VPN server last answered, and how many seconds ago. Both are left out when it never has.
  lastHandshake?: string
  handshakeAgeSec?: number
  // The server has stayed silent past the start-up grace time.
  noHandshake?: boolean
  // What the tunnel carried since it came up.
  rxBytes?: number
  txBytes?: number
}

// The answer to the Test button: a real request through the tunnel.
export interface VPNTestResult {
  ok: boolean
  ip?: string
  reason?: string
  lastHandshake?: string
  handshakeAgeSec?: number
  rxBytes: number
  txBytes: number
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
  // secretError says the saved passwords can't be read; nothing is sent until they are entered again.
  secretError?: string
}

export interface NotifyField {
  name: string
  label: string
  kind: 'text' | 'password' | 'number' | 'select' | 'push'
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
  nameTag?: string // tells it from another candidate of the same name ("US", "UK" or the year)
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
  // duplicate: the same film as another folder of this import, which is the copy used
  outcome: 'added' | 'already' | 'duplicate' | 'failed'
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
  // Set for a library folder that has no .mediarium-root file (see RootMarkerFix).
  marker?: RootMarkerInfo
}

export interface RootMarkerInfo {
  name: string
  seen: boolean // Mediarium saw the folder with its file before, and it was lost
  titles: number // movies and shows with files recorded under the folder
  strong: boolean // titles are recorded but none of their folders is there: the drive is probably not connected
  canCreate: boolean
  why?: string
}

export interface HealthItem {
  id: string
  level: 'error' | 'warn' | 'info'
  title: string
  impact: string
  action?: { label: string; path: string }
  // A button that hides the item for good (the VPN notice), with its label.
  dismiss?: string
}

export interface DiskUsage {
  files: number
  bytes: number
  trashDays?: number
}

export interface DashFolder extends FolderCheck {
  // movies, tv, music, ebooks, audiobooks or downloads; an extra movie or TV
  // folder is movies-extra-1, tv-extra-1 and so on.
  key: string
  label: string
  libraryBytes: number
  items: number
  itemsLabel: string
  hardlinks?: boolean
  off?: boolean // that media type is switched off
  newTitles?: boolean // titles added without a folder choice go here right now
}

// Where a title added without a folder choice goes right now: a path for
// administrators, a label for everyone else. mostRoom: the rule is "the
// folder with the most room", so the folder can change from day to day.
export interface NewTitleFolder {
  folder: string
  mostRoom: boolean
}

// The folders a movie or a show can be kept in, the main one first.
export interface LibraryFolders {
  movies: string[]
  tv: string[]
  newTitles: { movies: NewTitleFolder; tv: NewTitleFolder }
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
  recentInLibrary?: DashRecent[] // with files in the library (downloaded, or some episodes)
  recentToCome?: DashRecent[] // waiting for a release, downloading, or not monitored
  recentDownloads: DashRecent[]
  upcoming: CalendarEntry[]
  health: HealthItem[]
  setup: { indexers: number; usenetServers: number; vpnConnected: boolean; torrentsReady: boolean; mediaServers: number }
}

export interface CalendarEntry {
  kind: 'movie' | 'episode' | 'album' | 'book'
  id: number
  movieId?: number
  tmdbId?: number
  seriesId?: number
  albumId?: number
  artistId?: number
  bookId?: number
  season?: number
  episode?: number
  title: string
  subtitle?: string
  releaseDate: string
  airAt?: string // an episode's exact air time (RFC 3339, UTC) once TVmaze has one; the page shows it in the viewer's time zone
  release?: 'cinema' | 'digital' // a movie with both dates on different days shows twice: which date this is
  status: 'missing' | 'downloading' | 'downloaded'
  posterUrl?: string // the poster or cover, when there is one
  backdropUrl?: string // a wide still, once it has been fetched (the calendar's Backdrops view)
  logoUrl?: string // the title's logo, once it has been fetched
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
  coverChosen?: boolean // a person chose this cover, so "Remove my cover" brings the automatic one back
  description?: string
  addedAt: string
  ebook: BookFormatState
  audiobook: BookFormatState
  seriesName?: string
  seriesPosition?: string
  releaseDate?: string // "2026-11-04" when known; a date ahead means it isn't out yet
  narrators?: string // the audiobook's readers, "Ray Porter, Jane Doe"
  runtimeMin?: number // the audiobook's length
  asin?: string // its Audible id
}

// A book's series (from Open Library, or Hardcover with a token).
export interface BookSeriesEntry {
  key?: string // Open Library work key; missing when only Hardcover knows it
  title: string
  author: string
  authorKey?: string
  year?: number
  position: string
  releaseDate?: string
  hasEbook: boolean
  hasAudio: boolean
  coverUrl?: string
  libraryId?: number
}
export interface BookSeries {
  source: 'openlibrary' | 'hardcover'
  key: string
  name: string
  position?: string // the book the page is about
  followed: boolean
  ebook: boolean
  audiobook: boolean
  entries: BookSeriesEntry[]
}
export interface FollowedSeries {
  source: string
  key: string
  name: string
  ebook: boolean
  audiobook: boolean
  followedAt: string
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
  // What the run in phase is about; results of the other kind are kept from its own last run.
  kinds?: BookFormat[]
  done: number
  total: number
  summary: Record<string, number>
  results: BookImportResult[]
  error?: string
  started?: string
}
// Your e-reader access (profile page): the OPDS catalogue and Kobo sync.
export interface EReaderAccess {
  username: string
  ebooks: boolean // ebooks are switched on
  allowed: boolean // this account may read books on an e-reader
  opds: { on: boolean; path: string; created?: string }
  kobo: { on: boolean; path?: string; created?: string }
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
  chapters?: { title: string; start: number }[] // marks inside the file (an M4B)
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
export type ModulesAnswer = Modules & { subtitlesEnabled?: boolean; trailerBackground?: boolean; trailerSound?: boolean; torrentsOn?: boolean }

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
export interface MusicPreview extends MusicDiscoverItem {
  tracks: { disc: number; position: number; title: string; lengthMs?: number }[]
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
  // What this artist's page lists and following adds (only on GET /music/artists/{id}).
  filter?: MusicFilter & { useLibrary: boolean }
}

// The library-wide choice of what the music pages list, with the defaults, the
// "Official studio releases only" preset and the secondary types that can be hidden.
export interface MusicFilterChoices {
  filter: MusicFilter
  defaults: MusicFilter
  studio: MusicFilter
  secondaryTypes: { key: string; name: string }[]
}

// A release the filter leaves out of an artist's page.
export interface MusicHiddenGroup {
  mbid: string
  title: string
  type: string
  releaseDate: string
  secondaryTypes: string[]
  hiddenBy: string
}

// An album already in the library that the filter leaves out of the list
// (only ever one without files).
export interface MusicHiddenAlbum {
  albumId: number
  mbid: string
  hiddenBy: string
}

export interface MusicDiscography {
  filter: MusicFilter & { useLibrary: boolean }
  hidden: MusicHiddenGroup[]
  // Albums in the library the list leaves out because of the filter.
  library: MusicHiddenAlbum[]
  counts: Record<string, number>
  // Releases still being looked at for the bootleg test (shown until known).
  checking: number
}

// A release list with what the indexers said: how many were asked and
// which did not answer.
export interface SearchAnswer<T> {
  results: T[]
  sources: number
  unanswered: { name: string; message: string }[]
  // For example an indexer that was asked after its daily limit was used up.
  warnings?: string[]
  // Indexers that were not asked because they don't list this kind of release.
  skipped?: string[]
  // Every indexer the search had to do with, and what became of each.
  searched?: Searched[]
  // Releases that came back but were left out because they are not this title
  // (a movie search): how many, and the names of a few.
  ignored?: number
  ignoredSamples?: string[]
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

// ---- Music playlists, lyrics and scrobbling -----------------------------------

export interface MusicPlaylist {
  id: number
  name: string
  shared: boolean
  mine: boolean
  owner: string
  trackCount: number
  durationMs: number
  albumIds: number[] // the first few albums, for a cover mosaic
  updatedAt: string
}

export interface MusicPlaylistTrack {
  entryId: number
  trackId: number
  title: string
  artist: string
  artistId: number
  album: string
  albumId: number
  lengthMs: number
  hasFile: boolean
}

export interface MusicPlaylistDetail extends MusicPlaylist {
  tracks: MusicPlaylistTrack[]
}

// Which tracks to put on a playlist: tracks by id, a whole album, or the top
// tracks of an artist.
export interface PlaylistSelection {
  trackIds?: number[]
  albumId?: number
  artistId?: number
}

export interface TrackLyrics {
  source: '' | 'sidecar' | 'file' | 'lrclib'
  synced: string
  plain: string
  instrumental: boolean
  online: boolean
}

export interface TrackGainInfo {
  trackGain: number | null
  trackPeak: number | null
  albumGain: number | null
  albumPeak: number | null
}

export interface ListenBrainzStatus {
  connected: boolean
  user?: string
  enabled: boolean
  lastError?: string
  submitted: number
  pending: number
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
  note?: string
}

export interface MusicImportJob {
  id: string
  root: string
  phase: 'scanning' | 'matching' | 'done' | 'failed' | 'stopped'
  done: number
  total: number
  current?: string
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
// The anonymous usage numbers (Settings > General; docs/usage-stats.md).
export interface UsageStatsMessage {
  schema: number
  id: string
  version: string
  arch: string
  docker: boolean
  cores: string
  memory: string
  age: string
  library: string
  accounts: string
  indexers: string
  media: string[]
  features: string[]
  servers: string[]
}
export interface UsageStatsState {
  choice: '' | 'yes' | 'no'
  disabled: boolean
  url: string
  lastSent: string
  hasId: boolean
  preview: UsageStatsMessage
}

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

// What's been watched and the cleanup rules (Settings > Media servers).
export interface CleanupRules {
  enabled: boolean
  moviesWatchedDays: number // 0 = rule off
  moviesUnwatchedDays: number
  episodesWatchedDays: number
  keepTags: string[]
}
export interface WatchedStatus {
  lastSync?: string
  lastError?: string
  servers: number
  movies: number
  episodes: number
  lastCleanup?: string
}
export interface WatchedSettings {
  sync: boolean
  status: WatchedStatus
  cleanup: CleanupRules
}
export interface LibraryCleanupItem {
  kind: 'movie' | 'episode'
  id: number
  seriesId?: number
  title: string
  reason: string
}
export interface WatchedTitle {
  plays: number
  lastPlayed?: string
  episodes?: number
}

// Sign-in through a reverse proxy (Settings > Account & Access).
// Two-step sign-in (an authenticator app's code after the password).
export interface TwoStepChallenge {
  twoStep: true
  challenge: string
}
export function isTwoStepChallenge(v: unknown): v is TwoStepChallenge {
  return typeof v === 'object' && v !== null && (v as TwoStepChallenge).twoStep === true
}
// One place the account is signed in (Settings > Security > Signed-in devices).
export interface SessionInfo {
  createdAt: string
  expiresAt: string
  method: 'password' | 'oidc' | 'ldap' | 'passkey' | 'proxy' | string
  current: boolean
}
export interface TwoStepStatus {
  enabled: boolean
  recoveryLeft: number
  suggested?: string // why it is worth switching on, while it is off
}
export interface TwoStepSetup {
  secret: string // in groups of four, for typing in by hand
  uri: string // otpauth:// link
  qr: string[] // QR code rows, "1" dark
}

// What the sign-in page offers (GET /api/auth/methods, no sign-in needed).
export interface SignInMethods {
  local: boolean // the user name and password form
  oidc: { name: string } | null // "Sign in with <name>"
  ldap: boolean // the form also takes directory accounts
  passkeys: boolean
  passkeyHost?: string // passkeys only work on pages opened at this host
  autoSignIn?: 'oidc' // the sign-in page sends visitors to this method by itself
}

// Settings > Account & Access > All accounts: the general sign-in switches.
export interface SignInSettings {
  localSignIn: boolean
  localEffective: boolean
  passkeys: boolean
  passkeysAvailable: boolean
  publicUrl: string
  ssoDisabledByEnv: boolean
  externalOn: boolean
  canTurnOffLocal: boolean
  sessionMethod: string
  autoSignIn: '' | 'oidc' // visitors are sent to this method by themselves
  autoSignInChoices: { id: 'oidc'; name: string }[] // the ones that can be picked now
}

// Single sign-on with an OpenID Connect provider.
export interface OidcSettings {
  enabled: boolean
  name: string
  issuer: string
  clientId: string
  clientSecret?: string // only ever sent, never received
  hasClientSecret: boolean
  scopes: string
  usernameClaim: string
  emailClaim: string
  nameClaim: string
  groupsClaim: string
  autoCreate: boolean
  defaultRole: 'member' | 'viewer'
  adminGroup: string
  viewerGroup: string
  allowedGroup: string
  allowedDomains: string
  linkByEmail: boolean
  redirectUrl: string
  publicUrlSet: boolean
  disabledByEnv: boolean
}
export interface OidcTestResult {
  ok: boolean
  issuer: string
  authorizationEndpoint: string
  tokenEndpoint: string
  userinfoEndpoint: string
  scopesSupported?: string[]
  redirectUrl: string
  notes: string[]
  summary: string
}

// Sign-in with a directory (LDAP or Active Directory).
export interface LdapSettings {
  enabled: boolean
  url: string
  startTls: boolean
  allowPlain: boolean
  caCert: string
  skipVerify: boolean
  bindDn: string
  bindPassword?: string // only ever sent, never received
  hasBindPassword: boolean
  baseDn: string
  userFilter: string
  usernameAttr: string
  nameAttr: string
  emailAttr: string
  idAttr: string
  adminGroupFilter: string
  viewerGroupFilter: string
  groupBaseDn: string
  autoCreate: boolean
  defaultRole: 'member' | 'viewer'
  timeoutSeconds: number
  disabledByEnv: boolean
}
export interface LdapReport {
  found: boolean
  dn?: string
  username?: string
  name?: string
  email?: string
  id?: string
  idIsDn?: boolean // no unchanging id was found, so the DN stands in for it
  admin: boolean
  viewer?: boolean
  passwordChecked: boolean
  passwordOk: boolean
}

// Passkeys on the signed-in account.
export interface Passkey {
  id: number
  name: string
  createdAt: string
  lastUsedAt: string | null
  backedUp: boolean
}
export interface PasskeyList {
  available: boolean
  enabled: boolean // switched on by an administrator
  reason: string // why they can't be used, while they can't
  host: string
  max: number
  keys: Passkey[]
}
export interface PasskeyCeremony {
  ceremony: string
  options: { publicKey: Record<string, unknown> }
}

// Settings > Account & Access > Security: sign out browsers that sit unused.
export interface IdleSignOut {
  days: number // 0 = off
  maxDays: number // the longest limit that can be set
}

export interface ProxySignIn {
  header: string // "" = off
  addresses: string // where the proxy connects from (addresses or CIDR ranges)
  from: string // the address this page came from
  viaTrustedProxy: boolean // this request came from the proxy named in addresses
  seen: string // what the proxy put in the header for this request
}

// Settings > System: the script run after each import.
export interface ScriptRun {
  at: string
  script: string
  event: 'imported' | 'test'
  title?: string
  exitCode: number
  timedOut?: boolean
  seconds: number
  output?: string // the end of what it printed, when it failed
  problem?: string
}

// Cloud copies of the nightly backup. The saved keys, password and passphrase
// never come back; the has* flags say whether they're set.
export interface CloudBackup {
  enabled: boolean
  kind: '' | 's3' | 'webdav'
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  hasSecretKey: boolean
  pathStyle: boolean
  url: string
  username: string
  hasPassword: boolean
  hasPassphrase: boolean
  keep: number
  last: { at?: string; name?: string; size?: number; problem?: string }
}

// What the page sends. Empty secretKey, password or passphrase keep the saved ones.
export interface CloudBackupForm {
  enabled: boolean
  kind: 's3' | 'webdav'
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretKey: string
  pathStyle: boolean
  url: string
  username: string
  password: string
  passphrase: string
  keep: number
}

export interface CloudCopy {
  name: string
  size: number
  modified: string
}

export interface ScriptsState {
  folder: string
  scripts: string[] // executable files in the folder
  script: string // "" = off
  timeoutSec: number
  lastRun?: ScriptRun
}

export interface SystemOptions {
  updateCheck: boolean
  autoInstall: boolean
  allowPush: boolean
  autoRestartWhenStuck: boolean
}

export interface McpSettings {
  enabled: boolean
  path: string
}

// An app connected to MCP with OAuth (Settings > Other apps).
export interface McpApp {
  id: number
  clientName: string
  account: string
  scope: string
  createdAt: string
  lastUsedAt: string
}

// A client an administrator registered by hand, for apps that ask for a client ID and secret.
export interface McpClient {
  clientId: string
  name: string
  redirectUris: string[]
  createdAt: string
}

export interface McpClientMade extends McpClient {
  clientSecret: string
}

// What the consent page shows about an app that wants to connect.
export interface OAuthRequest {
  clientName: string
  redirectHost: string
  scopes: string[]
  account: string
  canApprove: boolean
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

// A subtitle add-on: a site reached through a small web service. The host is
// shown; the rest of its address can hold a key and stays on the server.
export type SubtitleAddon = { id: string; name: string; host: string }

export type MonitorMode = 'all' | 'future' | 'none'
export interface MonitorChange {
  stopped: number
  started: number
}

export const api = {
  version: () => get<{ version: string }>('/version'),
  onboardingStatus: () => get<OnboardingStatus>('/onboarding/status'),
  createAdmin: (data: { username: string; password: string; name?: string; email?: string; setupCode?: string }) => post<User>('/onboarding/admin', data),
  updateAccount: (data: { username: string; name: string; email: string }) => put<User>('/auth/profile', data),

  login: (username: string, password: string) => post<User | TwoStepChallenge>('/auth/login', { username, password }),
  loginCode: (challenge: string, answer: { code?: string; recoveryCode?: string }) => post<User & { recoveryLeft?: number }>('/auth/login/code', { challenge, ...answer }),
  twoStep: () => get<TwoStepStatus>('/auth/two-step'),
  twoStepSetup: () => post<TwoStepSetup>('/auth/two-step/setup', {}),
  twoStepEnable: (code: string) => post<{ recoveryCodes: string[] }>('/auth/two-step/enable', { code }),
  twoStepDisable: (password: string) => post<null>('/auth/two-step/disable', { password }),
  twoStepRecovery: (password: string) => post<{ recoveryCodes: string[] }>('/auth/two-step/recovery', { password }),
  userTwoStepOff: (id: number) => del<null>(`/users/${id}/two-step`),
  logout: () => post<null>('/auth/logout'),
  sessions: () => get<SessionInfo[]>('/auth/sessions'),
  endOtherSessions: () => del<{ ended: number }>('/auth/sessions'),
  me: () => get<User>('/auth/me'),
  changePassword: (currentPassword: string, newPassword: string) =>
    post<null>('/auth/change-password', { currentPassword, newPassword }),
  listAPIKeys: () => get<APIKey[]>('/auth/api-keys'),
  eReader: () => get<EReaderAccess>('/auth/ereader'),
  newEReaderPassword: () => post<EReaderAccess & { password: string }>('/auth/ereader/opds'),
  removeEReaderPassword: () => del<EReaderAccess>('/auth/ereader/opds'),
  newKoboAddress: () => post<EReaderAccess>('/auth/ereader/kobo'),
  removeKoboAddress: () => del<EReaderAccess>('/auth/ereader/kobo'),
  koboSyncAgain: () => post<EReaderAccess>('/auth/ereader/kobo/resync'),
  createAPIKey: (name: string, readOnly = false) => post<APIKey>('/auth/api-keys', { name, readOnly }),
  signInMethods: () => get<SignInMethods>('/auth/methods'),
  getSignIn: () => get<SignInSettings>('/auth/sign-in'),
  putSignIn: (data: { localSignIn?: boolean; passkeys?: boolean; autoSignIn?: '' | 'oidc' }) => put<SignInSettings>('/auth/sign-in', data),
  getOidc: () => get<OidcSettings>('/auth/oidc'),
  putOidc: (data: Partial<OidcSettings>) => put<OidcSettings>('/auth/oidc', data),
  testOidc: (data: Partial<OidcSettings>) => post<OidcTestResult>('/auth/oidc/test', data),
  linkOidc: (password: string) => post<{ url: string }>('/auth/oidc/link', { password }),
  unlinkOidc: () => del<null>('/auth/oidc/link'),
  getLdap: () => get<LdapSettings>('/auth/ldap'),
  putLdap: (data: Partial<LdapSettings>) => put<LdapSettings>('/auth/ldap', data),
  testLdap: (data: Partial<LdapSettings> & { testUsername: string; testPassword?: string }) => post<LdapReport>('/auth/ldap/test', data),
  passkeys: () => get<PasskeyList>('/auth/passkeys'),
  passkeyRegisterBegin: (password: string) => post<PasskeyCeremony>('/auth/passkeys/register/begin', { password }),
  passkeyRegisterFinish: (ceremony: string, name: string, answer: unknown) =>
    post<Passkey>(`/auth/passkeys/register/finish?c=${encodeURIComponent(ceremony)}&name=${encodeURIComponent(name)}`, answer),
  renamePasskey: (id: number, name: string) => put<null>(`/auth/passkeys/${id}`, { name }),
  deletePasskey: (id: number) => del<null>(`/auth/passkeys/${id}`),
  passkeyLoginBegin: () => post<PasskeyCeremony>('/auth/passkey/login/begin', {}),
  passkeyLoginFinish: (ceremony: string, answer: unknown) => post<User>(`/auth/passkey/login/finish?c=${encodeURIComponent(ceremony)}`, answer),
  revokeAPIKey: (id: number) => del<null>(`/auth/api-keys/${id}`),
  deleteAPIKey: (id: number) => del<null>(`/auth/api-keys/${id}?remove=true`),

  // Accounts (administrators only).
  listAccounts: () => get<Account[]>('/users'),
  createAccount: (data: NewAccount) => post<Account>('/users', data),
  listRequests: () => get<{ requests: TitleRequest[]; pending: number }>('/requests'),
  subtitleTiming: (body: { kind: 'movie' | 'series'; id: number; file: string; shiftMs?: number; reference?: string; undo?: boolean }) => post<{ message: string }>('/subtitles/timing', body),
  approveRequest: (id: number) => post<TitleRequest>(`/requests/${id}/approve`, {}),
  declineRequest: (id: number, note: string) => post<TitleRequest>(`/requests/${id}/decline`, { note }),
  deleteRequest: (id: number) => del<null>(`/requests/${id}`),
  requestStatus: () => get<RequestStatus>('/requests/status'),
  requestSettings: () => get<RequestSettings>('/requests/settings'),
  putRequestSettings: (body: Partial<RequestOptions> & { quotas?: Record<string, number> }) => put<RequestSettings>('/requests/settings', body),
  listIssues: () => get<{ issues: Issue[]; open: number; enabled: boolean }>('/issues'),
  createIssue: (body: { kind: 'movie' | 'tv'; itemId: number; category: IssueCategory; episode?: string; message?: string }) => post<Issue>('/issues', body),
  resolveIssue: (id: number, comment: string) => post<Issue>(`/issues/${id}/resolve`, { comment }),
  reopenIssue: (id: number) => post<Issue>(`/issues/${id}/reopen`, {}),
  deleteIssue: (id: number) => del<null>(`/issues/${id}`),
  requestNotices: () => get<RequestNotices>('/auth/request-notices'),
  putRequestNotices: (enabled: boolean) => put<RequestNotices>('/auth/request-notices', { enabled }),
  addNoticeDevice: (subscription: string, name: string) => post<RequestNotices>('/auth/request-notices/devices', { subscription, name }),
  removeNoticeDevice: (id: number) => del<RequestNotices>(`/auth/request-notices/devices/${id}`),
  importLists: () => get<ImportListsAnswer>('/import-lists'),
  createImportList: (body: ImportListInput) => post<ImportList>('/import-lists', body),
  updateImportList: (id: number, body: ImportListInput) => put<ImportList>(`/import-lists/${id}`, body),
  setImportListEnabled: (id: number, enabled: boolean) => put<ImportList>(`/import-lists/${id}/enabled`, { enabled }),
  deleteImportList: (id: number) => del<null>(`/import-lists/${id}`),
  runImportList: (id: number) => post<ImportList>(`/import-lists/${id}/run`, {}),
  forgetImportList: (id: number) => post<ImportList>(`/import-lists/${id}/forget`, {}),
  previewImportList: (body: ImportListInput & { id?: number }) => post<ImportListPreview>('/import-lists/preview', body),
  runPlexWatchlistFor: (userId: number) => post<{ added: number; requested: number }>(`/import-lists/plex/${userId}/run`, {}),
  updateAccountById: (id: number, data: AccountChanges) => put<Account>(`/users/${id}`, data),
  deleteAccount: (id: number) => del<null>(`/users/${id}`),

  getSettings: () => get<Settings>('/settings'),
  putSettings: (partial: Partial<Settings> & { legalAcknowledged?: boolean; tmdbApiKey?: string; openSubtitlesApiKey?: string; openSubtitlesUsername?: string; openSubtitlesPassword?: string }) => put<Settings>('/settings', partial),

  listIndexers: () => get<IndexerConfig[]>('/indexers'),
  addIndexerDefinition: (content: string) => post<IndexerDefinition>('/indexer-definitions/custom', { content }),
  removeIndexerDefinition: (id: string) => del<void>(`/indexer-definitions/custom/${encodeURIComponent(id)}`),
  indexerDefinitions: (refresh = false) =>
    get<{ updatedAt?: string; source?: string; licence?: string; definitions: IndexerDefinition[] }>(`/indexer-definitions${refresh ? '?refresh=1' : ''}`),
  createIndexer: (data: { name: string; definitionId: string; baseUrl: string; apiKey: string; protocol?: 'usenet' | 'torrent'; settings?: Record<string, string> }) =>
    post<IndexerConfig>('/indexers', data),
  deleteIndexer: (id: number) => del<null>(`/indexers/${id}`),
  testIndexerConfig: (data: { name: string; baseUrl: string; apiKey: string }) =>
    post<{ ok: boolean; message: string; detail?: string }>('/indexers/test', data),
  testIndexer: (id: number) => post<{ ok: boolean; message: string; detail?: string }>(`/indexers/${id}/test`),
  updateIndexer: (id: number, data: { name?: string; baseUrl?: string; apiKey?: string; enabled?: boolean; requestsPerDay?: number; minSeeders?: number; extraCategories?: Record<string, number[]> }) => put<IndexerConfig>(`/indexers/${id}`, data),
  indexerCategories: (id: number) => get<{ categories: { id: number; name: string }[] }>(`/indexers/${id}/categories`),
  setIndexerEnabled: (id: number, enabled: boolean) => put<null>(`/indexers/${id}/enabled`, { enabled }),

  listUsenetServers: () => get<UsenetServer[]>('/usenet-servers'),
  createUsenetServer: (data: UsenetServerInput) => post<UsenetServer>('/usenet-servers', data),
  updateUsenetServer: (id: number, data: UsenetServerInput) => put<UsenetServer>(`/usenet-servers/${id}`, data),
  deleteUsenetServer: (id: number) => del<null>(`/usenet-servers/${id}`),
  testUsenetServerConfig: (data: { id?: number; host: string; port: number; useSsl: boolean; username?: string; password?: string }) =>
    post<{ ok: boolean; message: string }>('/usenet-servers/test', data),
  testUsenetServer: (id: number) => post<{ ok: boolean; message: string }>(`/usenet-servers/${id}/test`),
  probeUsenetServer: (id: number) => post<{ accepted: number; requested: number; refused: boolean; message: string }>(`/usenet-servers/${id}/probe`),
  downloadsStatus: () => get<DownloadsStatus>('/downloads/status'),
  torrentOptions: () => get<TorrentOptions>('/downloads/torrent-options'),
  setTorrentOptions: (patch: Partial<Omit<TorrentOptions, 'portMapping' | 'uploadLimitKB'>> & { portMapping?: boolean; uploadLimitKB?: number }) => put<TorrentOptions>('/downloads/torrent-options', patch),
  torrentDetail: (id: number) => get<TorrentDetail>(`/queue/${id}/torrent`),
  setTorrentDetail: (id: number, patch: { sequential?: TorrentDetail['sequential']; downLimitKB?: number; upLimitKB?: number }) => put<TorrentDetail>(`/queue/${id}/torrent`, patch),
  setTorrentFiles: (id: number, skip: string[]) => put<TorrentDetail>(`/queue/${id}/torrent/files`, { skip }),
  seedRules: () => get<Record<string, SeedRule>>('/indexers/seed-rules'),
  setSeedRule: (indexerId: number, rule: SeedRule) => put<SeedRule>(`/indexers/${indexerId}/seed-rules`, rule),

  search: (q: string) => get<SearchResult[]>(`/search?q=${encodeURIComponent(q)}`),

  // The big lists that are read again and again send the tag of their last answer, and the server answers 304 when nothing changed (see revalidate).
  listMovies: () => request<Movie[]>('/movies?brief=1', { revalidate: true }),
  getMovie: (id: number) => get<Movie>(`/movies/${id}`),
  addMovie: (tmdbId: number, options: AddMovieOptions = {}) => post<Movie | Requested>('/movies', { tmdbId, ...options }),
  discoverSearch: (q: string, signal?: AbortSignal) => request<TitleResult[]>(`/discover/search?q=${encodeURIComponent(q)}`, { signal }),
  setMovieSources: (id: number, sources: SourcePref) => put<null>(`/movies/${id}/sources`, { sources }),
  setSeriesSources: (id: number, sources: SourcePref) => put<null>(`/series/${id}/sources`, { sources }),
  setSeriesSeasonPack: (id: number, seasonPack: SeasonPackChoice) => put<null>(`/series/${id}/season-pack`, { seasonPack }),
  // What "delete everything on disk" would remove for a title, before it is asked.
  movieDiskUsage: (id: number) => get<DiskUsage>(`/movies/${id}/disk-usage`),
  seriesDiskUsage: (id: number) => get<DiskUsage>(`/series/${id}/disk-usage`),
  artistDiskUsage: (id: number) => get<DiskUsage>(`/music/artists/${id}/disk-usage`),
  deleteMovie: (id: number, deleteFiles: boolean) => del<null>(`/movies/${id}?deleteFiles=${deleteFiles}`),
  grab: (movieId: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent'; replaceQueueId?: number; replace?: boolean }) =>
    post<{ queueId: number }>(`/movies/${movieId}/grab`, data),
  searchGrab: (data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ movieId: number; queueId: number }>('/search/grab', data),
  similarMovies: (movieId: number) => get<DiscoverMovie[]>(`/movies/${movieId}/similar`),
  tmdbMovieDetail: (tmdbId: number) => get<MovieDetail>(`/tmdb/movies/${tmdbId}`),
  tmdbTVDetail: (tmdbId: number) => get<TVDetail>(`/tmdb/tv/${tmdbId}`),
  tmdbPerson: (id: number) => get<PersonPage>(`/tmdb/people/${id}`),
  tmdbSimilarMovies: (tmdbId: number) => get<DiscoverMovie[]>(`/tmdb/movies/${tmdbId}/similar`),

  movieSubtitleStatus: (id: number) => get<SubtitleStatus>(`/movies/${id}/subtitles/status`),
  episodeSubtitleStatus: (id: number) => get<SubtitleStatus>(`/episodes/${id}/subtitles/status`),
  searchEpisodeSubtitles: (id: number, lang: string, variant = '') =>
    get<SubtitleSearch>(`/episodes/${id}/subtitles?lang=${encodeURIComponent(lang)}${variant ? `&variant=${variant}` : ''}`),
  downloadEpisodeSubtitle: (id: number, pick: Pick<SubtitleResult, 'fileId' | 'source' | 'link' | 'variant'>, language: string) =>
    post<{ path: string }>(`/episodes/${id}/subtitles/download`, { fileId: pick.fileId, source: pick.source ?? '', link: pick.link ?? '', language, variant: pick.variant ?? '' }),
  subtitlesWanted: (includeDismissed = false) => get<SubtitleWanted[]>(`/subtitles/wanted${includeDismissed ? '?includeDismissed=1' : ''}`),
  subtitleQuota: () => get<SubtitleQuota>('/subtitles/quota'),
  subtitlesGet: (data: { items?: { kind: string; id: number }[]; all?: boolean }) =>
    post<{ downloaded: number; stopped: boolean; skipped: number; message: string; quota: SubtitleQuota }>('/subtitles/get', data),
  dismissSubtitles: (items: { kind: string; id: number }[]) => post<null>('/subtitles/dismiss', { items }),
  undismissSubtitles: (items: { kind: string; id: number }[]) => request<null>('/subtitles/dismiss', { method: 'DELETE', body: JSON.stringify({ items }) }),
  subtitleSweep: () => post<{ downloaded: number; message: string }>('/subtitles/sweep'),
  searchSubtitles: (movieId: number, lang = 'en', variant = '') => get<SubtitleSearch>(`/movies/${movieId}/subtitles?lang=${lang}${variant ? `&variant=${variant}` : ''}`),
  downloadSubtitle: (movieId: number, pick: Pick<SubtitleResult, 'fileId' | 'source' | 'link' | 'variant'>, language = 'en') =>
    post<{ path: string }>(`/movies/${movieId}/subtitles/download`, { fileId: pick.fileId, source: pick.source ?? '', link: pick.link ?? '', language, variant: pick.variant ?? '' }),

  listProfiles: () => get<{ profiles: QualityProfile[]; tiers: string[]; defaultId: number; defaultTvId: number; defaultSources?: SourcePref }>('/quality-profiles'),
  createProfile: (data: QualityProfileInput) =>
    post<QualityProfile>('/quality-profiles', data),
  updateProfile: (id: number, data: QualityProfileInput) =>
    put<QualityProfile>(`/quality-profiles/${id}`, data),
  deleteProfile: (id: number) => del<null>(`/quality-profiles/${id}`),
  listCustomFormats: () => get<{ formats: CustomFormat[] }>('/custom-formats'),
  importCustomFormats: (json: string, app: '' | 'radarr' | 'sonarr') => post<FormatImportResult>('/custom-formats', { json, app }),
  renameCustomFormat: (id: number, name: string) => put<CustomFormat>(`/custom-formats/${id}`, { name }),
  deleteCustomFormat: (id: number) => del<null>(`/custom-formats/${id}`),
  testCustomFormats: (title: string, profileId?: number) => post<FormatTestResult>('/custom-formats/test', { title, profileId: profileId ?? 0 }),
  assignCustomFormats: (formatIds: number[], profileIds: number[], mode: { trash: true } | { score: number }, replaceScores = false) =>
    post<FormatAssignResult>('/custom-formats/assign', { formatIds, profileIds, mode: 'trash' in mode ? 'trash' : 'score', score: 'score' in mode ? mode.score : 0, replaceScores }),
  trashDefaultScores: (formatIds: number[]) => post<{ scores: FormatScore[] }>('/custom-formats/default-scores', { formatIds }),
  trashUpdatePreview: () => get<TrashUpdateResult>('/custom-formats/trash-update'),
  trashUpdate: () => post<TrashUpdateResult>('/custom-formats/trash-update', {}),
  trashUpdateStatus: () => get<TrashUpdateStatus>('/custom-formats/trash-update/status'),
  trashGuides: (app: 'radarr' | 'sonarr') => get<{ formats: TrashFormat[]; profiles: TrashProfile[] }>(`/trash-guides/${app}`),
  importTrashFormats: (app: 'radarr' | 'sonarr', trashIds: string[], profileId?: number, replaceScores = false) =>
    post<FormatImportResult>(`/trash-guides/${app}/formats`, { trashIds, profileId: profileId ?? 0, replaceScores }),
  importTrashProfile: (app: 'radarr' | 'sonarr', trashId: string) =>
    post<{ profile: QualityProfile; formats: FormatImportResult }>(`/trash-guides/${app}/profiles`, { trashId }),
  // searching: the file is below the new profile, so a better release is being looked for now
  setMovieProfile: (id: number, profileId: number) => put<{ searching?: boolean } | null>(`/movies/${id}/profile`, { profileId }),
  setSeriesProfile: (id: number, profileId: number) => put<{ searching?: boolean } | null>(`/series/${id}/profile`, { profileId }),

  listQueue: () => request<QueueItem[]>('/queue', { revalidate: true }),
  // Only what is waiting, downloading or importing: the count in the menu and the dashboard need no more.
  listActiveQueue: () => request<QueueItem[]>('/queue?active=1', { revalidate: true }),
  // How many downloads are waiting, downloading or importing: the number beside Activity, without the whole queue.
  queueCount: () => get<{ active: number; queued: number; downloading: number; importing: number }>('/queue/count'),
  // Which TMDB titles are in the library and how each is doing (Discover's marks), far smaller than the movie
  // list, the show list and the queue together. Answers 304 (the very same object) when nothing changed.
  libraryOwned: () => request<OwnedAnswer>('/library/owned', { revalidate: true }),
  retryQueueItem: (id: number) => post<{ queueId: number }>(`/queue/${id}/retry`),
  clientTorrents: (scope: 'mine' | 'all') => get<ClientTorrentsAnswer>(`/download-client/torrents?scope=${scope}`),
  usenetClientJobs: (scope: 'mine' | 'all') => get<UsenetClientJobsAnswer>(`/usenet-client/jobs?scope=${scope}`),
  usenetClientJobAction: (id: string, action: 'pause' | 'resume' | 'remove' | 'remove-files') => post<{ done: string }>(`/usenet-client/jobs/${encodeURIComponent(id)}/${action}`),
  clientTorrentAction: (hash: string, action: 'pause' | 'resume' | 'recheck' | 'remove' | 'remove-files') => post<{ done: string }>(`/download-client/torrents/${encodeURIComponent(hash)}/${action}`),
  debridTorrents: () => get<DebridTorrentsAnswer>('/debrid/torrents'),
  debridRemove: (id: string) => post<{ done: string }>(`/debrid/torrents/${encodeURIComponent(id)}/remove`),
  blocklistQueueItem: (id: number) => post<null>(`/queue/${id}/blocklist`),
  deleteQueueItem: (id: number, deleteFiles = false) => del<null>(`/queue/${id}${deleteFiles ? '?deleteFiles=1' : ''}`),
  pauseQueueItem: (id: number) => post<{ status: string; pending?: string }>(`/queue/${id}/pause`),
  resumeQueueItem: (id: number) => post<{ status: string }>(`/queue/${id}/resume`),
  stopQueueItem: (id: number, deleteFiles: boolean) => post<{ status: string; pending?: string }>(`/queue/${id}/stop`, { deleteFiles }),
  frontQueueItem: (id: number) => post<{ status: string }>(`/queue/${id}/front`),
  bulkQueue: (ids: number[], action: 'pause' | 'resume' | 'stop' | 'retry' | 'front' | 'remove' | 'blocklist', deleteFiles = false) =>
    post<{ done: number; failed: { id: number; error: string }[] }>('/queue/bulk', { ids, action, deleteFiles }),
  pauseAllQueue: (protocol?: 'usenet' | 'torrent') => post<{ paused: number }>(`/queue/pause-all${protocol ? `?protocol=${protocol}` : ''}`),
  resumeAllQueue: (protocol?: 'usenet' | 'torrent') => post<{ resumed: number; skipped: number; problem?: string }>(`/queue/resume-all${protocol ? `?protocol=${protocol}` : ''}`),
  clearFinishedQueue: () => del<{ removed: number }>('/queue'),
  resolveConflict: (id: number, overwrite: boolean) => post<null>(`/queue/${id}/resolve-conflict`, { overwrite }),
  getWanted: (kind: 'missing' | 'cutoff') => request<WantedItem[]>(`/wanted?kind=${kind}`, { revalidate: true }),
  // One page of the list, one row per movie and per show; a show's episodes come from getWantedShow.
  getWantedPage: (kind: 'missing' | 'cutoff', query: WantedPageQuery = {}) => request<WantedPage>(wantedPagePath(kind, query), { revalidate: true }),
  getWantedShow: (kind: 'missing' | 'cutoff', seriesId: number) => request<WantedItem[]>(`/wanted?kind=${kind}&series=${seriesId}`, { revalidate: true }),
  libraryCounts: () => get<{ movies: number; shows: number }>('/library/counts'),
  // How many of the library's files have had their video and audio tracks read.
  fileDetailsStatus: () => get<{ done: number; total: number }>('/library/file-details'),
  searchNowMovie: (id: number) => post<{ grabbed: number; message: string }>(`/movies/${id}/search-now`),
  replaceMovieSmaller: (id: number) => post<{ grabbed: number; message: string }>(`/movies/${id}/replace-smaller`),
  searchNowSeries: (id: number, scope: { season?: number; episode?: number } = {}) =>
    post<{ grabbed: number; message: string }>(`/series/${id}/search-now`, scope),
  movieSearch: (id: number, q?: string) => get<SearchAnswer<SearchResult>>(`/movies/${id}/search?detail=1${q ? `&q=${encodeURIComponent(q)}` : ''}`),
  libraryFolders: () => get<LibraryFolders>('/library/folders'),
  setMovieFolder: (id: number, rootPath: string) => put<{ rootPath: string }>(`/movies/${id}/folder`, { rootPath }),
  setSeriesFolder: (id: number, rootPath: string) => put<{ rootPath: string }>(`/series/${id}/folder`, { rootPath }),
  setMovieMonitored: (id: number, monitored: boolean) => put<null>(`/movies/${id}/monitored`, { monitored }),
  setSeriesMonitored: (id: number, monitored: boolean) => put<null>(`/series/${id}/monitored`, { monitored }),
  // Which episodes of a show are monitored: all, only the ones that have not aired yet, or none. dryRun only counts.
  setSeriesMonitorMode: (id: number, mode: MonitorMode, dryRun = false) => post<MonitorChange>(`/series/${id}/monitor-mode`, { mode, dryRun }),
  setSeriesType: (id: number, type: SeriesType) => put<{ type: SeriesType }>(`/series/${id}/type`, { type }),
  seriesNumbering: (id: number) => get<SeriesNumbering>(`/series/${id}/numbering`),
  refreshSeriesNumbering: (id: number) => post<SeriesNumbering>(`/series/${id}/numbering/refresh`),
  customNumbering: (id: number) => get<CustomNumberingBody>(`/series/${id}/numbering/custom`),
  saveCustomNumbering: (id: number, body: CustomNumberingBody) => put<CustomNumberingBody>(`/series/${id}/numbering/custom`, body),
  setSeasonMonitored: (id: number, season: number, monitored: boolean) =>
    put<null>(`/series/${id}/seasons/${season}/monitored`, { monitored }),
  setEpisodeMonitored: (id: number, monitored: boolean) => put<null>(`/episodes/${id}/monitored`, { monitored }),
  // One page of the blocklist, newest first, with how many there are in all.
  listBlocklist: (limit = 100, offset = 0) => get<{ items: BlocklistEntry[]; total: number }>(`/blocklist?limit=${limit}&offset=${offset}`),
  removeBlocklistEntry: (id: number) => del<null>(`/blocklist/${id}`),
  clearBlocklist: () => del<null>('/blocklist'),
  savedBackups: () => get<SavedBackups>('/system/backups'),
  saveBackupNow: () => post<{ name: string; size: number }>('/system/backups'),
  cloudBackup: () => get<CloudBackup>('/system/backups/cloud'),
  putCloudBackup: (body: CloudBackupForm) => put<CloudBackup>('/system/backups/cloud', body),
  testCloudBackup: (body: CloudBackupForm) => post<{ ok: boolean }>('/system/backups/cloud/test', body),
  uploadCloudBackup: () => post<{ at: string; name: string; size: number }>('/system/backups/cloud/upload'),
  cloudBackupList: () => get<CloudCopy[]>('/system/backups/cloud/list'),
  setIndexerPriority: (id: number, priority: number) => put<null>(`/indexers/${id}/priority`, { priority }),
  resumeIndexer: (id: number) => post<null>(`/indexers/${id}/resume`),
  indexerStats: () => get<{ days: number; indexers: IndexerStat[] }>('/stats/indexers'),
  delayProfile: () => get<DelayProfile>('/search/delay'),
  putDelayProfile: (p: DelayProfile) => put<DelayProfile>('/search/delay', p),
  searchWaiting: () => get<WaitingRelease[]>('/search/waiting'),
  // Movies and TV each have a size table; kind picks one.
  sizeLimits: (kind: 'movie' | 'tv' = 'movie') => get<SizeTable>(`/quality/size-limits?kind=${kind}`),
  putSizeLimits: (table: SizeTable, kind: 'movie' | 'tv' = 'movie') => put<SizeTable>(`/quality/size-limits?kind=${kind}`, table),
  resetSizeLimits: (kind: 'movie' | 'tv' = 'movie') => post<SizeTable>(`/quality/size-limits/reset?kind=${kind}`),
  minSizeCheck: () => get<{ enabled: boolean }>('/quality/min-size-check'),
  putMinSizeCheck: (enabled: boolean) => put<{ enabled: boolean }>('/quality/min-size-check', { enabled }),
  trashSizes: (list: string) => get<TrashSizes>(`/quality/size-limits/trash/${encodeURIComponent(list)}`),
  calendarFeed: () => get<{ on: boolean; path?: string }>('/calendar/feed'),
  newCalendarFeed: () => post<{ on: boolean; path?: string }>('/calendar/feed'),
  removeCalendarFeed: () => del<{ on: boolean; path?: string }>('/calendar/feed'),
  // Adds an NZB, a torrent file, an address or a magnet link to the download line for a movie or an episode.
  // The form carries movieId, or seriesId with season and episode (episode 0 = the whole season), and one of
  // file or url; name is optional. The empty headers let the browser set the multipart boundary.
  manualDownload: (form: FormData) => request<ManualDownloadResult>('/downloads/manual', { method: 'POST', body: form, headers: {} }),
  manualImportList: (folder = '', incomplete = false) =>
    get<ManualList>(`/manual-import?folder=${encodeURIComponent(folder)}${incomplete ? '&incomplete=1' : ''}`),
  manualImportFolders: (path = '') => get<ManualFolders>(`/manual-import/folders?path=${encodeURIComponent(path)}`),
  manualImport: (body: { folder?: string; path: string; mode?: ManualMode; incomplete?: boolean; movieId?: number; seriesId?: number; season?: number; episode?: number }) =>
    post<{ path: string; sourceKept?: boolean }>('/manual-import', body),
  listExclusions: () => get<{ kind: 'movie' | 'tv'; tmdbId: number; title: string; year?: number }[]>('/exclusions'),
  addExclusion: (e: { kind: 'movie' | 'tv'; tmdbId: number; title: string; year?: number }) => post<null>('/exclusions', e),
  removeExclusion: (kind: string, tmdbId: number) => del<null>(`/exclusions/${kind}/${tmdbId}`),
  libraryStats: () => get<LibraryStats>('/stats/library'),
  resetStats: () => post<{ reset: boolean }>('/stats/reset'),
  renamePreview: () => get<RenameItem[]>('/rename'),
  rename: (items: { kind: string; id: number }[]) => post<{ renamed: number; failed: string[] }>('/rename', { items }),
  listTrash: () => get<TrashList>('/trash'),
  restoreTrash: (kind: string, id: string) => post<{ label: string; folder: string }>(`/trash/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/restore`),
  deleteTrash: (kind: string, id: string) => del<null>(`/trash/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`),
  emptyTrash: () => del<{ removed: number; freed: number }>('/trash'),
  listActivity: (opts: { limit?: number; q?: string } = {}) => request<ActivityEntry[]>(`/activity?limit=${opts.limit ?? 100}${opts.q ? `&q=${encodeURIComponent(opts.q)}` : ''}`, { revalidate: true }),

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
  listBooks: () => request<Book[]>('/books?brief=1', { revalidate: true }),
  getBook: (id: number) => get<Book>(`/books/${id}`),
  // With an author, the books by that author come first.
  searchBooks: (q: string, author = '') =>
    get<BookFound[]>(`/books/search?q=${encodeURIComponent(q)}${author ? `&author=${encodeURIComponent(author)}` : ''}`),
  bookDiscover: (p: { list: 'trending' | 'browse'; period?: string; subject?: string; from?: string; to?: string; sort?: string; page?: number }) => {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries(p)) if (v !== undefined && v !== '') q.set(k, String(v))
    return get<BookFound[]>(`/books/discover?${q}`)
  },
  bookLinks: (id: number) => get<{ serverId: number; name: string; kind: MediaServerKind; url: string }[]>(`/books/${id}/links`),
  bookTracks: (id: number) => get<{ tracks: BookTrack[]; format: string }>(`/books/${id}/tracks`),
  bookTrackUrl: (id: number, n: number) => withBase(`/api/books/${id}/listen/${n}`),
  bookFileUrl: (id: number, download = false) => withBase(`/api/books/${id}/read${download ? '?download=1' : ''}`),
  bookProgress: (id: number, format: BookFormat) => get<BookProgress>(`/books/${id}/progress?format=${format}`),
  listBookProgress: () => get<BookProgress[]>('/books/progress'),
  saveBookProgress: (id: number, p: { format: BookFormat; position: string; percent: number; finished?: boolean }, keepalive = false) =>
    fetch(withBase(`/api/books/${id}/progress`), { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p), keepalive, credentials: 'same-origin' }).then(() => undefined),
  authorWorks: (key: string, sort: 'popular' | 'newest' = 'popular') => get<BookFound[]>(`/book-authors/${encodeURIComponent(key)}/works?sort=${sort}`),
  followedAuthors: () => get<{ key: string; name: string; ebook: boolean; audiobook: boolean; followedAt: string }[]>('/book-authors'),
  followAuthor: (key: string, data: { name: string; ebook: boolean; audiobook: boolean }) => put<null>(`/book-authors/${encodeURIComponent(key)}/follow`, data),
  unfollowAuthor: (key: string) => del<null>(`/book-authors/${encodeURIComponent(key)}/follow`),
  bookSeries: (key: string, title = '', author = '') =>
    get<{ series: BookSeries | null }>(`/book-works/${encodeURIComponent(key)}/series?${new URLSearchParams({ title, author })}`),
  followedSeries: () => get<FollowedSeries[]>('/book-series'),
  followSeries: (source: string, key: string, body: { name: string; ebook: boolean; audiobook: boolean }) =>
    put<{ added: number; message?: string }>(`/book-series/${source}/${encodeURIComponent(key)}/follow`, body),
  unfollowSeries: (source: string, key: string) => del<null>(`/book-series/${source}/${encodeURIComponent(key)}/follow`),
  simklStatus: () => get<SimklStatus>('/settings/simkl'),
  simklStartPin: () => post<{ code: string; url: string; fullUrl?: string; flow: 'device' | 'pin'; expiresIn: number; interval: number }>('/settings/simkl/pin', {}),
  simklPinStatus: (code: string) => get<{ done: boolean; status?: SimklStatus; name?: string; interval?: number }>(`/settings/simkl/pin/${encodeURIComponent(code)}`),
  simklDisconnect: () => del<null>('/settings/simkl'),
  simklTest: () => post<{ ok: boolean; message: string }>('/settings/simkl/test', {}),
  getHardcover: () => get<{ set: boolean; username?: string }>('/settings/hardcover'),
  putHardcover: (token: string) => put<{ set: boolean; username?: string }>('/settings/hardcover', { token }),
  // tracks=false answers with the album alone (fast); the tracklist is a second request.
  musicPreview: (mbid: string, tracks = true) => get<MusicPreview>(`/music/preview/${encodeURIComponent(mbid)}${tracks ? '' : '?tracks=0'}`),
  bookWork: (key: string) => get<BookWork>(`/book-works/${encodeURIComponent(key)}`),
  bookImportStatus: () => get<BookImportState>('/books/import'),
  startBookImport: (format?: BookFormat) => post<BookImportState>('/books/import', { format: format ?? '' }),
  retryBookImport: (format?: BookFormat) => post<BookImportState>('/books/import/retry', { format: format ?? '' }),
  matchBookImport: (body: { path: string; kind: BookFormat; olKey: string; title: string; author: string; authorKey?: string; year?: number; coverId?: number; rename: boolean }) =>
    post<BookImportState>('/books/import/match', body),
  bookSubjects: () => get<{ label: string; subject: string }[]>('/books/subjects'),
  addBook: (data: { olKey: string; title: string; author: string; authorKey?: string; year?: number; coverId?: number; ebook: boolean; audiobook: boolean; searchNow?: boolean }) =>
    post<Book | Requested>('/books', data),
  setBookWanted: (id: number, format: BookFormat, wanted: boolean) => put<Book>(`/books/${id}/want`, { format, wanted }),
  deleteBook: (id: number, deleteFiles: boolean) => del<null>(`/books/${id}?deleteFiles=${deleteFiles}`),
  // Change cover: upload a picture, give a link to one, pick another edition's cover, or take the choice away.
  uploadBookCover: (id: number, file: Blob, name: string) => {
    const form = new FormData()
    form.set('file', file, name)
    return request<Book>(`/books/${id}/cover`, { method: 'POST', body: form, headers: {} })
  },
  setBookCoverLink: (id: number, url: string) => post<Book>(`/books/${id}/cover/link`, { url }),
  bookCoverEditions: (id: number) => get<{ editions: { coverId: number; url: string }[]; current: number }>(`/books/${id}/cover/editions`),
  pickBookCover: (id: number, coverId: number) => put<Book>(`/books/${id}/cover/edition`, { coverId }),
  removeBookCover: (id: number) => del<Book>(`/books/${id}/cover`),
  bookReleases: (id: number, format: BookFormat) => get<SearchAnswer<SearchResult>>(`/books/${id}/releases?format=${format}&detail=1`),
  grabBook: (id: number, data: { format: BookFormat; releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ queueId: number }>(`/books/${id}/grab`, data),
  searchNowBook: (id: number, format: BookFormat) => post<{ grabbed: number; message: string }>(`/books/${id}/search?format=${format}`),
  listArtists: () => request<MusicArtist[]>('/music/artists', { revalidate: true }),
  getArtist: (id: number) => get<MusicArtist>(`/music/artists/${id}`),
  addArtist: (data: { mbid: string; monitor?: MusicMonitor; profileId?: number; searchNow?: boolean; albumMbid?: string; albumTitle?: string }) => post<MusicArtist | Requested>('/music/artists', data),
  updateArtist: (id: number, changes: { monitored?: boolean; profileId?: number; filter?: MusicFilter; useLibraryFilter?: boolean }) => put<MusicArtist>(`/music/artists/${id}`, changes),
  musicFilters: () => get<MusicFilterChoices>('/music/filters'),
  setMusicFilters: (filter: MusicFilter) => put<MusicFilterChoices>('/music/filters', filter),
  artistDiscography: (id: number) => get<MusicDiscography>(`/music/artists/${id}/discography`),
  addArtistAlbum: (id: number, mbid: string) => post<MusicAlbum>(`/music/artists/${id}/albums`, { mbid }),
  deleteArtist: (id: number, deleteFiles: boolean) => del<null>(`/music/artists/${id}?deleteFiles=${deleteFiles}`),
  getAlbum: (id: number) => get<MusicAlbum>(`/music/albums/${id}`),
  setAlbumMonitored: (id: number, monitored: boolean) => put<null>(`/music/albums/${id}/monitored`, { monitored }),
  albumSearch: (id: number) => post<SearchAnswer<MusicRelease>>(`/music/albums/${id}/search?detail=1`),
  grabAlbum: (id: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent' }) =>
    post<{ queueId: number }>(`/music/albums/${id}/grab`, data),
  searchNowAlbum: (id: number) => post<{ grabbed: number; message: string }>(`/music/albums/${id}/search-now`),
  albumFiles: (id: number) => get<{ folder: string; files: TitleFile[] }>(`/music/albums/${id}/files`),
  rescanAlbum: (id: number) => post<AlbumRescanReport>(`/music/albums/${id}/rescan`),
  deleteAlbumFile: (id: number, path: string) => del<AlbumRescanReport>(`/music/albums/${id}/files?path=${encodeURIComponent(path)}`),
  albumStreamUrl: (id: number, path: string) => withBase(`/api/files/stream?album=${id}&path=${encodeURIComponent(path)}`),
  albumEvents: (id: number) => get<TitleEvent[]>(`/music/albums/${id}/events`),
  musicWanted: (kind: 'missing' | 'cutoff') => request<MusicWanted[]>(`/music/wanted?kind=${kind}`, { revalidate: true }),
  musicPlaylists: () => get<MusicPlaylist[]>('/music/playlists'),
  musicPlaylist: (id: number) => get<MusicPlaylistDetail>(`/music/playlists/${id}`),
  createMusicPlaylist: (data: { name: string; shared?: boolean } & PlaylistSelection) => post<MusicPlaylistDetail>('/music/playlists', data),
  updateMusicPlaylist: (id: number, changes: { name?: string; shared?: boolean }) => put<MusicPlaylistDetail>(`/music/playlists/${id}`, changes),
  deleteMusicPlaylist: (id: number) => del<null>(`/music/playlists/${id}`),
  addToMusicPlaylist: (id: number, what: PlaylistSelection) => post<{ added: number; playlist: MusicPlaylistDetail }>(`/music/playlists/${id}/tracks`, what),
  removeFromMusicPlaylist: (id: number, entryId: number) => del<MusicPlaylistDetail>(`/music/playlists/${id}/tracks/${entryId}`),
  reorderMusicPlaylist: (id: number, entryIds: number[]) => put<MusicPlaylistDetail>(`/music/playlists/${id}/order`, { entryIds }),
  trackLyrics: (id: number) => get<TrackLyrics>(`/music/tracks/${id}/lyrics`),
  trackGain: (id: number) => get<TrackGainInfo>(`/music/tracks/${id}/gain`),
  musicPlayerSettings: () => get<{ lyricsOnline: boolean }>('/music/player-settings'),
  setMusicPlayerSettings: (lyricsOnline: boolean) => put<{ lyricsOnline: boolean }>('/music/player-settings', { lyricsOnline }),
  listenBrainz: () => get<ListenBrainzStatus>('/music/listenbrainz'),
  saveListenBrainz: (data: { token?: string; enabled?: boolean }) => put<ListenBrainzStatus>('/music/listenbrainz', data),
  forgetListenBrainz: () => del<null>('/music/listenbrainz'),
  scrobblePlaying: (trackId: number) => post<{ sent: boolean }>('/music/listenbrainz/playing', { trackId }),
  scrobbleListen: (trackId: number, listenedAt: number) => post<{ sent: boolean }>('/music/listenbrainz/listen', { trackId, listenedAt }),
  startMusicScan: () => post<{ id: string }>('/music/import/scan'),
  musicScan: (id: string) => get<MusicImportJob>(`/music/import/scan/${id}`),
  // The latest scan, running or finished; id is empty when none has run.
  latestMusicScan: () => get<MusicImportJob | { id: '' }>('/music/import/scan'),
  stopMusicScan: (id: string) => del<MusicImportJob>(`/music/import/scan/${id}`),

  searchTV: (q: string) => get<DiscoverMovie[]>(`/tv/search?q=${encodeURIComponent(q)}`),
  listSeries: () => request<Series[]>('/series?brief=1', { revalidate: true }),
  getSeries: (id: number) => get<SeriesDetail>(`/series/${id}`),
  addSeries: (tmdbId: number, options: AddSeriesOptions = {}) => post<Series | Requested>('/series', { tmdbId, ...options }),
  refreshSeries: (id: number) => post<Series>(`/series/${id}/refresh`),
  getStreamPause: () => get<{ enabled: boolean; watching: number; servers: number; held: number }>('/downloads/stream-pause'),
  putStreamPause: (enabled: boolean) => put<{ enabled: boolean; watching: number; servers: number; held: number }>('/downloads/stream-pause', { enabled }),
  rescanMovie: (id: number) => post<RescanReport>(`/movies/${id}/rescan`),
  rescanSeries: (id: number) => post<RescanReport>(`/series/${id}/rescan`),
  browseLibrary: (path = '') => get<BrowseAnswer>(`/library/browse?path=${encodeURIComponent(path)}`),
  locateMovieFile: (id: number, path: string) => post<RescanReport>(`/movies/${id}/locate-file`, { path }),
  locateEpisodeFile: (id: number, path: string) => post<RescanReport>(`/episodes/${id}/locate-file`, { path }),
  locateMovieFolder: (id: number, path: string) => post<RescanReport>(`/movies/${id}/locate-folder`, { path }),
  changeSeriesFolder: (id: number, path: string) => post<SeriesFolderReport>(`/series/${id}/change-folder`, { path }),
  deleteMovieFile: (id: number) => del<{ files: number; episodes: number; trashed: boolean }>(`/movies/${id}/file`),
  deleteEpisodeFile: (id: number) => del<{ files: number; episodes: number; trashed: boolean }>(`/episodes/${id}/file`),
  redoIncomplete: (req: { movieIds?: number[]; seriesIds?: number[]; episodeIds?: number[] }) =>
    post<{ files: number; titles: number; searching: number; skipped: number }>('/library/incomplete/redo', req),
  deleteSeasonFiles: (seriesId: number, season: number) => del<{ files: number; episodes: number; trashed: boolean }>(`/series/${seriesId}/seasons/${season}/files`),
  deleteSeries: (id: number, deleteFiles = false) => del<null>(`/series/${id}?deleteFiles=${deleteFiles}`),
  titleCopies: (kind: 'movie' | 'series', id: number) => get<TitleCopies>(`/${kind === 'movie' ? 'movies' : 'series'}/${id}/copies`),
  addTitleCopy: (kind: 'movie' | 'series', id: number, profileId: number) => post<TitleCopy>(`/${kind === 'movie' ? 'movies' : 'series'}/${id}/copies`, { profileId }),
  seriesSearch: (id: number, season: number, episode?: number, q?: string) =>
    get<SearchAnswer<SearchResult>>(`/series/${id}/search?detail=1&season=${season}${episode ? `&episode=${episode}` : ''}${q ? `&q=${encodeURIComponent(q)}` : ''}`),
  grabSeries: (id: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent'; season?: number; episode?: number; replaceQueueId?: number; replace?: boolean }) =>
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
  debrid: () => get<DebridSettings>('/debrid'),
  saveDebrid: (service: string, apiKey: string, cached?: { preferCached: boolean; cachedLowerScore: boolean }) => put<DebridSettings>('/debrid', { service, apiKey, ...cached }),
  testDebrid: (service: string, apiKey: string) => post<{ ok: boolean; account?: DebridAccount; error?: string }>('/debrid/test', { service, apiKey }),
  downloadClient: () => get<DownloadClientSettings>('/download-client'),
  usenetClient: () => get<UsenetClientSettings>('/usenet-client'),
  saveUsenetClient: (data: UsenetClientInput) => put<UsenetClientSettings>('/usenet-client', data),
  testUsenetClient: (data: UsenetClientInput) => post<{ ok: boolean; version?: string; error?: string }>('/usenet-client/test', data),
  saveDownloadClient: (data: DownloadClientInput) => put<DownloadClientSettings>('/download-client', data),
  testDownloadClient: (data: DownloadClientInput) => post<{ ok: boolean; version?: string; error?: string }>('/download-client/test', data),
  plexWatchlist: () => get<PlexWatchlistStatus>('/plex-watchlist'),
  plexWatchlistPin: () => post<{ pinId: number; code: string; authUrl: string; expiresIn: number }>('/plex-watchlist/pin', {}),
  plexWatchlistPinStatus: (pinId: number) => get<{ done: boolean; status?: PlexWatchlistStatus }>(`/plex-watchlist/pin/${pinId}`),
  setPlexWatchlist: (autoAdd: boolean) => put<PlexWatchlistStatus>('/plex-watchlist', { autoAdd }),
  disconnectPlexWatchlist: () => del<void>('/plex-watchlist'),
  checkPlexWatchlist: () => post<{ added: number; requested: number; status: PlexWatchlistStatus }>('/plex-watchlist/check', {}),
  plexWatchlistItems: () => get<DiscoverMovie[]>('/plex-watchlist/items'),
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
  sendFeedback: (req: { kind: 'problem' | 'idea'; title: string; message: string; contact?: string; report?: string; lang?: string; screenshot?: string }) =>
    post<{ id: string }>('/feedback', req),
  usageStats: () => get<UsageStatsState>('/usage-stats'),
  setUsageStats: (choice: 'yes' | 'no') => put<UsageStatsState>('/usage-stats', { choice }),
  resetUsageStatsId: () => post<UsageStatsState>('/usage-stats/reset-id'),
  problems: (query = '') => get<ProblemList>(`/system/problems${query}`),
  problemCounts: () => get<{ counts: ProblemCounts }>('/system/problems?summary=1').then((r) => r.counts),
  // With all: true, `query` (the page's filters, as problemQuery makes them) limits it to what matches; without one it is every problem.
  markProblemsRead: (req: { ids?: number[]; all?: boolean }, query = '') => post<{ marked: number; counts: ProblemCounts }>(`/system/problems/read${query}`, req),
  setNotifyOnProblems: (enabled: boolean) => put<{ notifyOnProblems: boolean }>('/system/problems/notify', { enabled }),
  setDebugLogging: (enabled: boolean) => put<{ debugLogging: boolean }>('/system/problems/debug', { enabled }),
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
  mcpSettings: () => get<McpSettings>('/system/mcp'),
  putMcpSettings: (enabled: boolean) => put<McpSettings>('/system/mcp', { enabled }),
  mcpApps: () => get<{ apps: McpApp[]; clients: McpClient[] }>('/system/mcp/apps'),
  revokeMcpApp: (id: number) => del<null>(`/system/mcp/apps/${id}`),
  createMcpClient: (name: string, redirectUri: string) => post<McpClientMade>('/system/mcp/clients', { name, redirectUri }),
  deleteMcpClient: (clientId: string) => del<null>(`/system/mcp/clients/${encodeURIComponent(clientId)}`),
  oauthRequest: (id: string) => get<OAuthRequest>(`/oauth/requests/${encodeURIComponent(id)}`),
  oauthDecide: (id: string, allow: boolean, write: boolean) => post<{ redirect: string }>(`/oauth/requests/${encodeURIComponent(id)}`, { allow, write }),
  getProxySignIn: () => get<ProxySignIn>('/auth/proxy-signin'),
  getScripts: () => get<ScriptsState>('/scripts'),
  putScripts: (body: { script: string; timeoutSec: number }) => put<ScriptsState>('/scripts', body),
  testScript: () => post<ScriptRun>('/scripts/test', {}),
  watchedSettings: () => get<WatchedSettings>('/watched/settings'),
  putWatchedSettings: (body: { sync?: boolean; cleanup?: CleanupRules }) => put<WatchedSettings>('/watched/settings', body),
  syncWatched: () => post<WatchedStatus>('/watched/sync', {}),
  cleanupPreview: (rules: CleanupRules) => get<{ items: LibraryCleanupItem[]; limit: number }>(`/watched/cleanup/preview?${new URLSearchParams({ rules: JSON.stringify(rules) })}`),
  runLibraryCleanup: (items?: LibraryCleanupItem[]) =>
    post<{ removed: LibraryCleanupItem[]; failed: LibraryCleanupItem[]; more: number }>('/watched/cleanup/run', items ? { items: items.map((i) => ({ kind: i.kind, id: i.id })) } : {}),
  watched: () => get<{ movies: Record<string, WatchedTitle>; series: Record<string, WatchedTitle> }>('/watched'),
  putProxySignIn: (header: string, addresses: string) => put<ProxySignIn>('/auth/proxy-signin', { header, addresses }),
  getIdleSignOut: () => get<IdleSignOut>('/auth/idle-signout'),
  putIdleSignOut: (days: number) => put<IdleSignOut>('/auth/idle-signout', { days }),
  restartApp: (safe = false) => post<{ ok: boolean; restarting: boolean; safe: boolean }>(`/system/restart${safe ? '?safe=true' : ''}`),
  shutdownApp: () => post<{ ok: boolean }>('/system/shutdown'),
  movieEvents: (id: number) => get<TitleEvent[]>(`/movies/${id}/events`),
  seriesEvents: (id: number) => get<TitleEvent[]>(`/series/${id}/events`),
  movieFiles: (id: number) => get<{ folder: string; files: TitleFile[] }>(`/movies/${id}/files`),
  seriesFiles: (id: number) => get<{ folder: string; files: TitleFile[] }>(`/series/${id}/files`),
  streamUrl: (kind: 'movie' | 'series', id: number, path: string) => withBase(`/api/files/stream?${kind}=${id}&path=${encodeURIComponent(path)}`),
  discoverList: (kind: 'movie' | 'tv', list: 'trending' | 'popular' | 'upcoming' | 'similar', page = 1, older = false) =>
    get<{ page: number; totalPages: number; totalResults?: number; results: DiscoverMovie[] }>(
      `/discover/list?kind=${kind}&list=${list}&page=${page}${older ? '&older=true' : ''}`,
    ),
  discoverGenres: (kind: 'movie' | 'tv') => get<{ id: number; name: string }[]>(`/discover/genres?kind=${kind}`),
  discoverBrowse: (q: { kind: 'movie' | 'tv'; genre?: string; yearFrom?: string; yearTo?: string; sort?: string; language?: string; minRating?: string; runtime?: string; page?: number }) =>
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
  vpnTest: () => post<VPNTestResult>('/vpn/test'),
  pushKey: () => get<{ publicKey: string }>('/notifications/push-key'),
  statsHistory: (days = 0) => get<{ days: StatsDay[] }>(`/stats/history${days ? `?days=${days}` : ''}`),
  getProxy: () => get<ProxySettings>('/proxy'),
  putProxy: (data: ProxyInput) => put<ProxySettings>('/proxy', data),
  testProxy: (data: ProxyInput) => post<{ ok: boolean; message: string }>('/proxy/test', data),

  listIrcAnnounce: () => get<IrcAnnounceList>('/irc-announce'),
  createIrcConnection: (data: IrcConnectionInput) => post<IrcConnection>('/irc-announce', data),
  updateIrcConnection: (id: number, data: IrcConnectionInput) => put<IrcConnection>(`/irc-announce/${id}`, data),
  deleteIrcConnection: (id: number) => del<null>(`/irc-announce/${id}`),
  setIrcConnectionEnabled: (id: number, enabled: boolean) => put<IrcConnection>(`/irc-announce/${id}/enabled`, { enabled }),
  ircAnnounceLog: (id: number) => get<IrcLogRow[]>(`/irc-announce/${id}/log`),
  testIrcAnnounce: (data: IrcConnectionInput & { lines: string[] }) => post<IrcTestResult>('/irc-announce/test', data),

  listNotificationTargets: () => get<NotificationTarget[]>('/notifications'),
  createNotificationTarget: (data: Record<string, unknown>) => post<NotificationTarget>('/notifications', data),
  updateNotificationTarget: (id: number, data: Record<string, unknown>) => put<NotificationTarget>(`/notifications/${id}`, data),
  notificationTypes: () => get<NotifyType[]>('/notifications/types'),
  listApps: () => get<AppLink[]>('/apps'),
  saveApps: (list: AppLink[]) => put<AppLink[]>('/apps', list),
  getQuiet: () => get<QuietState>('/automation/quiet'),
  setQuiet: (minutes: number) => put<QuietState>('/automation/quiet', { minutes }),
  sendDigest: () => post<{ sent: number }>('/notifications/digest'),
  testNotification: (data: Record<string, unknown>) => post<{ sent: boolean; error?: string; steps?: NotifyTestStep[] }>('/notifications/test', data),
  monitorStatus: () => get<MonitorStatus[]>('/monitor/status'),
  runMonitor: () => post<null>('/monitor/run'),
  deleteNotificationTarget: (id: number) => del<null>(`/notifications/${id}`),

  calendar: () => request<CalendarEntry[]>('/calendar', { revalidate: true }),

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
  bulkReplaceSmaller: (items: BulkTitle[]) => post<BulkSearchResult>('/library/bulk/replace-smaller', { items }),
  // deleteFiles is sent every time and is false unless the person ticked it.
  bulkRemove: (items: BulkTitle[], deleteFiles: boolean) => post<BulkResult>('/library/bulk/remove', { items, deleteFiles }),
  bulkArtistFollow: (ids: number[], monitored: boolean) => put<BulkResult>('/music/bulk/follow', { ids, monitored }),
  bulkArtistProfile: (ids: number[], profileId: number) => put<BulkResult>('/music/bulk/profile', { ids, profileId }),
  bulkArtistRemove: (ids: number[], deleteFiles: boolean) => post<BulkResult>('/music/bulk/remove', { ids, deleteFiles }),
  tmdbSearch: (kind: 'movie' | 'tv', q: string) =>
    get<ImportCandidate[]>(`/tmdb/search?kind=${kind}&q=${encodeURIComponent(q)}`),

  subtitleAddons: () => get<SubtitleAddon[]>('/subtitles/addons'),
  addSubtitleAddon: (url: string) => post<SubtitleAddon>('/subtitles/addons', { url }),
  removeSubtitleAddon: (id: string) => del<{ ok: boolean }>(`/subtitles/addons/${encodeURIComponent(id)}`),
  testSubtitleAddon: (id: string) => post<{ ok: boolean; message: string }>(`/subtitles/addons/${encodeURIComponent(id)}/test`),
  testService: (data: { service: 'tmdb' | 'opensubtitles' | 'subdl' | 'wizdom' | 'trakt' | 'simkl'; key?: string; username?: string; password?: string }) =>
    post<{ ok: boolean; message: string }>('/settings/test-service', data),
  createFolder: (path: string) => post<FolderCheck>('/settings/folder-create', { path }),
  createRootMarker: (path: string, driveConnected: boolean) => post<{ ok: boolean; folder: string }>('/settings/library-marker', { path, driveConnected }),
  folderCheck: (path: string) => get<FolderCheck>(`/settings/folder-check?path=${encodeURIComponent(path)}`),
  health: () => get<{ items: HealthItem[] }>('/health'),
  modules: () => get<ModulesAnswer>('/modules'),
  setModules: (changes: Partial<Record<ModuleKey, boolean>>) => put<Modules>('/modules', changes),
  dashboard: () => request<DashboardData>('/dashboard', { revalidate: true }),

  filesystemCheck: (a: string, b: string) =>
    get<{ sameFilesystem: boolean; supported: boolean; error?: string }>(
      `/settings/filesystem-check?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`,
    ),

  namingPreview: (preset: string, format?: string, kind: 'movie' | 'tv' = 'movie', keep = false, folder?: string) =>
    get<{ folder: string; filename: string; problem?: string }>(namingPreviewPath(preset, format, kind, keep, folder)),

  namingTokens: () => get<NamingTokens>('/settings/naming-tokens'),

  // Library files: permissions, moving titles, collections, NFO and artwork.
  getFilePermissions: () => get<FilePermissions>('/settings/file-permissions'),
  putFilePermissions: (body: { fileMode?: string; folderMode?: string; confirmWorldWritable?: boolean }) => put<FilePermissions>('/settings/file-permissions', body),
  collections: () => get<CollectionListItem[]>('/collections'),
  collection: (id: number) => get<CollectionInfo>(`/collections/${id}`),
  addCollection: (id: number, body: AddCollectionBody) => post<AddCollectionResult>(`/collections/${id}/add`, body),
  followCollection: (id: number, body: { follow: boolean; profileId?: number; rootPath?: string; monitored?: boolean }) => put<CollectionInfo>(`/collections/${id}/follow`, body),
  bulkFolder: (items: BulkTitle[], rootPath: string) => post<BulkResult>('/library/bulk/folder', { items, rootPath }),
  previewMoveFiles: (items: BulkTitle[], rootPath: string) => post<MovePreview>('/library/move-files/preview', { items, rootPath }),
  moveFiles: (items: BulkTitle[], rootPath: string) => post<MoveStatus>('/library/move-files', { items, rootPath }),
  moveFilesStatus: () => get<MoveStatus>('/library/move-files/status'),
  cancelMoveFiles: () => post<MoveStatus>('/library/move-files/cancel', {}),
  getMetadataFiles: () => get<MetadataFilesSettings>('/settings/metadata-files'),
  putMetadataFiles: (body: Omit<MetadataFilesSettings, 'hasTmdbKey'>) => put<MetadataFilesSettings>('/settings/metadata-files', body),
  runMetadataFiles: (body: { dryRun?: boolean; kind?: 'all' | 'movies' | 'tv' }) => post<MetadataFilesStatus>('/library/metadata-files/run', body),
  metadataFilesStatus: () => get<MetadataFilesStatus>('/library/metadata-files/status'),
  cancelMetadataFiles: () => post<MetadataFilesStatus>('/library/metadata-files/cancel', {}),
}

export interface MoveTitle {
  kind: 'movie' | 'tv'
  id: number
  title: string
  from?: string
  to?: string
  files: number
  bytes: number
  method: 'rename' | 'copy' | '' // same drive, another drive, or nothing on disk to move
  problem?: string // why this title will be left as it is
  note?: string
  linkedFiles?: number // files hard-linked to something else (a seeding torrent)
  linkNote?: string // what a copy to another drive does to those links
}

export interface MovePreview {
  titles: MoveTitle[]
  canMove: number
  blocked: number
  files: number
  bytes: number
  copyBytes: number
  freeBytes: number
  problem?: string // a reason that stops the whole move
  truncated?: number
}

export interface MoveStatus {
  running: boolean
  titles: number
  done: number
  moved: number
  left: number
  bytesAll: number
  bytesDone: number
  current?: string
  problems: { title: string; reason: string }[]
  warnings: string[]
  folder?: string
  startedAt?: string
  finishedAt?: string
  canceled: boolean
  message?: string
}

export interface MetadataFilesSettings {
  movies: { nfo: boolean; artwork: boolean }
  tv: { nfo: boolean; artwork: boolean }
  replace: boolean // replace the files Mediarium wrote earlier instead of keeping them
  hasTmdbKey: boolean
}

export interface MetadataFileCounts {
  nfo: number
  posters: number
  fanart: number
  logos: number
  seasonPosters: number
  episodeNfos: number
}

export interface MetadataFilesStatus {
  running: boolean
  dryRun: boolean
  kind: string
  titles: number // titles looked at; for a dry run, the titles that would get files is `done`
  done: number
  current?: string
  needed: MetadataFileCounts
  written: MetadataFileCounts
  failed: string[]
  failedCount: number
  startedAt?: string
  finishedAt?: string
  canceled: boolean
  message?: string
}

export interface CollectionRef {
  id: number
  name: string
  posterUrl?: string
}

export interface CollectionPart {
  tmdbId: number
  title: string
  year?: number
  releaseDate?: string
  posterUrl?: string
  released: boolean
  libraryId?: number // not there when the film is not in the library
  status?: 'missing' | 'downloading' | 'downloaded'
}

export interface CollectionInfo {
  id: number
  name: string
  overview?: string
  posterUrl?: string
  backdropUrl?: string
  parts: CollectionPart[]
  inLibrary: number
  follow: boolean
  followProfileId?: number
  followRootPath?: string
  followMonitored: boolean
}

export interface CollectionListItem {
  id: number
  name: string
  posterUrl?: string
  movieIds: number[]
  follow: boolean
}

export interface AddCollectionBody {
  profileId?: number
  rootPath?: string
  monitored?: boolean
  sources?: SourcePref
  searchNow?: boolean
  tmdbIds?: number[]
  follow?: boolean
}

export interface AddCollectionResult {
  added: number
  alreadyHad: number
  failed: string[]
  following: boolean
  message: string
}

export interface FilePermissions {
  fileMode: string // three octal digits like "664"; "" = the container default
  folderMode: string
  umask: string // the UMASK the container was started with, "" when none
}

// IRC announce: connections to the IRC channels where trackers announce new releases.
export interface IrcTemplate {
  multi: boolean
  patterns: string[]
  sizeUnits: string
  downloadUrl: string
}

export interface IrcPreset {
  id: string
  name: string
  description: string
  template: IrcTemplate
  keys: { name: string; hint: string }[]
  auth: string
  inviteHint: string
  examples: string[]
}

export interface IrcChannelStatus {
  name: string
  joined: boolean
  error?: string
}

export interface IrcStatus {
  state: 'stopped' | 'connecting' | 'connected' | 'joined'
  channels: IrcChannelStatus[]
  lastError: string
  connectedAt?: string
  lastAnnounce?: string
  reconnects: number
  heard: number
  grabbed: number
  dropped: number
  problem: string
}

export interface IrcConnection {
  id: number
  name: string
  enabled: boolean
  preset: string
  server: string
  port: number
  tls: boolean
  insecureTls: boolean
  nick: string
  auth: 'none' | 'sasl' | 'nickserv'
  authAccount: string
  hasPassword: boolean
  hasInvite: boolean
  channels: { name: string; hasKey: boolean }[]
  announcers: string[]
  trackerUrl: string
  template: IrcTemplate
  keys: { name: string; hasValue: boolean }[]
  needKeys: string[]
  freeleechOnly: boolean
  maxPerHour: number
  maxPerDay: number
  status: IrcStatus
}

export interface IrcAnnounceList {
  connections: IrcConnection[]
  presets: IrcPreset[]
}

// What create, update and test take. A blank password, invite command, channel
// password or key keeps the saved one.
export interface IrcConnectionInput {
  name?: string
  enabled?: boolean
  preset?: string
  server?: string
  port?: number
  tls?: boolean
  insecureTls?: boolean
  nick?: string
  auth?: string
  authAccount?: string
  authPassword?: string
  invite?: string
  clearInvite?: boolean
  channels?: { name: string; key: string }[]
  announcers?: string[]
  trackerUrl?: string
  template?: IrcTemplate
  keys?: { name: string; value: string }[]
  freeleechOnly?: boolean
  maxPerHour?: number
  maxPerDay?: number
  id?: number
}

export interface IrcLogRow {
  at: string
  channel: string
  announcer: string
  release: string
  category: string
  sizeBytes: number
  decision: 'grabbed' | 'would-grab' | 'skipped' | 'duplicate' | 'ignored' | 'failed'
  reason: string
  item: string
}

export interface IrcTestResult {
  matched: boolean
  message: string
  fields: Record<string, string>
  release: string
  sizeBytes: number
  category: string
  uploader: string
  torrentId: string
  freeleech: boolean
  downloadUrl: string
  urlProblem: string
  decision: IrcLogRow['decision'] | ''
  reason: string
  item: string
}

export interface FlareSolverrStatus {
  bundled: boolean
  configured: boolean
  running: boolean
  version?: string
}

// The list of switched-on modules is asked for while the sign-in is still being checked (the page needs it
// right after), and the first read of it afterwards takes that answer instead of asking again.
let earlyModules: Promise<ModulesAnswer> | null = null

export function startModulesEarly() {
  earlyModules = api.modules()
  earlyModules.catch(() => undefined)
}

export function takeEarlyModules(): Promise<ModulesAnswer> | null {
  const early = earlyModules
  earlyModules = null
  return early
}
