// Thin typed wrapper around the REST API in internal/api. Session auth is
// a plain HTTP-only cookie (PRD §5.1), so every call just needs
// credentials: 'include' — no token to manage client-side.

const BASE = '/api'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    credentials: 'include',
    cache: 'no-store',
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    ...init,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `request failed with status ${res.status}`)
  }
  return data as T
}

const get = <T>(path: string) => request<T>(path)
const post = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body !== undefined ? JSON.stringify(body) : undefined })
const put = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'PUT', body: body !== undefined ? JSON.stringify(body) : undefined })
const del = <T>(path: string) => request<T>(path, { method: 'DELETE' })

export interface OnboardingStatus {
  firstRunNeeded: boolean
}

export interface User {
  id: number
  username: string
  isAdmin?: boolean
  name?: string
  email?: string
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
  hasOpenSubtitlesApiKey?: boolean
  hasOpenSubtitlesAccount?: boolean
  openSubtitlesAccountName?: string
  tmdbKeyBuiltIn?: boolean
  openSubtitlesKeyBuiltIn?: boolean
  traktClientIdBuiltIn?: boolean
  subtitleLanguages?: string[]
  subtitleAutoDownload?: boolean
  illegalCharMode?: string
  illegalCharReplacement?: string
  requireVpnForTorrents?: boolean
  importConflictPolicy?: string
  torrentEnabled?: boolean
  legalAcknowledgedAt?: string
}

export interface IndexerConfig {
  id: number
  name: string
  definitionId: string
  baseUrl: string
  protocol: 'usenet' | 'torrent'
  enabled: boolean
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
  torrent: { ready: boolean; listenPort: number; vpnRequired: boolean; vpnConnected: boolean; blockedByVpn: boolean }
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
}

export interface AddSeriesOptions {
  profileId?: number
  monitor?: 'all' | 'future' | 'none'
  sources?: SourcePref
  searchNow?: boolean
}

export interface Series {
  id: number
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
}

export interface Movie {
  id: number
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
}

export interface QueueItem {
  id: number
  movieId: number
  seriesId?: number
  tmdbId?: number
  title: string
  subtitle?: string
  posterUrl?: string
  protocol: 'usenet' | 'torrent'
  addedAt?: string
  completedAt?: string
  releaseTitle: string
  sizeBytes: number
  status: 'queued' | 'downloading' | 'importing' | 'completed' | 'failed' | 'conflict'
  progressPct: number
  error?: string
  destPath?: string
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

export interface TitleDetails {
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

export interface SubtitleWanted {
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
  connected: boolean
  label?: string
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

export interface ImportResult {
  key: string
  title: string
  imported: number
  skipped: number
  message?: string
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
  results: ImportResult[]
  error?: string
}

export interface FolderCheck {
  path: string
  exists: boolean
  writable: boolean
  freeBytes: number
  totalBytes: number
  mounted: boolean
  mountKnown: boolean
  warnings: string[]
}

export interface HealthItem {
  id: string
  level: 'error' | 'warn' | 'info'
  title: string
  impact: string
  action?: { label: string; path: string }
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
  kind: 'movie' | 'series'
  id: number
  tmdbId?: number
  seriesId?: number
  title: string
  subtitle?: string
  year?: number
  posterUrl?: string
  quality?: string
  sizeBytes?: number
  at: string
}

export interface DashboardData {
  library: {
    movies: { total: number; downloaded: number; missing: number; downloading: number }
    series: { total: number; episodes: number; episodesDownloaded: number; episodesMissing: number }
    sizeBytes: number
    qualities: { tier: string; count: number }[]
  }
  folders: DashFolder[]
  active: DashActive[]
  recentlyAdded: DashRecent[]
  recentDownloads: DashRecent[]
  upcoming: CalendarEntry[]
  health: HealthItem[]
  setup: { indexers: number; usenetServers: number; vpnConnected: boolean }
}

export interface CalendarEntry {
  kind: 'movie' | 'episode'
  id: number
  movieId?: number
  seriesId?: number
  title: string
  subtitle?: string
  releaseDate: string
  status: 'missing' | 'downloading' | 'downloaded'
}

export const api = {
  version: () => get<{ version: string }>('/version'),
  onboardingStatus: () => get<OnboardingStatus>('/onboarding/status'),
  createAdmin: (data: { username: string; password: string; name?: string; email?: string }) => post<User>('/onboarding/admin', data),
  updateAccount: (data: { username: string; name: string; email: string }) => put<User>('/auth/profile', data),

  login: (username: string, password: string) => post<User>('/auth/login', { username, password }),
  logout: () => post<null>('/auth/logout'),
  me: () => get<User>('/auth/me'),
  changePassword: (currentPassword: string, newPassword: string) =>
    post<null>('/auth/change-password', { currentPassword, newPassword }),
  listAPIKeys: () => get<APIKey[]>('/auth/api-keys'),
  createAPIKey: (name: string) => post<APIKey>('/auth/api-keys', { name }),
  revokeAPIKey: (id: number) => del<null>(`/auth/api-keys/${id}`),

  getSettings: () => get<Settings>('/settings'),
  putSettings: (partial: Partial<Settings> & { legalAcknowledged?: boolean; tmdbApiKey?: string; openSubtitlesApiKey?: string; openSubtitlesUsername?: string; openSubtitlesPassword?: string }) => put<Settings>('/settings', partial),

  listIndexers: () => get<IndexerConfig[]>('/indexers'),
  createIndexer: (data: { name: string; definitionId: string; baseUrl: string; apiKey: string; protocol?: 'usenet' | 'torrent' }) =>
    post<IndexerConfig>('/indexers', data),
  deleteIndexer: (id: number) => del<null>(`/indexers/${id}`),
  testIndexerConfig: (data: { name: string; baseUrl: string; apiKey: string }) =>
    post<{ ok: boolean; message: string }>('/indexers/test', data),
  testIndexer: (id: number) => post<{ ok: boolean; message: string }>(`/indexers/${id}/test`),
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

  listMovies: () => get<Movie[]>('/movies'),
  getMovie: (id: number) => get<Movie>(`/movies/${id}`),
  addMovie: (tmdbId: number, options: AddMovieOptions = {}) => post<Movie>('/movies', { tmdbId, ...options }),
  discoverSearch: (q: string, signal?: AbortSignal) => request<TitleResult[]>(`/discover/search?q=${encodeURIComponent(q)}`, { signal }),
  setMovieSources: (id: number, sources: SourcePref) => put<null>(`/movies/${id}/sources`, { sources }),
  setSeriesSources: (id: number, sources: SourcePref) => put<null>(`/series/${id}/sources`, { sources }),
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
  subtitlesWanted: () => get<SubtitleWanted[]>('/subtitles/wanted'),
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
  deleteQueueItem: (id: number) => del<null>(`/queue/${id}`),
  clearFinishedQueue: () => del<{ removed: number }>('/queue'),
  resolveConflict: (id: number, overwrite: boolean) => post<null>(`/queue/${id}/resolve-conflict`, { overwrite }),
  getWanted: (kind: 'missing' | 'cutoff') => get<WantedItem[]>(`/wanted?kind=${kind}`),
  searchNowMovie: (id: number) => post<{ grabbed: number; message: string }>(`/movies/${id}/search-now`),
  searchNowSeries: (id: number, scope: { season?: number; episode?: number } = {}) =>
    post<{ grabbed: number; message: string }>(`/series/${id}/search-now`, scope),
  movieSearch: (id: number) => get<SearchResult[]>(`/movies/${id}/search`),
  setMovieMonitored: (id: number, monitored: boolean) => put<null>(`/movies/${id}/monitored`, { monitored }),
  setSeriesMonitored: (id: number, monitored: boolean) => put<null>(`/series/${id}/monitored`, { monitored }),
  setSeasonMonitored: (id: number, season: number, monitored: boolean) =>
    put<null>(`/series/${id}/seasons/${season}/monitored`, { monitored }),
  setEpisodeMonitored: (id: number, monitored: boolean) => put<null>(`/episodes/${id}/monitored`, { monitored }),
  listBlocklist: () => get<BlocklistEntry[]>('/blocklist'),
  removeBlocklistEntry: (id: number) => del<null>(`/blocklist/${id}`),
  clearBlocklist: () => del<null>('/blocklist'),
  listActivity: () => get<ActivityEntry[]>('/activity'),

  searchTV: (q: string) => get<DiscoverMovie[]>(`/tv/search?q=${encodeURIComponent(q)}`),
  listSeries: () => get<Series[]>('/series'),
  getSeries: (id: number) => get<SeriesDetail>(`/series/${id}`),
  addSeries: (tmdbId: number, options: AddSeriesOptions = {}) => post<Series>('/series', { tmdbId, ...options }),
  refreshSeries: (id: number) => post<Series>(`/series/${id}/refresh`),
  deleteSeries: (id: number, deleteFiles = false) => del<null>(`/series/${id}?deleteFiles=${deleteFiles}`),
  seriesSearch: (id: number, season: number, episode?: number) =>
    get<SearchResult[]>(`/series/${id}/search?season=${season}${episode ? `&episode=${episode}` : ''}`),
  grabSeries: (id: number, data: { releaseTitle: string; downloadUrl: string; sizeBytes: number; protocol?: 'usenet' | 'torrent'; season?: number; episode?: number }) =>
    post<{ queueId: number }>(`/series/${id}/grab`, data),

  discoverTrending: () => get<DiscoverMovie[]>('/discover/trending'),
  discoverPopular: () => get<DiscoverMovie[]>('/discover/popular'),
  discoverTrendingTV: () => get<DiscoverMovie[]>('/discover/tv/trending'),
  discoverPopularTV: () => get<DiscoverMovie[]>('/discover/tv/popular'),
  importList: (listUrl: string) => get<DiscoverMovie[]>(`/discover/import-list?url=${encodeURIComponent(listUrl)}`),
  discoverForYou: () => get<DiscoverMovie[]>('/discover/for-you'),

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
  notificationTypes: () => get<NotifyType[]>('/notifications/types'),
  testNotification: (data: Record<string, unknown>) => post<{ sent: boolean; error?: string }>('/notifications/test', data),
  monitorStatus: () => get<MonitorStatus[]>('/monitor/status'),
  runMonitor: () => post<null>('/monitor/run'),
  deleteNotificationTarget: (id: number) => del<null>(`/notifications/${id}`),

  calendar: () => get<CalendarEntry[]>('/calendar'),

  scanLibrary: (path: string, kind: 'movie' | 'tv') => post<{ jobId: string }>('/library/scan', { path, kind }),
  getScan: (id: string) => get<ImportJob>(`/library/scan/${id}`),
  runImport: (jobId: string, selections: { key: string; tmdbId: number }[]) =>
    post<{ jobId: string }>('/library/import', { jobId, selections }),
  tmdbSearch: (kind: 'movie' | 'tv', q: string) =>
    get<ImportCandidate[]>(`/tmdb/search?kind=${kind}&q=${encodeURIComponent(q)}`),

  testService: (data: { service: 'tmdb' | 'opensubtitles' | 'trakt'; key?: string; username?: string; password?: string }) =>
    post<{ ok: boolean; message: string }>('/settings/test-service', data),
  folderCheck: (path: string) => get<FolderCheck>(`/settings/folder-check?path=${encodeURIComponent(path)}`),
  health: () => get<{ items: HealthItem[] }>('/health'),
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
