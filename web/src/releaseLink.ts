// The link from a release in interactive search to its page on the indexer.
// The server only sends `infoUrl` when it is a plain page without a key in it
// (see indexers.SafeInfoURL); this is the second check before it becomes a link
// in the browser: http or https only, no name and password, nothing else.
export function releasePageUrl(r: { infoUrl?: string; protocol?: string }): string | undefined {
  if (!r.infoUrl || r.protocol === 'soulseek') return undefined
  try {
    const u = new URL(r.infoUrl)
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return undefined
    if (u.username || u.password || !u.hostname) return undefined
    return u.href
  } catch {
    return undefined
  }
}
