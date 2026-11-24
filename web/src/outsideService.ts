// Which outside service a read waits on, if any. Those reads can be slow for
// reasons that have nothing to do with Mediarium (TMDB, Open Library or an
// indexer taking its time), so they don't raise the "Mediarium is slow"
// banner, and a timeout names the service instead.
//
// No React here, so it can be tested on its own.
import { t } from './i18n.ts'

const RULES: [RegExp, string][] = [
  [/^\/discover\/import-list/, 'Trakt'],
  [/^\/settings\/simkl\/pin/, 'Simkl'],
  [/^\/(discover|tmdb|tv\/search)(\/|$|\?)/, t('The movie database (TMDB)')],
  [/^\/movies\/\d+\/similar/, t('The movie database (TMDB)')],
  [/^\/music\/(discover|search|covers)/, 'MusicBrainz'],
  [/^\/music\/(albums|artists)\/\d+\/cover/, 'The Cover Art Archive'],
  [/^\/music\/tracks\/\d+\/lyrics/, 'LRCLIB'],
  [/^\/(books\/(discover|search)|book-authors)(\/|$|\?)/, 'Open Library'],
  [/^\/books\/\d+\/cover\/editions/, 'Open Library'],
  [/^\/(search|movies\/\d+\/search|series\/\d+\/search|books\/\d+\/releases)(\/|$|\?)/, t('Your indexers')],
  [/^\/(media-servers\/links|books\/\d+\/links)/, t('Your media server')],
  [/^\/(movies|episodes)\/\d+\/subtitles(\/|$|\?)/, 'OpenSubtitles'],
]

// outsideService returns the name of the service a read of path (without the
// /api prefix) waits on, or '' when Mediarium answers by itself.
export function outsideService(path: string): string {
  for (const [re, name] of RULES) if (re.test(path)) return name
  return ''
}

export function slowServiceMessage(service: string): string {
  if (service === t('Your indexers')) return t('The search is taking longer than usual because some of your indexers are slow. Try again in a moment.')
  return t("{service} is slow to answer right now. This is on their side, not Mediarium's. Try again in a moment.", { service })
}
