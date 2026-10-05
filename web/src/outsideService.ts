// Which outside service a read waits on, if any. Those reads can be slow for
// reasons that have nothing to do with Mediarium (TMDB, Open Library or an
// indexer taking its time), so they don't raise the "Mediarium is slow"
// banner, and a timeout names the service instead.
//
// No React here, so it can be tested on its own.

const RULES: [RegExp, string][] = [
  [/^\/discover\/import-list/, 'Trakt'],
  [/^\/(discover|tmdb|tv\/search)(\/|$|\?)/, 'The movie database (TMDB)'],
  [/^\/movies\/\d+\/similar/, 'The movie database (TMDB)'],
  [/^\/music\/(discover|search|covers)/, 'MusicBrainz'],
  [/^\/music\/(albums|artists)\/\d+\/cover/, 'The Cover Art Archive'],
  [/^\/(books\/(discover|search)|book-authors)(\/|$|\?)/, 'Open Library'],
  [/^\/(search|movies\/\d+\/search|series\/\d+\/search|books\/\d+\/releases)(\/|$|\?)/, 'Your indexers'],
  [/^\/(media-servers\/links|books\/\d+\/links)/, 'Your media server'],
  [/^\/(movies|episodes)\/\d+\/subtitles(\/|$|\?)/, 'OpenSubtitles'],
]

// outsideService returns the name of the service a read of path (without the
// /api prefix) waits on, or '' when Mediarium answers by itself.
export function outsideService(path: string): string {
  for (const [re, name] of RULES) if (re.test(path)) return name
  return ''
}

export function slowServiceMessage(service: string): string {
  return `${service} is slow to answer right now. This is on their side, not Mediarium's. Try again in a moment.`
}
