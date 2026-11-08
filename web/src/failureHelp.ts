// What a failed download's message means, and what to try, in plain words.
// No React here, so it can be tested on its own.
import { t } from './i18n.ts'

export interface FailureHelp {
  why: string
  tips: string[]
}

const has = (msg: string, ...words: string[]) => words.some((w) => msg.includes(w))

export function failureHelp(error: string | undefined, protocol: 'usenet' | 'torrent' | 'soulseek' | undefined): FailureHelp | null {
  const msg = (error ?? '').toLowerCase()
  if (!msg) return null
  if (has(msg, "couldn't be found on your usenet servers", 'articles were not found', 'missing articles')) {
    return {
      why: t('This release is no longer complete on your Usenet provider. Older posts lose parts over time, or are taken down, and this one has no repair files left to fill the gaps.'),
      tips: [
        t('Press Blocklist & search again: Mediarium skips this release and picks the next best one. With automatic searching on, it does this by itself.'),
        t('If no other release is found, the quality profile may be too narrow. Open the title and choose a profile that accepts more qualities (for example 720p as well as 1080p), so there are more releases to pick from.'),
        t('If this happens often, add a second Usenet provider from a different company (a "backup" in Settings > Downloading > Usenet). It often still has the missing parts.'),
        t('If torrents are switched on, the title can also be found that way.'),
      ],
    }
  }
  if (has(msg, 'par2', 'repair')) {
    return {
      why: t('The download arrived damaged and could not be repaired.'),
      tips: [t('Press Blocklist & search again to try another release.'), t('A second Usenet provider from a different company often helps with damaged posts.')],
    }
  }
  if (has(msg, 'password that came with it is wrong')) {
    return { why: t('The release came with a password, but it does not open the archive.'), tips: [t('Press Blocklist & search again: this release is skipped from now on.')] }
  }
  if (has(msg, 'password')) {
    return { why: t('The release is locked with a password, which usually means it is not what it says.'), tips: [t('Press Blocklist & search again: this release is skipped from now on.')] }
  }
  if (has(msg, 'no video', 'could not be matched', "couldn't find the movie file", 'sample')) {
    return { why: t('The download did not contain a usable video file for this title (it may be fake, a sample or something else).'), tips: [t('Press Blocklist & search again to try another release.')] }
  }
  if (has(msg, 'unpack', 'archive', 'extract')) {
    return { why: t('The download could not be unpacked. The archive may be broken or incomplete.'), tips: [t('Press Blocklist & search again to try another release.'), t('If it keeps happening, check there is free space in the downloads folder.')] }
  }
  if (has(msg, 'stalled', 'no one seems to be sharing', 'no data')) {
    return {
      why: t('Nobody is sharing this torrent any more, so it stopped getting data.'),
      tips: [t('Press Blocklist & search again to try another release.'), t('Releases with more seeders finish more reliably. With a Usenet provider set up, Usenet releases are preferred when they are equally good.')],
    }
  }
  if (has(msg, 'disk is full', 'no space', 'not enough space')) {
    return { why: t('The disk ran out of space.'), tips: [t('Free some space, then press Retry.'), t('Settings > Downloading > Download rules can hold new downloads back while space is low.')] }
  }
  if (has(msg, 'login', 'password was refused', 'authentication', '401', '403', 'account')) {
    return { why: t('Your provider or indexer refused the login, or the account has a problem.'), tips: [t('Check the login under Settings (Downloading or Indexers & Search) and press Test.'), t('Then press Retry.')] }
  }
  if (has(msg, 'nzb file', 'getnzb', 'indexer')) {
    return { why: t('The indexer did not hand over the download file.'), tips: [t('Press Retry in a few minutes: indexers are sometimes busy or limit downloads per day.'), t('Or press Blocklist & search again to take a release from another indexer.')] }
  }
  if (has(msg, 'already exist')) {
    return { why: t('A file for this title is already in your library, so nothing was imported.'), tips: [t('Nothing to fix if you are happy with the file you have. To replace it, change "If a file already exists" in Settings > Library > File names and importing.')] }
  }
  return {
    why: protocol === 'torrent' ? t('The torrent could not be finished.') : t('The download could not be finished.'),
    tips: [t('Press Retry to try the same release again, or Blocklist & search again to try another.'), t('Settings > System > Logs and errors has the details.')],
  }
}
