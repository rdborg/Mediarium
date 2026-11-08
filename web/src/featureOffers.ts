// "Switch this on?" offers (V4 A7): new things that start off, offered to an
// administrator after the update that brings them (in the What's new window)
// and listed under Settings > Media types, with Switch on, Not now and Never.
// Every one is also in the setup wizard, so a new install gets the same choice.
// The server remembers the answers (GET/PUT /api/feature-offers).
//
// No React in this file, so it can be tested on its own.
import type { ModuleKey } from './api.ts'
import { t } from './i18n.ts'
import { compareVersions } from './whatsNew.ts'

export type OfferId = 'anime' | 'udp-dht' | 'thetvdb' | 'torrent-route' | 'rss-lists' | 'prowlarr' | 'omdb' | 'soulseek' | 'lifecycle' | 'archiver'

export interface Offer {
  id: OfferId
  since: string // the version that brings it
  title: string
  text: string
  // module: Switch on turns this module on. setting: it sets this setting.
  // open: it needs more than a switch (a key, a choice per indexer), so the
  // button opens the page where it is set up.
  action: { kind: 'module'; module: ModuleKey } | { kind: 'setting'; field: 'discoverThroughTunnel' } | { kind: 'open'; to: string }
  // Where the setup wizard offers it.
  wizard: string
}

export const OFFERS: Offer[] = [
  {
    id: 'anime',
    since: '4.0.0',
    title: t('The anime library'),
    text: t('Anime shows get their own tab in the Library, their own folder, quality profile and file names.'),
    action: { kind: 'module', module: 'anime' },
    wizard: 'Media types',
  },
  {
    id: 'lifecycle',
    since: '4.0.0',
    title: t('Library lifecycle rules'),
    text: t('Titles nobody watches wait on a Leaving soon shelf first. Nothing goes until you have seen a dry run.'),
    action: { kind: 'module', module: 'lifecycle' },
    wizard: 'Media types',
  },
  {
    id: 'archiver',
    since: '4.0.0',
    title: t('Channel archiver'),
    text: t('Follow YouTube channels and keep their videos or just the audio, with a yt-dlp you supply yourself.'),
    action: { kind: 'module', module: 'archiver' },
    wizard: 'Media types',
  },
  {
    id: 'thetvdb',
    since: '4.0.0',
    title: t('TheTVDB as a second source for shows'),
    text: t('Fills in seasons and episodes a show is missing on TMDB, with your own TheTVDB key.'),
    action: { kind: 'open', to: '/settings/metadata' },
    wizard: 'Connect services',
  },
  {
    id: 'torrent-route',
    since: '4.0.0',
    title: t('Where each indexer sends its torrents'),
    text: t('A private tracker can seed from your download client while public indexers use debrid. Set it on each indexer card.'),
    action: { kind: 'open', to: '/settings/indexers' },
    wizard: '', // a choice on each indexer's card, once it is added
  },
  {
    id: 'rss-lists',
    since: '4.0.0',
    title: t('RSS feeds as import lists'),
    text: t('Add the titles in a feed, and download better releases from it for titles you have.'),
    action: { kind: 'open', to: '/settings/lists' },
    wizard: '',
  },
  {
    id: 'prowlarr',
    since: '4.0.0',
    title: t('A link to your Prowlarr'),
    text: t("Every indexer your Prowlarr has, kept in step, also the sites Mediarium can't read itself."),
    action: { kind: 'open', to: '/settings/indexers' },
    wizard: '',
  },
  {
    id: 'soulseek',
    since: '4.0.0',
    title: t('Soulseek for music, through your slskd'),
    text: t('Albums can also come from Soulseek: Choose a release lists the folders users share, and slskd downloads them.'),
    action: { kind: 'open', to: '/settings/download-rules' },
    wizard: '',
  },
  {
    id: 'omdb',
    since: '4.0.0',
    title: t('Scores from IMDb, Rotten Tomatoes and Metacritic'),
    text: t('With a free OMDb key, title pages show those scores next to TMDB’s, and Discover cards the Rotten Tomatoes one.'),
    action: { kind: 'open', to: '/settings/metadata' },
    wizard: 'Connect services',
  },
  {
    id: 'udp-dht',
    since: '3.9.1',
    title: t('UDP trackers and DHT through the VPN'),
    text: t('Torrents that only list UDP trackers can start while the built-in VPN is on.'),
    action: { kind: 'setting', field: 'discoverThroughTunnel' },
    wizard: 'VPN',
  },
]

// The modules that were there before offers existed; every module after them
// needs an offer (a test checks this).
export const MODULES_BEFORE_OFFERS: readonly ModuleKey[] = ['movies', 'tv', 'music', 'ebooks', 'audiobooks']

export type OfferChoices = Partial<Record<string, string>> // id -> "never" | "later:<version>"

// The offers to show after an update from lastSeen to current: the ones that
// came in that span, are not on and were not answered with Never, or with Not
// now during this same version.
export function offersAfterUpdate(lastSeen: string | null, current: string, choices: OfferChoices, isOn: (id: OfferId) => boolean, offers: Offer[] = OFFERS): Offer[] {
  if (lastSeen === null) return [] // a new install had the wizard
  return offers.filter(
    (o) => compareVersions(o.since, lastSeen) > 0 && compareVersions(o.since, current) <= 0 && !isOn(o.id) && !answered(choices[o.id], current),
  )
}

// The offers listed under Settings: every one that is not on. Answers don't
// hide them there, so a Never can be taken back.
export function offersInSettings(current: string, isOn: (id: OfferId) => boolean, offers: Offer[] = OFFERS): Offer[] {
  return offers.filter((o) => compareVersions(o.since, current) <= 0 && !isOn(o.id))
}

function answered(choice: string | undefined, current: string): boolean {
  if (!choice) return false
  if (choice === 'never') return true
  return choice.startsWith('later:') && compareVersions(choice.slice(6), current) === 0
}
