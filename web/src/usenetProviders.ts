// Convenience presets for the news-server host of common Usenet providers.
// Purely a time-saver for filling in the form — the list is alphabetical,
// not an endorsement, and unrelated to any affiliate listing. Always check
// the exact host and port on your provider's own account page.
export interface UsenetProviderPreset {
  name: string
  host: string
  port: number
  useSsl: boolean
  connections: number
}

export const USENET_PROVIDERS: UsenetProviderPreset[] = [
  { name: 'Astraweb', host: 'news.astraweb.com', port: 563, useSsl: true, connections: 20 },
  { name: 'Blocknews (US)', host: 'usnews.blocknews.net', port: 563, useSsl: true, connections: 20 },
  { name: 'Easynews', host: 'news.easynews.com', port: 563, useSsl: true, connections: 20 },
  { name: 'Eweka', host: 'news.eweka.nl', port: 563, useSsl: true, connections: 50 },
  { name: 'Frugal Usenet', host: 'news.frugalusenet.com', port: 563, useSsl: true, connections: 40 },
  { name: 'Giganews', host: 'news.giganews.com', port: 563, useSsl: true, connections: 30 },
  { name: 'Newshosting', host: 'news.newshosting.com', port: 563, useSsl: true, connections: 30 },
  { name: 'NewsDemon', host: 'news.newsdemon.com', port: 563, useSsl: true, connections: 50 },
  { name: 'TweakNews', host: 'news.tweaknews.eu', port: 563, useSsl: true, connections: 30 },
  { name: 'Usenet.Farm', host: 'news.usenet.farm', port: 563, useSsl: true, connections: 30 },
  { name: 'UsenetServer', host: 'news.usenetserver.com', port: 563, useSsl: true, connections: 20 },
]
