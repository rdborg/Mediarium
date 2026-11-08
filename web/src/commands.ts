// The command palette (Ctrl+K or Cmd+K, V4 D7): every page and settings page
// by name, a few things to do, and a search for whatever was typed. This file
// has the list and the matching, with no React, so it can be tested on its own.
import { t } from './i18n.ts'

export type CommandAction = 'pause-all' | 'resume-all' | 'quiet-hour' | 'quiet-end'

export interface Command {
  id: string
  label: string
  hint?: string // the group or what it does, shown dim on the right
  to?: string // a page to open
  action?: CommandAction
  admin?: boolean // only offered to administrators
  words?: string // more words it is found by
}

export interface PageEntry {
  to: string
  label: string
}

// commandsFor builds the list: the main pages, the settings pages given, and
// the actions an administrator can take.
export function commandsFor(settings: PageEntry[], admin: boolean): Command[] {
  const pages: Command[] = [
    { id: 'p-dash', label: t('Dashboard'), to: '/', words: 'home start' },
    { id: 'p-discover', label: t('Discover'), to: '/discover', words: 'popular trending new' },
    { id: 'p-library', label: t('Library'), to: '/library', words: 'movies shows' },
    { id: 'p-wanted', label: t('Wanted'), to: '/wanted', words: 'missing' },
    { id: 'p-calendar', label: t('Calendar'), to: '/calendar', words: 'upcoming airs' },
    { id: 'p-activity', label: t('Activity'), to: '/queue', words: 'queue downloads history' },
    { id: 'p-search', label: t('Search'), to: '/search', words: 'find' },
    { id: 'p-releases', label: t('Search releases'), to: '/search/releases', words: 'indexers nzb torrent' },
  ]
  const settingsPages: Command[] = settings.map((p) => ({ id: 's-' + p.to, label: p.label, hint: t('Settings'), to: p.to }))
  const actions: Command[] = [
    { id: 'a-pause', label: t('Pause all downloads'), hint: t('Activity'), action: 'pause-all', admin: true, words: 'stop' },
    { id: 'a-resume', label: t('Resume all downloads'), hint: t('Activity'), action: 'resume-all', admin: true, words: 'start continue' },
    { id: 'a-quiet', label: t('Quiet for an hour'), hint: t('Automatic searching'), action: 'quiet-hour', admin: true, words: 'pause searches busy' },
    { id: 'a-quiet-end', label: t('Back to normal'), hint: t('Automatic searching'), action: 'quiet-end', admin: true, words: 'quiet end resume searches' },
  ]
  return [...pages, ...settingsPages, ...actions].filter((c) => admin || !c.admin)
}

// score is how well text matches a command, 0 for not at all. Every word of
// the text has to be found, at the start of a word scores more than inside
// one, and the label counts more than the extra words.
export function score(cmd: Command, text: string): number {
  const words = text.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return 1
  const label = cmd.label.toLowerCase()
  const extra = `${cmd.hint ?? ''} ${cmd.words ?? ''}`.toLowerCase()
  let total = 0
  for (const w of words) {
    const at = label.indexOf(w)
    if (at === 0) total += 6
    else if (at > 0 && /\W/.test(label[at - 1])) total += 5
    else if (at > 0) total += 3
    else if (extra.includes(w)) total += 1
    else return 0
  }
  return total
}

// matching is the commands that fit text, best first, at most limit of them.
export function matching(cmds: Command[], text: string, limit = 9): Command[] {
  return cmds
    .map((c, i) => ({ c, s: score(c, text), i }))
    .filter((x) => x.s > 0)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .slice(0, limit)
    .map((x) => x.c)
}
