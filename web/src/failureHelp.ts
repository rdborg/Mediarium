// What a failed download's message means, and what to try, in plain words.
// No React here, so it can be tested on its own.

export interface FailureHelp {
  why: string
  tips: string[]
}

const has = (msg: string, ...words: string[]) => words.some((w) => msg.includes(w))

export function failureHelp(error: string | undefined, protocol: 'usenet' | 'torrent' | undefined): FailureHelp | null {
  const msg = (error ?? '').toLowerCase()
  if (!msg) return null
  if (has(msg, "couldn't be found on your usenet servers", 'articles were not found', 'missing articles')) {
    return {
      why: 'This release is no longer complete on your Usenet provider. Older posts lose parts over time, or are taken down, and this one has no repair files left to fill the gaps.',
      tips: [
        'Press Blocklist & search again: Mediarium skips this release and picks the next best one. With automatic searching on, it does this by itself.',
        'If no other release is found, the quality profile may be too narrow. Open the title and choose a profile that accepts more qualities (for example 720p as well as 1080p), so there are more releases to pick from.',
        'If this happens often, add a second Usenet provider from a different company (a "backup" in Settings > Downloading). It often still has the missing parts.',
        'If torrents are switched on, the title can also be found that way.',
      ],
    }
  }
  if (has(msg, 'par2', 'repair')) {
    return {
      why: 'The download arrived damaged and could not be repaired.',
      tips: ['Press Blocklist & search again to try another release.', 'A second Usenet provider from a different company often helps with damaged posts.'],
    }
  }
  if (has(msg, 'password')) {
    return { why: 'The release is locked with a password, which usually means it is not what it says.', tips: ['Press Blocklist & search again: this release is skipped from now on.'] }
  }
  if (has(msg, 'no video', 'could not be matched', "couldn't find the movie file", 'sample')) {
    return { why: 'The download did not contain a usable video file for this title (it may be fake, a sample or something else).', tips: ['Press Blocklist & search again to try another release.'] }
  }
  if (has(msg, 'unpack', 'archive', 'extract')) {
    return { why: 'The download could not be unpacked. The archive may be broken or incomplete.', tips: ['Press Blocklist & search again to try another release.', 'If it keeps happening, check there is free space in the downloads folder.'] }
  }
  if (has(msg, 'stalled', 'no one seems to be sharing', 'no data')) {
    return {
      why: 'Nobody is sharing this torrent any more, so it stopped getting data.',
      tips: ['Press Blocklist & search again to try another release.', 'Releases with more seeders finish more reliably. With a Usenet provider set up, Usenet releases are preferred when they are equally good.'],
    }
  }
  if (has(msg, 'disk is full', 'no space', 'not enough space')) {
    return { why: 'The disk ran out of space.', tips: ['Free some space, then press Retry.', 'Settings > Downloading > Speed and space can hold new downloads back while space is low.'] }
  }
  if (has(msg, 'login', 'password was refused', 'authentication', '401', '403', 'account')) {
    return { why: 'Your provider or indexer refused the login, or the account has a problem.', tips: ['Check the login under Settings (Downloading or Indexers & Search) and press Test.', 'Then press Retry.'] }
  }
  if (has(msg, 'nzb file', 'getnzb', 'indexer')) {
    return { why: 'The indexer did not hand over the download file.', tips: ['Press Retry in a few minutes: indexers are sometimes busy or limit downloads per day.', 'Or press Blocklist & search again to take a release from another indexer.'] }
  }
  if (has(msg, 'already exist')) {
    return { why: 'A file for this title is already in your library, so nothing was imported.', tips: ['Nothing to fix if you are happy with the file you have. To replace it, change "If a file already exists" in Settings > Library.'] }
  }
  return {
    why: protocol === 'torrent' ? 'The torrent could not be finished.' : 'The download could not be finished.',
    tips: ['Press Retry to try the same release again, or Blocklist & search again to try another.', 'Settings > System > Logs and errors has the details.'],
  }
}
