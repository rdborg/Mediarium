import { t } from './i18n.ts'

// The "Move from other apps" preview explains each line in English, in words
// the server writes. These are the common ones, put into the screen language;
// anything not listed here is shown as the server wrote it.
//
// A reason is one or more parts joined by "; ". Each part is matched from the
// start of what is left, so a part that holds a "; " itself still matches.

type Rule = [RegExp, (m: RegExpMatchArray) => string]

// Why an indexer stays with Prowlarr: the first half of its sentence.
function why(text: string): string {
  let m = text.match(/^Mediarium's site list has no "(.*)"$/)
  if (m) return t('Mediarium’s site list has no “{id}”', { id: m[1] })
  m = text.match(/^it is an unsupported type \((.*)\)$/)
  if (m) return t('it is an unsupported type ({type})', { type: m[1] })
  if (text === 'the definition lists no address') return t('the definition lists no address')
  m = text.match(/^Mediarium can't run the (.*) definition yet \((.*)\)$/)
  if (m) return t('Mediarium can’t run the {name} definition yet ({problem})', { name: m[1], problem: m[2] })
  return text
}

const RULES: Rule[] = [
  [/^no TMDB id: TMDB has no show with TVDB id (\d+)/, (m) => t('no TMDB id: TMDB has no show with TVDB id {id}', { id: m[1] })],
  [/^no TMDB id/, () => t('no TMDB id')],
  [/^already in your library; the file in its folder will be linked/, () => t('already in your library; the file in its folder will be linked')],
  [/^already in your library; episodes in its folder not linked yet will be/, () => t('already in your library; episodes in its folder not linked yet will be')],
  [/^already in your library; folder not found at (.+?), so its file can't be linked/, (m) => t('already in your library; folder not found at {folder}, so its file can’t be linked', { folder: m[1] })],
  [/^already in your library; folder not found at (.+?), so its episodes can't be linked/, (m) => t('already in your library; folder not found at {folder}, so its episodes can’t be linked', { folder: m[1] })],
  [/^already in your library/, () => t('already in your library')],
  [/^folder not found at (.+?)(?=; |$)/, (m) => t('folder not found at {folder}', { folder: m[1] })],
  [/^the file is linked where it is/, () => t('the file is linked where it is')],
  [/^(\d+) episode file\(s\) are linked where they are/, (m) => t('{n} episode file(s) are linked where they are', { n: m[1] })],
  [/^Radarr has no file for it yet: added as wanted, no search is started/, () => t('Radarr has no file for it yet: added as wanted, no search is started')],
  [/^Sonarr has no episode files for it yet: added, no search is started/, () => t('Sonarr has no episode files for it yet: added, no search is started')],
  [/^Prowlarr gave no address for it/, () => t('Prowlarr gave no address for it')],
  [/^already added \(same address\)/, () => t('already added (same address)')],
  [/^already added \(same site\)/, () => t('already added (same site)')],
  [/^already added \(same host and username\)/, () => t('already added (same host and username)')],
  [/^no usable host name/, () => t('no usable host name')],
  [/^(.+?), so it stays on Prowlarr: added as Prowlarr's own feed for it, with Prowlarr's API key/, (m) =>
    t('{why}, so it stays on Prowlarr: added as Prowlarr’s own feed for it, with Prowlarr’s API key', { why: why(m[1]) })],
  [/^Mediarium's site list has no (.+?), so it stays on Jackett/, (m) => t('Mediarium’s site list has no {id}, so it stays on Jackett', { id: m[1] })],
  [/^switched off in (\w+), so added switched off/, (m) => t('switched off in {app}, so added switched off', { app: m[1] })],
  [/^Prowlarr hid its API key: added switched off, enter the key under Settings > Finding releases > Indexers & Search/, () => t('Prowlarr hid its API key: added switched off, enter the key under Settings > Finding releases > Indexers & Search')],
  [/^Prowlarr hid its password or cookie: added switched off, enter it under Settings > Finding releases > Indexers & Search/, () => t('Prowlarr hid its password or cookie: added switched off, enter it under Settings > Finding releases > Indexers & Search')],
  [/^(\w+) did not reveal the password: added switched off, enter it under Settings > Downloading > Usenet/, (m) => t('{app} did not reveal the password: added switched off, enter it under Settings > Downloading > Usenet', { app: m[1] })],
  [/^optional in (\w+); Mediarium tries servers in priority order/, (m) => t('optional in {app}; Mediarium tries servers in priority order', { app: m[1] })],
  [/^added as the site itself, not through Jackett/, () => t('added as the site itself, not through Jackett')],
  [/^it is a private tracker, so it is added switched off: enter your login under Settings > Finding releases > Indexers & Search/, () => t('it is a private tracker, so it is added switched off: enter your login under Settings > Finding releases > Indexers & Search')],
  [/^added through Jackett's Torznab feed, with Jackett's API key/, () => t('added through Jackett’s Torznab feed, with Jackett’s API key')],
]

/** importReason puts a preview line's explanation into the screen language where it knows the words. */
export function importReason(reason: string): string {
  let rest = reason.trim()
  const out: string[] = []
  while (rest) {
    let done = false
    for (const [re, say] of RULES) {
      const m = rest.match(re)
      if (!m) continue
      out.push(say(m))
      rest = rest.slice(m[0].length).replace(/^;\s*/, '')
      done = true
      break
    }
    if (!done) {
      // Not one of the known parts: keep it as written, up to the next part.
      const cut = rest.indexOf('; ')
      out.push(cut < 0 ? rest : rest.slice(0, cut))
      rest = cut < 0 ? '' : rest.slice(cut + 2)
    }
  }
  return out.join('; ')
}
