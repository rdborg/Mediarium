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

// Every preset starts at 10 connections. Plans allow more (often 20 to 50), but a login
// that opens more than its plan is refused, so 10 is the safe place to start. Raise it
// once you know your plan.
export const USENET_PROVIDERS: UsenetProviderPreset[] = [
  { name: 'Astraweb', host: 'news.astraweb.com', port: 563, useSsl: true, connections: 10 },
  { name: 'Blocknews (US)', host: 'usnews.blocknews.net', port: 563, useSsl: true, connections: 10 },
  { name: 'Easynews', host: 'news.easynews.com', port: 563, useSsl: true, connections: 10 },
  { name: 'Eweka', host: 'news.eweka.nl', port: 563, useSsl: true, connections: 10 },
  { name: 'Frugal Usenet', host: 'news.frugalusenet.com', port: 563, useSsl: true, connections: 10 },
  { name: 'Giganews', host: 'news.giganews.com', port: 563, useSsl: true, connections: 10 },
  { name: 'Newshosting', host: 'news.newshosting.com', port: 563, useSsl: true, connections: 10 },
  { name: 'NewsDemon', host: 'news.newsdemon.com', port: 563, useSsl: true, connections: 10 },
  { name: 'TweakNews', host: 'news.tweaknews.eu', port: 563, useSsl: true, connections: 10 },
  { name: 'Usenet.Farm', host: 'news.usenet.farm', port: 563, useSsl: true, connections: 10 },
  { name: 'UsenetServer', host: 'news.usenetserver.com', port: 563, useSsl: true, connections: 10 },
]
