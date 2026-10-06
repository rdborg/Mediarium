// Demo mode: sample titles in every state (downloaded, downloading, searching,
// pending, added, failed) so every screen can be judged with realistic content.
// It only fakes what the app READS; nothing is written to your library or
// database, and turning it off restores your real data.
import type {
  ActivityEntry,
  CalendarEntry,
  DashboardData,
  DiscoverMovie,
  Episode,
  Movie,
  MovieDetail,
  QueueItem,
  Series,
  SeriesDetail,
  TVDetail,
  TitleResult,
  WantedItem,
} from './api'

import { demoAuthorWorks, demoBookCounts, demoBookList, demoBooks, demoBookSearch, demoProgress, demoTracks, demoWork } from './demoBooks'

const KEY = 'mediarium-demo'

export function demoEnabled(): boolean {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

export function setDemo(on: boolean) {
  try {
    if (on) localStorage.setItem(KEY, '1')
    else localStorage.removeItem(KEY)
  } catch {
    // ignore
  }
  window.location.reload()
}

// Abstract, text-free poster art: two seeded hues, a horizon and a light.
function poster(seed: number): string {
  const h1 = (seed * 47) % 360
  const h2 = (h1 + 40 + ((seed * 13) % 90)) % 360
  const sun = 60 + ((seed * 29) % 120)
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 300"><defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="hsl(${h1} 65% 38%)"/><stop offset="1" stop-color="hsl(${h2} 70% 14%)"/></linearGradient></defs><rect width="200" height="300" fill="url(#g)"/><circle cx="${sun}" cy="${90 + (seed % 5) * 12}" r="${34 + (seed % 4) * 6}" fill="hsl(${h1} 90% 78%)" opacity=".55"/><path d="M0 230 L${40 + (seed % 7) * 9} ${150 + (seed % 5) * 8} L${95 + (seed % 3) * 15} 215 L${140 + (seed % 4) * 8} ${160 + (seed % 6) * 6} L200 235 V300 H0Z" fill="hsl(${h2} 60% 8%)" opacity=".85"/></svg>`
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
}

const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()
const day = (offset: number) => {
  const d = new Date()
  d.setDate(d.getDate() + offset)
  return d.toISOString().slice(0, 10)
}

interface Seed {
  id: number
  title: string
  year: number
  genres: string[]
  rating: number
  overview: string
  state: 'downloaded' | 'downloading' | 'searching' | 'pending' | 'added' | 'failed'
  quality?: string
  progress?: number
  noPoster?: boolean
}

const OV = 'A sample synopsis for demo mode. In the real app this text describes the plot in two or three sentences.'

const MOVIE_SEEDS: Seed[] = [
  { id: 1, title: 'Saltwind: Part Two', year: 2024, genres: ['Science Fiction', 'Adventure'], rating: 8.2, overview: OV, state: 'downloading', progress: 63, quality: '2160p' },
  { id: 2, title: 'The Long Equation', year: 2023, genres: ['Drama', 'History'], rating: 8.1, overview: OV, state: 'downloaded', quality: '2160p Remux' },
  { id: 3, title: 'The Marigold Lodge', year: 2014, genres: ['Comedy', 'Drama'], rating: 8.0, overview: OV, state: 'downloaded', quality: '1080p Bluray' },
  { id: 4, title: 'Neon Meridian', year: 2017, genres: ['Science Fiction', 'Drama'], rating: 7.6, overview: OV, state: 'downloaded', quality: '1080p WEB-DL' },
  { id: 5, title: 'Iron Dust', year: 2015, genres: ['Action', 'Adventure'], rating: 7.6, overview: OV, state: 'downloaded', quality: '1080p Bluray' },
  { id: 6, title: 'The Lantern Bathhouse', year: 2001, genres: ['Animation', 'Fantasy'], rating: 8.5, overview: OV, state: 'downloaded', quality: '1080p Bluray' },
  { id: 7, title: 'Northbound', year: 2014, genres: ['Adventure', 'Science Fiction'], rating: 8.4, overview: OV, state: 'downloading', progress: 12, quality: '1080p' },
  { id: 8, title: 'The Quiet Signal', year: 2016, genres: ['Science Fiction', 'Drama'], rating: 7.6, overview: OV, state: 'searching' },
  { id: 9, title: 'Night Warden', year: 2022, genres: ['Crime', 'Mystery'], rating: 7.7, overview: OV, state: 'searching' },
  { id: 10, title: 'Tidewalker', year: 2023, genres: ['Science Fiction', 'Horror'], rating: 7.7, overview: OV, state: 'pending' },
  { id: 11, title: 'A House of Suspects', year: 2019, genres: ['Mystery', 'Comedy'], rating: 7.8, overview: OV, state: 'added', noPoster: true },
  { id: 12, title: 'Half Basement', year: 2019, genres: ['Thriller', 'Drama'], rating: 8.5, overview: OV, state: 'downloaded', quality: '1080p WEB-DL' },
  { id: 13, title: 'All the Doors at Once', year: 2022, genres: ['Action', 'Comedy'], rating: 7.8, overview: OV, state: 'failed' },
  { id: 14, title: 'High Ceiling', year: 2022, genres: ['Action', 'Drama'], rating: 8.2, overview: OV, state: 'downloaded', quality: '2160p WEB-DL' },
]

const SERIES_SEEDS: (Seed & { episodes: number; have: number })[] = [
  { id: 101, title: 'The Clean Floor', year: 2022, genres: ['Drama', 'Mystery'], rating: 8.7, overview: OV, state: 'downloaded', episodes: 19, have: 19 },
  { id: 102, title: 'Salt & Static', year: 2022, genres: ['Drama', 'Comedy'], rating: 8.3, overview: OV, state: 'downloading', episodes: 28, have: 20, progress: 41 },
  { id: 103, title: 'The Cartographer’s Daughter', year: 2024, genres: ['Drama', 'War'], rating: 8.6, overview: OV, state: 'downloaded', episodes: 10, have: 10 },
  { id: 104, title: 'Ember Road', year: 2022, genres: ['Science Fiction', 'Action'], rating: 8.4, overview: OV, state: 'searching', episodes: 24, have: 12 },
  { id: 105, title: 'Paper Ghosts', year: 2022, genres: ['Crime', 'Drama'], rating: 8.2, overview: OV, state: 'pending', episodes: 30, have: 0, noPoster: true },
  { id: 106, title: 'Glass Orchard', year: 2021, genres: ['Animation', 'Fantasy'], rating: 8.8, overview: OV, state: 'downloaded', episodes: 18, have: 18 },
]

const movies: Movie[] = MOVIE_SEEDS.map((s) => ({
  id: s.id,
  tmdbId: 9000 + s.id,
  title: s.title,
  year: s.year,
  overview: s.overview,
  posterUrl: s.noPoster ? undefined : poster(s.id),
  status: s.state === 'downloaded' ? 'downloaded' : s.state === 'downloading' ? 'downloading' : 'missing',
  quality: s.quality,
  monitored: s.state !== 'added',
  filePath: s.state === 'downloaded' ? `/movies/${s.title} (${s.year})/${s.title} (${s.year}).mkv` : undefined,
}))

const series: Series[] = SERIES_SEEDS.map((s) => ({
  id: s.id,
  tmdbId: 9500 + s.id,
  title: s.title,
  year: s.year,
  overview: s.overview,
  posterUrl: s.noPoster ? undefined : poster(s.id),
  monitored: true,
  episodeCount: s.episodes,
  downloadedCount: s.have,
}))

const queue: QueueItem[] = [
  { id: 1, movieId: 1, tmdbId: 9001, title: 'Saltwind: Part Two', protocol: 'usenet', releaseTitle: 'Saltwind.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR10Plus.H.265-KESTREL', sizeBytes: 21.4e9, status: 'downloading', progressPct: 63, addedAt: ago(38), posterUrl: poster(1) },
  { id: 2, movieId: 7, tmdbId: 9007, title: 'Northbound', protocol: 'torrent', releaseTitle: 'Northbound.2014.1080p.BluRay.x264-LUMEN', sizeBytes: 9.1e9, status: 'downloading', progressPct: 12, addedAt: ago(9), posterUrl: poster(7) },
  { id: 3, movieId: 0, seriesId: 102, title: 'Salt & Static', subtitle: 'S03E08', protocol: 'usenet', releaseTitle: 'Salt.and.Static.S03E08.1080p.WEB.h264-ASHGROVE', sizeBytes: 2.3e9, status: 'downloading', progressPct: 41, addedAt: ago(6), posterUrl: poster(102) },
  { id: 4, movieId: 10, tmdbId: 9010, title: 'Tidewalker', protocol: 'usenet', releaseTitle: 'Tidewalker.2023.1080p.BluRay.x264-MARLIN', sizeBytes: 11.6e9, status: 'queued', queuePosition: 1, progressPct: 0, addedAt: ago(2), posterUrl: poster(10) },
  { id: 5, movieId: 13, tmdbId: 9013, title: 'All the Doors at Once', protocol: 'usenet', releaseTitle: 'All.the.Doors.at.Once.2022.1080p.WEB-DL-TINDER', sizeBytes: 8.2e9, status: 'failed', progressPct: 0, error: 'Your Usenet provider refused the login.', addedAt: ago(120), completedAt: ago(115), posterUrl: poster(13) },
  { id: 6, movieId: 2, tmdbId: 9002, title: 'The Long Equation', protocol: 'usenet', releaseTitle: 'The.Long.Equation.2023.2160p.UHD.BluRay.REMUX.DV.HDR.HEVC-ORBIT', sizeBytes: 58e9, status: 'completed', progressPct: 100, addedAt: ago(1500), completedAt: ago(1440), posterUrl: poster(2) },
  { id: 7, movieId: 0, seriesId: 103, title: 'The Cartographer’s Daughter', subtitle: 'S01E10', protocol: 'torrent', releaseTitle: 'The.Cartographers.Daughter.2024.S01E10.1080p.WEB.h264-ASHGROVE', sizeBytes: 3.1e9, status: 'completed', progressPct: 100, addedAt: ago(3000), completedAt: ago(2970), posterUrl: poster(103) },
]

const activity: ActivityEntry[] = [
  { id: 1, eventType: 'grabbed', message: 'Saltwind: Part Two: grabbed Saltwind.Part.Two.2024.2160p.WEB-DL from Indexer One', createdAt: ago(38) },
  { id: 2, eventType: 'added', message: 'Tidewalker added to the library', createdAt: ago(41) },
  { id: 3, eventType: 'failed', message: 'All the Doors at Once: your Usenet provider refused the login', createdAt: ago(115) },
  { id: 4, eventType: 'imported', message: 'The Long Equation imported to /movies/The Long Equation (2023)/The Long Equation (2023).mkv', createdAt: ago(1440) },
  { id: 5, eventType: 'subtitle', message: 'The Long Equation: downloaded English subtitles', createdAt: ago(1438) },
  { id: 6, eventType: 'blocklisted', message: 'The.Quiet.Signal.2016.720p.HDTV-JUNK blocklisted (bad release)', createdAt: ago(2100) },
  { id: 7, eventType: 'imported', message: 'The Cartographer’s Daughter: imported 1 episode from The.Cartographers.Daughter.2024.S01E10.1080p', createdAt: ago(2970) },
]

type Cat = [string, number, string[], number]
const CAT_MOVIES: Cat[] = [
  ['The Ninth Lighthouse', 2024, ['Action', 'Drama'], 6.8], ['Wildflower Bureau', 2024, ['Fantasy', 'Drama'], 7.4], ['Rust Valley Run', 2024, ['Action', 'Adventure'], 7.6],
  ['Small Weather', 2024, ['Animation', 'Comedy'], 7.6], ['The Second Skin', 2024, ['Horror', 'Science Fiction'], 7.1], ['Lucky Vera', 2024, ['Comedy', 'Drama'], 7.1],
  ['Border Static', 2024, ['Action', 'Thriller'], 7.0], ['Copper Summer', 2024, ['Drama', 'Romance'], 7.2], ['Letters Across the Strait', 2023, ['Drama', 'Romance'], 7.9],
  ['Winter Term', 2023, ['Comedy', 'Drama'], 7.7], ['Lemonade Kingdom', 2023, ['Comedy', 'Fantasy'], 7.0], ['The Salt Road Accounts', 2023, ['Crime', 'History'], 7.6],
  ['Last Contract', 2023, ['Action', 'Thriller'], 7.7], ['Over the Ridge', 2022, ['Horror', 'Mystery'], 6.8], ['The Conductor', 2022, ['Drama', 'Music'], 7.3],
  ['Tasting Menu', 2022, ['Horror', 'Comedy'], 7.2], ['Holiday Film Roll', 2022, ['Drama'], 7.6], ['Night Driver', 2011, ['Crime', 'Drama'], 7.6],
  ['Tempo', 2014, ['Drama', 'Music'], 8.3], ['The Voice in the Wire', 2013, ['Romance', 'Science Fiction'], 7.8], ['Far Side Station', 2009, ['Science Fiction', 'Drama'], 7.6],
  ['The Cartel Line', 2015, ['Crime', 'Thriller'], 7.6], ['Turing Garden', 2015, ['Science Fiction', 'Thriller'], 7.6], ['Seven Days Missing', 2013, ['Crime', 'Mystery'], 8.1],
]
const CAT_TV: Cat[] = [
  ['Wild Country', 2023, ['Drama', 'Action'], 8.6], ['Crown of Embers', 2022, ['Fantasy', 'Drama'], 8.4], ['Below Ground', 2024, ['Science Fiction', 'Action'], 8.3],
  ['Murders on Floor Six', 2021, ['Comedy', 'Mystery'], 8.0], ['The Drifter', 2022, ['Action', 'Crime'], 8.1], ['Bayou County', 2014, ['Crime', 'Drama'], 8.3],
  ['The Island Resort', 2021, ['Comedy', 'Drama'], 7.9], ['Coach Abroad', 2020, ['Comedy', 'Drama'], 8.4], ['Caves of Time', 2017, ['Science Fiction', 'Mystery'], 8.5],
  ['Glass Screens', 2011, ['Science Fiction', 'Drama'], 8.3], ['The Night Lawyer', 2015, ['Crime', 'Drama'], 8.7], ['Snowfall County', 2014, ['Crime', 'Drama'], 8.3],
  ['The Deep Tower', 2023, ['Science Fiction', 'Drama'], 8.1], ['Sisters of the Sand', 2024, ['Science Fiction', 'Drama'], 7.6], ['A Talented Stranger', 2024, ['Crime', 'Drama'], 8.0],
]

// Mostly titles you do not own (so Add buttons show), plus a couple you do.
const discoverPool = (offset: number, tv: boolean): DiscoverMovie[] => {
  const cat = tv ? CAT_TV : CAT_MOVIES
  const owned = tv ? SERIES_SEEDS : MOVIE_SEEDS
  const fresh = Array.from({ length: 10 }, (_, i) => {
    const [title, year, genres, rating] = cat[(i + offset * 3) % cat.length]
    const id = (tv ? 700 : 300) + ((i + offset * 3) % cat.length)
    return { tmdbId: (tv ? 9800 : 9300) + id, title, year, overview: OV, posterUrl: i % 7 === 4 ? undefined : poster(id), genres, rating, mediaType: (tv ? 'tv' : 'movie') as 'tv' | 'movie' }
  })
  const mine = [0, 1].map((k) => {
    const sd = owned[(offset + k * 2) % owned.length]
    return { tmdbId: (tv ? 9500 : 9000) + sd.id, title: sd.title, year: sd.year, overview: OV, posterUrl: sd.noPoster ? undefined : poster(sd.id), genres: sd.genres, rating: sd.rating, mediaType: (tv ? 'tv' : 'movie') as 'tv' | 'movie' }
  })
  return [mine[0], ...fresh.slice(0, 5), mine[1], ...fresh.slice(5)]
}

const calendar: CalendarEntry[] = [
  { kind: 'episode', id: 1, seriesId: 102, title: 'Salt & Static', subtitle: 'S03E09 · Second Service', releaseDate: day(1), status: 'missing' },
  { kind: 'episode', id: 2, seriesId: 104, title: 'Ember Road', subtitle: 'S02E05 · The Long Fuse', releaseDate: day(2), status: 'missing' },
  { kind: 'movie', id: 3, movieId: 10, title: 'Tidewalker', releaseDate: day(3), status: 'downloading' },
  { kind: 'episode', id: 4, seriesId: 101, title: 'The Clean Floor', subtitle: 'S03E01 · Season premiere', releaseDate: day(5), status: 'missing' },
  { kind: 'movie', id: 5, movieId: 9, title: 'Night Warden', releaseDate: day(6), status: 'missing' },
  { kind: 'episode', id: 6, seriesId: 105, title: 'Paper Ghosts', subtitle: 'S04E02 · Cold Desk', releaseDate: day(8), status: 'missing' },
  { kind: 'episode', id: 7, seriesId: 106, title: 'Glass Orchard', subtitle: 'S03E01 · New chapter', releaseDate: day(12), status: 'missing' },
  { kind: 'movie', id: 8, movieId: 8, title: 'The Quiet Signal', releaseDate: day(-2), status: 'downloaded' },
  { kind: 'episode', id: 9, seriesId: 103, title: 'The Cartographer’s Daughter', subtitle: 'S02E01 · Return', releaseDate: day(-4), status: 'downloaded' },
]

const wantedMissing: WantedItem[] = [
  { kind: 'movie', id: 8, movieId: 8, tmdbId: 9008, title: 'The Quiet Signal', date: day(-400), profileName: '1080p' },
  { kind: 'movie', id: 9, movieId: 9, tmdbId: 9009, title: 'Night Warden', date: day(-30), profileName: '4K & over' },
  { kind: 'episode', id: 41, seriesId: 104, title: 'Ember Road', subtitle: 'S02E04 · Harvest', date: day(-6), profileName: '1080p' },
  { kind: 'episode', id: 42, seriesId: 104, title: 'Ember Road', subtitle: 'S02E03 · Welcome', date: day(-13), profileName: '1080p' },
]
const wantedCutoff: WantedItem[] = [
  { kind: 'movie', id: 4, movieId: 4, tmdbId: 9004, title: 'Neon Meridian', quality: '1080p WEB-DL', cutoff: '1080p Bluray', profileName: '1080p' },
  { kind: 'movie', id: 5, movieId: 5, tmdbId: 9005, title: 'Iron Dust', quality: '720p', cutoff: '1080p Bluray', profileName: '1080p' },
]

function dashboard(real: DashboardData | null): DashboardData {
  const down = movies.filter((m) => m.status === 'downloaded').length
  const recent = (arr: Movie[]) =>
    arr.map((m, i) => ({ kind: 'movie' as const, id: m.id, tmdbId: m.tmdbId, title: m.title, year: m.year, posterUrl: m.posterUrl, quality: m.quality, sizeBytes: 9e9 + i * 1e9, at: ago(30 + i * 90) }))
  return {
    library: {
      movies: { total: movies.length, downloaded: down, missing: 3, downloading: 2 },
      series: { total: series.length, episodes: 129, episodesDownloaded: 79, episodesMissing: 50 },
      wanted: { movies: 3, episodes: 12 },
      sizeBytes: 1.42e12,
      qualities: [
        { tier: '1080p WEB-DL', count: 34 },
        { tier: '2160p WEB-DL', count: 9 },
        { tier: '1080p Bluray', count: 21 },
        { tier: '720p', count: 6 },
      ],
    },
    folders: [
      { key: 'movies', label: 'Movies', path: '/movies', exists: true, writable: true, freeBytes: 3.1e12, totalBytes: 8e12, mounted: true, mountKnown: true, warnings: [], libraryBytes: 0.95e12, items: movies.length, itemsLabel: 'movies', hardlinks: true },
      { key: 'tv', label: 'TV shows', path: '/tv', exists: true, writable: true, freeBytes: 3.1e12, totalBytes: 8e12, mounted: true, mountKnown: true, warnings: [], libraryBytes: 0.47e12, items: 79, itemsLabel: 'episodes', hardlinks: true },
      { key: 'downloads', label: 'Downloads', path: '/downloads', exists: true, writable: true, freeBytes: 3.1e12, totalBytes: 8e12, mounted: true, mountKnown: true, warnings: [], libraryBytes: 0, items: 3, itemsLabel: 'active' },
    ],
    active: [],
    recentlyAdded: [...movies.slice(0, 6).map((m, i) => ({ kind: 'movie' as const, id: m.id, tmdbId: m.tmdbId, title: m.title, year: m.year, posterUrl: m.posterUrl, at: ago(20 + i * 50) })), ...series.slice(0, 3).map((s, i) => ({ kind: 'series' as const, id: s.id, seriesId: s.id, title: s.title, year: s.year, posterUrl: s.posterUrl, at: ago(400 + i * 60) }))],
    recentDownloads: [
      ...recent(movies.filter((m) => m.status === 'downloaded').slice(0, 3)),
      { kind: 'series', id: 103, seriesId: 103, title: 'The Cartographer’s Daughter', subtitle: 'S01E10', posterUrl: poster(103), quality: '1080p WEB', sizeBytes: 3.1e9, at: ago(2970) },
    ],
    upcoming: calendar.filter((c) => c.releaseDate >= day(0)).slice(0, 5),
    // The demo shows a set-up install: the real "not set up yet" warnings would hide the sample content.
    health: (real?.health ?? []).filter((h) => h.level === 'info'),
    setup: { indexers: 2, usenetServers: 1, vpnConnected: true, torrentsReady: true, mediaServers: 1 },
  }
}

function seriesDetail(id: number): SeriesDetail | null {
  const s = series.find((x) => x.id === id)
  const seed = SERIES_SEEDS.find((x) => x.id === id)
  if (!s || !seed) return null
  const eps: Episode[] = Array.from({ length: Math.min(s.episodeCount, 20) }, (_, i) => ({
    id: id * 100 + i,
    season: Math.floor(i / 10) + 1,
    episode: (i % 10) + 1,
    title: `Episode ${i + 1}`,
    overview: OV,
    airDate: day(-300 + i * 7),
    status: i < seed.have ? 'downloaded' : 'missing',
    quality: i < seed.have ? '1080p WEB-DL' : undefined,
    monitored: true,
  }))
  return { ...s, episodes: eps }
}

const titleDetail = (m: Movie): MovieDetail => {
  const seed = MOVIE_SEEDS.find((x) => x.id === m.id)!
  return {
    tmdbId: m.tmdbId,
    title: m.title,
    year: m.year,
    overview: m.overview,
    posterUrl: m.posterUrl,
    libraryId: m.id,
    status: m.status,
    quality: m.quality,
    filePath: m.filePath,
    genres: seed.genres,
    rating: seed.rating,
    voteCount: 12840,
    runtime: 148,
    tagline: 'A sample tagline for demo mode.',
    certification: 'PG-13',
    releaseDate: `${m.year}-03-01`,
    language: 'en',
    cast: [
      { name: 'Actor One', character: 'Lead' },
      { name: 'Actor Two', character: 'Support' },
      { name: 'Actor Three', character: 'Villain' },
      { name: 'Actor Four', character: 'Friend' },
    ],
    trailers: [{ name: 'Official Trailer', site: 'YouTube', key: 'demo', url: 'https://www.youtube.com/results?search_query=' + encodeURIComponent(m.title + ' official trailer'), official: true }],
  } as MovieDetail
}

const tvDetail = (tmdbId: number): TVDetail | null => {
  const seed = SERIES_SEEDS.find((x) => 9500 + x.id === tmdbId)
  if (!seed) return null
  return {
    tmdbId,
    title: seed.title,
    year: seed.year,
    overview: seed.overview,
    posterUrl: poster(seed.id),
    libraryId: seed.id,
    genres: seed.genres,
    rating: seed.rating,
    voteCount: 5320,
    tagline: 'A sample tagline for demo mode.',
    contentRating: 'TV-MA',
    firstAirDate: `${seed.year}-02-18`,
    seasons: Math.ceil(seed.episodes / 10),
    episodes: seed.episodes,
    networks: ['Sample Network'],
    releaseStatus: 'Returning Series',
    cast: [{ name: 'Actor One', character: 'Lead' }, { name: 'Actor Two', character: 'Support' }],
    trailers: [{ name: 'Official Trailer', site: 'YouTube', key: 'demo', url: 'https://www.youtube.com/results?search_query=' + encodeURIComponent(seed.title + ' official trailer'), official: true }],
  }
}

function titleSearch(q: string): TitleResult[] {
  const needle = q.toLowerCase()
  const out: TitleResult[] = []
  for (const m of movies) {
    if (m.title.toLowerCase().includes(needle)) out.push({ kind: 'movie', tmdbId: m.tmdbId, title: m.title, year: m.year, overview: m.overview, posterUrl: m.posterUrl, inLibrary: true, libraryId: m.id, status: m.status })
  }
  for (const s of series) {
    if (s.title.toLowerCase().includes(needle)) out.push({ kind: 'tv', tmdbId: s.tmdbId, title: s.title, year: s.year, overview: s.overview, posterUrl: s.posterUrl, inLibrary: true, libraryId: s.id, status: s.downloadedCount === s.episodeCount ? 'downloaded' : 'missing' })
  }
  out.push(
    { kind: 'movie', tmdbId: 8801, title: `${q} (new result)`, year: 2021, overview: OV, posterUrl: poster(31), inLibrary: false },
    { kind: 'tv', tmdbId: 8802, title: `${q}: The Series`, year: 2020, overview: OV, posterUrl: poster(32), inLibrary: false },
  )
  return out
}

// Installs the demo layer over fetch. Anything it does not know is passed
// through untouched (login, settings, etc. stay real).
export function installDemo() {
  const real = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.pathname + input.search : input.url
    const method = (init?.method ?? 'GET').toUpperCase()
    const u = new URL(url, window.location.origin)
    const p = u.pathname
    if (!p.startsWith('/api/')) return real(input, init)
    const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })

    if (method !== 'GET') {
      // Pretend common actions worked so buttons feel alive; nothing is saved.
      // Only library actions are faked. Everything else (profile, settings,
      // notifications, sign-in) is real, so saving those is never swallowed.
      if (/^\/api\/(books|book-authors)/.test(p)) return json({ grabbed: 0, message: 'Demo mode: nothing was changed.', tags: [] })
      if (p === '/api/library/scan') {
        demoScanTV = String(init?.body ?? '').includes('"tv"')
        return json({ jobId: 'demo' })
      }
      if (/^\/api\/(movies|series|queue|blocklist|library)/.test(p)) {
        if (p === '/api/movies' || p === '/api/series') return json({ id: 999, tmdbId: 0, title: 'Demo', year: 2024, status: 'missing' }, 201)
        return json({ grabbed: 0, message: 'Demo mode: nothing was changed.', queueId: 1, removed: 0 })
      }
      return real(input, init)
    }

    switch (true) {
      // A sample title's history and files: there are none on this server.
      case /^\/api\/(movies|series)\/\d+\/events$/.test(p):
        return json([])
      case /^\/api\/(movies|series)\/\d+\/files$/.test(p):
        return json({ folder: '', files: [] })
      case p === '/api/movies':
        return json(movies)
      case /^\/api\/movies\/\d+$/.test(p):
        return json(movies.find((m) => m.id === Number(p.split('/').pop())) ?? movies[0])
      case p === '/api/series':
        return json(series)
      case /^\/api\/series\/\d+$/.test(p): {
        const d = seriesDetail(Number(p.split('/').pop()))
        return d ? json(d) : json({ error: 'not found' }, 404)
      }
      case p === '/api/queue':
        return json(queue)
      case p === '/api/activity':
        return json(activity)
      case p === '/api/blocklist':
        return json([{ id: 1, releaseTitle: 'The.Quiet.Signal.2016.720p.HDTV-JUNK', protocol: 'usenet', reason: 'Bad release: archive is corrupt', createdAt: ago(2100) }])
      case p === '/api/subtitles/wanted':
        return json([
          { kind: 'movie', id: 2, tmdbId: 9002, title: 'The Long Equation', missing: ['en'] },
          { kind: 'movie', id: 3, tmdbId: 9003, title: 'The Marigold Lodge', missing: ['en', 'fr'] },
          { kind: 'movie', id: 4, tmdbId: 9004, title: 'Neon Meridian', missing: ['en'] },
          { kind: 'episode', id: 201, seriesId: 101, title: 'The Clean Floor', subtitle: 'S02E03 · Floor Nine', missing: ['en'] },
          { kind: 'episode', id: 202, seriesId: 101, title: 'The Clean Floor', subtitle: 'S02E04 · Hollow', missing: ['en'] },
        ])
      case p === '/api/subtitles/quota':
        return json({ hasKey: true, hasAccount: false, limit: 5, used: 3, remaining: 2, windowHours: 24, resetsAt: new Date(Date.now() + 6 * 3600e3).toISOString(), source: 'estimated', missingItems: 5, missingFiles: 6, daysToFinish: 2, exceeded: false, message: 'You have 6 subtitles to get but 2 downloads left today. At 5 a day that takes 2 days. A free OpenSubtitles account raises it to about 20 a day.' })
      case p === '/api/calendar':
        return json(calendar)
      case p === '/api/wanted':
        return json(u.searchParams.get('kind') === 'cutoff' ? wantedCutoff : wantedMissing)
      case p === '/api/dashboard': {
        let realData: DashboardData | null = null
        try {
          realData = (await (await real(input, init)).json()) as DashboardData
        } catch {
          realData = null
        }
        return json({ ...dashboard(realData), library: { ...dashboard(realData).library, ...demoBookCounts, music: demoMusicCounts() } })
      }
      case p === '/api/books':
        return json(demoBooks)
      case p === '/api/books/progress':
        return json(demoProgress)
      case p === '/api/books/search':
        return json(demoBookSearch(u.searchParams.get('q') ?? ''))
      case p === '/api/books/discover': {
        const list = u.searchParams.get('list') === 'trending' ? (u.searchParams.get('period') === 'yearly' ? 1 : 0) : (u.searchParams.get('subject') ?? '').length + 2
        return json(demoBookList(list, Number(u.searchParams.get('page') ?? 1)))
      }
      case p === '/api/book-authors':
        return json([])
      case /^\/api\/book-authors\/[^/]+\/works$/.test(p):
        return json(demoAuthorWorks(decodeURIComponent(p.split('/')[3])))
      case /^\/api\/book-works\/[^/]+$/.test(p): {
        const w = demoWork(decodeURIComponent(p.split('/').pop() ?? ''))
        return w ? json(w) : json({ error: 'not found' }, 404)
      }
      case /^\/api\/books\/\d+$/.test(p):
        return json(demoBooks.find((b) => b.id === Number(p.split('/').pop())) ?? demoBooks[0])
      case /^\/api\/books\/\d+\/progress$/.test(p): {
        const id = Number(p.split('/')[3])
        const f = u.searchParams.get('format')
        return json(demoProgress.find((x) => x.bookId === id && x.format === f) ?? { bookId: id, format: f, position: '', percent: 0, finished: false })
      }
      case /^\/api\/books\/\d+\/tracks$/.test(p):
        return json({ tracks: demoTracks, format: 'm4b' })
      case /^\/api\/books\/\d+\/links$/.test(p):
        return json([])
      case /^\/api\/books\/\d+\/read$/.test(p):
        // A short public-domain sample, so the reader has something to show.
        return real('/demo/sample-book.epub')
      case p === '/api/discover/trending':
      case p === '/api/discover/for-you':
        return json(discoverPool(0, false))
      case p === '/api/discover/list' && u.searchParams.get('list') === 'similar': {
        const tv = u.searchParams.get('kind') === 'tv'
        const results = Number(u.searchParams.get('page') ?? 1) > 1 ? [] : discoverPool(1, tv)
        return json({ page: 1, totalPages: 1, totalResults: results.length, results })
      }
      case p === '/api/discover/popular':
        return json(discoverPool(3, false))
      case p === '/api/discover/tv/trending':
        return json(discoverPool(0, true))
      case p === '/api/discover/tv/popular':
        return json(discoverPool(2, true))
      case p === '/api/discover/search':
        return json(titleSearch(u.searchParams.get('q') ?? ''))
      case /^\/api\/tmdb\/movies\/\d+$/.test(p): {
        const tmdb = Number(p.split('/').pop())
        const m = movies.find((x) => x.tmdbId === tmdb)
        if (m) return json(titleDetail(m))
        return json({ tmdbId: tmdb, title: 'Sample Movie', year: 2021, overview: OV, posterUrl: poster(31), genres: ['Drama'], rating: 7.4, runtime: 112, certification: 'R', trailers: [], cast: [] })
      }
      case /^\/api\/tmdb\/tv\/\d+$/.test(p): {
        const d = tvDetail(Number(p.split('/').pop()))
        return d ? json(d) : json({ tmdbId: 8802, title: 'Sample Series', year: 2020, overview: OV, posterUrl: poster(32), genres: ['Drama'], rating: 7.9 })
      }
      case /^\/api\/tmdb\/movies\/\d+\/similar$/.test(p):
        return json(discoverPool(4, false).slice(0, 8))
      // Every other Discover list and filter: made-up titles, never the real
      // server's (screenshots must not show real films, shows or artists).
      case p === '/api/discover/list':
      case p === '/api/discover/browse': {
        const tv = u.searchParams.get('kind') === 'tv'
        const list = u.searchParams.get('list') ?? u.searchParams.get('sort') ?? ''
        const offset = { trending: 0, popular: 3, upcoming: 2, top_rated: 4, now_playing: 1 }[list as 'trending'] ?? list.length % 5
        const results = Number(u.searchParams.get('page') ?? 1) > 1 ? [] : discoverPool(offset, tv)
        return json({ page: 1, totalPages: 1, totalResults: results.length, results })
      }
      case p === '/api/discover/import-list':
        return json(discoverPool(1, false))
      case p === '/api/music/discover':
        return json({ page: 1, totalPages: 1, items: DEMO_RELEASES })
      case p === '/api/music/discover/artists':
        return json({ page: 1, totalPages: 1, items: DEMO_ARTISTS.map((a, i) => ({ mbid: `demo-artist-${i}`, name: a, listenCount: 90000 - i * 7300, inLibrary: i === 1 })) })
      case p === '/api/music/artists':
        return json(DEMO_ARTISTS.slice(0, 6).map((_, i) => demoArtist(i)))
      case /^\/api\/music\/artists\/\d+$/.test(p):
        return json(demoArtist(Number(p.split('/').pop()) - 1, true))
      case p === '/api/library/scan/demo':
        return json(demoScan(demoScanTV))
      case p === '/api/library/import/active':
        return json({ jobs: [], batches: [] })
      case /^\/api\/library\/import\/batches\/\d+$/.test(p):
        return json(demoBatch)
      case p === '/api/music/search':
        return json([])
      default:
        return real(input, init)
    }
  }
}

// Made-up artists and releases for the demo's music Discover.
const DEMO_ARTISTS = ['Marrow & Lane', 'Mira Calloway', 'Northern Static Club', 'Velvet Harbour Choir', 'Jonah Reyes Trio', 'Orchard Signal', 'Tamsin Vale', 'The Quiet Ferries']
const DEMO_RELEASES = DEMO_ARTISTS.flatMap((artist, i) => [
  { mbid: `demo-rel-${i}-a`, title: ['Night Ferry', 'Copper Lines', 'Slow Weather', 'Harbour Lights', 'Blue Hours', 'Fieldwork', 'Long Light', 'Undertow'][i], type: 'album', artistName: artist, artistMbid: `demo-artist-${i}`, releaseDate: `2026-0${(i % 9) + 1}-1${i % 9}`, genres: [['indie', 'folk', 'electronic', 'soul', 'jazz', 'rock', 'pop', 'ambient'][i]], inLibrary: i === 1 },
])

// The music card on the dashboard, counted from the made-up artists.
function demoMusicCounts() {
  const artists = DEMO_ARTISTS.slice(0, 6).map((_, i) => demoArtist(i, true))
  const albums = artists.flatMap((a) => a.albums ?? [])
  return { artists: artists.length, albums: albums.length, downloaded: albums.filter((a) => a.status === 'downloaded').length, missing: albums.filter((a) => a.status !== 'downloaded' && a.monitored).length }
}

// A made-up music library: a few of the artists above with their albums.
function demoArtist(i: number, withAlbums = false) {
  const k = ((i % 6) + 6) % 6
  const albums = [0, 1, 2, 3].slice(0, 2 + (k % 3)).map((j) => {
    const n = (k * 4 + j) % 8
    const status = j === 0 || (j === 1 && k % 2 === 0) ? 'downloaded' : j === 3 ? 'missing' : 'wanted'
    return { id: k * 10 + j + 1, artistId: k + 1, artistName: DEMO_ARTISTS[k], mbid: `demo-album-${k}-${j}`, title: ['Night Ferry', 'Copper Lines', 'Slow Weather', 'Harbour Lights', 'Blue Hours', 'Fieldwork', 'Long Light', 'Undertow'][n], type: j === 2 ? 'ep' : 'album', releaseDate: `${2016 + k + j}-0${j + 3}-12`, year: 2016 + k + j, monitored: status !== 'missing', status, quality: status === 'downloaded' ? 'FLAC' : undefined, coverUrl: poster(200 + k * 10 + j) }
  })
  const downloaded = albums.filter((a) => a.status === 'downloaded').length
  return {
    id: k + 1,
    mbid: `demo-artist-${k}`,
    name: DEMO_ARTISTS[k],
    sortName: DEMO_ARTISTS[k],
    imageUrl: poster(300 + k),
    coverUrl: poster(300 + k),
    genres: [['indie', 'folk', 'electronic', 'soul', 'jazz', 'rock'][k]],
    monitored: k !== 4,
    monitorNew: k !== 4,
    profileId: 1,
    profileName: 'Lossless',
    addedAt: new Date(Date.now() - (k + 2) * 86400e3 * 9).toISOString(),
    albumCount: albums.length,
    monitoredCount: albums.filter((a) => a.monitored).length,
    downloadedCount: downloaded,
    albums: withAlbums ? albums : undefined,
  }
}

let demoScanTV = false

// A made-up library import: what a scan of a movie or TV folder found.
function demoScan(tv: boolean) {
  const names = tv
    ? [['Salt & Static', 2021, [1, 2, 3]], ['The Cartographer’s Daughter', 2024, [1]], ['Harbour Street', 2019, [1, 2]], ['Quiet Hours', 2022, [1, 2]]]
    : [['Northbound', 2014], ['Tidewalker', 2023], ['The Long Equation', 2023], ['All the Doors at Once', 2022], ['Paper Moons', 2018], ['Lantern Field', 2016]]
  const items = names.map((n, i) => {
    const [title, year, seasons] = n as [string, number, number[] | undefined]
    const folder = tv ? `/data/TV/${title} (${year})` : `/data/Movies/${title} (${year})`
    const match = i === 4 ? 'ambiguous' : i === 5 ? 'unmatched' : 'matched'
    const candidates = match === 'unmatched' ? [] : match === 'ambiguous' ? [{ tmdbId: 9400 + i, title, year, posterUrl: poster(40 + i) }, { tmdbId: 9450 + i, title, year: year - 31, posterUrl: poster(60 + i) }] : [{ tmdbId: 9400 + i, title, year, posterUrl: poster(40 + i) }]
    return { key: `demo-${i}`, title, year, fileCount: tv ? (seasons?.length ?? 1) * 8 : 1, sizeBytes: (tv ? 18e9 : 9e9) + i * 1.3e9, samplePath: tv ? `${folder}/Season 01/${title} - S01E01.mkv` : `${folder}/${title} (${year}).mkv`, seasons, quality: i % 3 === 0 ? 'Bluray-1080p' : i % 3 === 1 ? 'WEBDL-2160p' : 'WEBDL-1080p', match, candidates, inLibrary: false }
  })
  return { id: 'demo', kind: tv ? 'tv' : 'movie', root: tv ? '/data/TV' : '/data/Movies', phase: 'ready', done: items.length, total: items.length, items, skipped: [] }
}

const demoBatch = {
  id: 1,
  kind: 'tv',
  root: '/data/TV',
  createdAt: new Date(Date.now() - 4 * 60e3).toISOString(),
  finishedAt: new Date(Date.now() - 2 * 60e3).toISOString(),
  elapsedMs: 118000,
  running: false,
  dismissed: false,
  monitor: false,
  noUpgrade: true,
  monitorMissing: false,
  total: 3,
  done: 3,
  added: 3,
  already: 0,
  problems: 0,
  items: [
    { title: 'Salt & Static', kind: 'series', outcome: 'added', state: 'done', imported: 24, skipped: 0 },
    { title: 'Harbour Street', kind: 'series', outcome: 'added', state: 'done', imported: 16, skipped: 0 },
    { title: 'Quiet Hours', kind: 'series', outcome: 'added', state: 'done', imported: 15, skipped: 0 },
  ],
}
