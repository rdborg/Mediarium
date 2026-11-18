// Run with: npm test
// Texts that send a person to a settings page must use the names the menu has now
// (Settings > Library and files > Folders), not the old groups.
import assert from 'node:assert/strict'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { test } from 'node:test'
import { SETTINGS_NAV, settingsGroupsFor } from './settingsNav.ts'

const here = new URL('.', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1')

function sources(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      if (name !== 'locales' && name !== 'node_modules') sources(p, out)
    } else if (/\.(ts|tsx)$/.test(name) && !/\.test\.ts$/.test(name)) out.push(p)
  }
  return out
}

// The groups that no longer exist in the menu.
const OLD = [
  /Settings (?:>|→) Info, lists and subtitles/,
  /Settings (?:>|→) Account & Access/,
  /Settings (?:>|→) Connections/,
  /Settings (?:>|→) Library (?:>|→)/,
  /Settings (?:>|→) Indexers & Search/,
  /Settings (?:>|→) Media types/,
]

test('no text sends a person to a settings group the menu no longer has', () => {
  const bad: string[] = []
  for (const file of sources(here)) {
    const lines = readFileSync(file, 'utf8').split(/\r?\n/)
    lines.forEach((line, i) => {
      // Another program's own settings (Prowlarr, Radarr, Emby) keep their names.
      if (/Prowlarr|Radarr|Sonarr|Emby|Audiobookshelf|Media Management/.test(line)) return
      for (const rx of OLD) if (rx.test(line)) bad.push(`${file.slice(here.length)}:${i + 1}: ${line.trim().slice(0, 120)}`)
    })
  }
  assert.deepEqual(bad, [])
})

test('the settings menu has the seven groups the texts talk about', () => {
  const labels = settingsGroupsFor(true).map((g) => g.label)
  assert.deepEqual(labels, ['Basics', 'Library and files', 'Finding releases', 'Downloading', 'Media servers and apps', 'People and access', 'System'])
  assert.ok(SETTINGS_NAV.length > 20)
})

// Every "Settings > Group > Page" a text names must exist in the menu: in the web app, in the
// sentences the server sends and in the guide, not only in the old groups' names.
const root = join(here, '..', '..')
function files(dir: string, ext: RegExp, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      if (!['node_modules', 'locales', 'testdata', 'images', '.git'].includes(name)) files(p, ext, out)
    } else if (ext.test(name) && !/_test\.go$|\.test\.ts$/.test(name)) out.push(p)
  }
  return out
}

// Another program's own settings keep their names (Prowlarr, Emby, a release page ...).
const OTHER_APP = /Prowlarr|Radarr|Sonarr|Emby|Audiobookshelf|Media Management|Letterboxd|NZBGet|SABnzbd|Overseerr|Ombi|Bazarr|Medusa|Calendar|themoviedb|Devices & services|Help & About|Environments|API Keys|Import & Export|Docker/
const PATH = /Settings (?:&gt;|>|→) ([A-Z][^.;:)"'`*,/>→&]*?)(?: (?:&gt;|>|→) ([A-Z][^.;:)"'`*,/>→&]*?))?(?=[.;:)"'`*,\]]| (?:&gt;|>|→))/g

test('every settings path in the app, the server texts and the guide names a real page', () => {
  const groups = settingsGroupsFor(true)
  const bad: string[] = []
  const all = [
    ...sources(here),
    ...files(join(root, 'internal'), /\.go$/),
    ...files(join(root, 'docs'), /\.md$/).filter((f) => !/RELEASING/.test(f)),
  ]
  for (const file of all) {
    readFileSync(file, 'utf8').split(/\r?\n/).forEach((line, i) => {
      if (OTHER_APP.test(line)) return
      for (const m of line.matchAll(PATH)) {
        const g = groups.find((x) => m[1] === x.label || m[1].startsWith(x.label + ' '))
        const where = `${file.slice(root.length + 1)}:${i + 1}: ${m[0]}`
        if (!g) bad.push(where)
        else if (m[2] && !g.pages.some((p) => m[2] === p.label || m[2].startsWith(p.label + ' '))) bad.push(where)
      }
    })
  }
  assert.deepEqual(bad, [])
})
