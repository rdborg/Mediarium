import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { buildSettingsIndex, searchSettings, settingsRoutes } from './settingsIndex.ts'
import { SETTINGS_NAV, SETTINGS_REDIRECTS, settingsGroupsFor } from './settingsNav.ts'

const here = new URL('.', import.meta.url)

// The settings routes the router really has: the path="..." lines inside the
// /settings route in App.tsx.
function routerPaths(): Set<string> {
  const app = readFileSync(new URL('App.tsx', here), 'utf8')
  const start = app.indexOf('<Route path="/settings"')
  const end = app.indexOf('</Route>', start)
  assert.ok(start > 0 && end > start, 'the /settings route was not found in App.tsx')
  const paths = new Set<string>()
  for (const m of app.slice(start, end).matchAll(/<Route path="([a-z-]+)"/g)) paths.add(`/settings/${m[1]}`)
  return paths
}

function sourceText(): string {
  const out: string[] = []
  const walk = (dir: URL) => {
    for (const ent of readdirSync(dir, { withFileTypes: true })) {
      if (ent.isDirectory()) walk(new URL(`${ent.name}/`, dir))
      else if (/\.tsx$/.test(ent.name)) out.push(readFileSync(new URL(ent.name, dir), 'utf8'))
    }
  }
  walk(here)
  return out.join('\n')
}

test('every entry points at a settings route that exists', () => {
  const routes = routerPaths()
  for (const [admin, viewer] of [[true, false], [false, false], [false, true]] as const) {
    const entries = buildSettingsIndex(admin, viewer)
    for (const to of settingsRoutes(entries)) assert.ok(routes.has(to), `${to} is not a route (admin ${admin})`)
  }
  for (const p of SETTINGS_NAV) assert.ok(routes.has(p.to), `${p.to} is in the sidebar list but not in the router`)
  for (const [, to] of SETTINGS_REDIRECTS) assert.ok(routes.has(to), `${to} (a redirect target) is not a route`)
})

test('every settings page can be found, and nothing is listed twice', () => {
  const admin = buildSettingsIndex(true, false)
  const pages = admin.filter((e) => e.page).map((e) => e.to)
  assert.deepEqual(pages, SETTINGS_NAV.map((p) => p.to))
  const seen = new Set<string>()
  for (const e of admin) {
    const key = `${e.to}|${e.anchor ?? ''}`
    assert.ok(!seen.has(key), `listed twice: ${key}`)
    seen.add(key)
  }
  // every page can be reached by its own name
  for (const p of SETTINGS_NAV) {
    const top = searchSettings(p.label, admin, 3)[0]
    assert.ok(top && top.score >= 85, `${p.label} is not found by its name`)
  }
})

test('every card and setting is a heading the pages really show', () => {
  const src = sourceText()
  for (const e of buildSettingsIndex(true, false)) {
    if (e.page) continue
    assert.ok(src.includes(`'${e.anchor}'`) || src.includes(`"${e.anchor}"`), `no page shows "${e.anchor}" (${e.to})`)
  }
})

test('members and view-only accounts are only offered what they can open', () => {
  const member = buildSettingsIndex(false, false)
  assert.ok(member.every((e) => SETTINGS_NAV.find((p) => p.to === e.to)?.member), 'a member got an administrator page')
  assert.ok(!member.some((e) => e.anchor === 'Address of Mediarium'), 'an administrator card was offered to a member')
  const viewer = buildSettingsIndex(false, true)
  assert.ok(!viewer.some((e) => e.to === '/settings/services'), 'a view-only account got Linked services')
  // a media type that is off takes its folder with it
  const noMusic = buildSettingsIndex(true, false, (m) => m !== 'music')
  assert.ok(!noMusic.some((e) => e.anchor === 'Music folder'))
  assert.ok(buildSettingsIndex(true, false).some((e) => e.anchor === 'Music folder'))
})

test('typing what people look for lands on the right place', () => {
  const index = buildSettingsIndex(true, false)
  const first = (q: string) => {
    const hit = searchSettings(q, index, 1)[0]?.entry
    return hit ? `${hit.to}${hit.anchor ? ` > ${hit.anchor}` : ''}` : ''
  }
  const cases: [string, string][] = [
    ['Folders', '/settings/media'],
    ['temp folder', '/settings/download-rules > Temporary download folder'],
    ['Quality profiles', '/settings/quality > Quality profiles'],
    ['indexers', '/settings/indexers'],
    ['usenet', '/settings/usenet'],
    ['vpn', '/settings/vpn'],
    ['Notifications', '/settings/notifications'],
    ['backups', '/settings/backup'],
    ['api keys', '/settings/profile > API keys'],
    ['timezone', '/settings/general > Time zone'],
    ['dark mode', '/settings/general > Light or dark'],
    ['passkey', '/settings/security > Passkeys'],
    ['two factor', '/settings/security > Two-step sign-in'],
    ['flaresolverr', '/settings/indexers > How to add FlareSolverr to Docker'],
  ]
  // the words people use for automatic quality upgrades reach the pages that hold them
  for (const q of ['automatic quality updates', 'automatic quality upgrades', 'upgrade', 'upgrades', 'better quality', 'replace with better']) {
    const hits = searchSettings(q, index, 5).map((h) => h.entry.to)
    assert.ok(hits.includes('/settings/quality') || hits.includes('/settings/automation'), `"${q}" finds neither Quality nor Automatic searching`)
  }
  for (const [q, want] of cases) assert.equal(first(q), want, q)
  // "Folders" is found as Library › Folders
  const folders = searchSettings('Folders', index, 1)[0].entry
  assert.equal(`${folders.trail} › ${folders.label}`, 'Library and files › Folders')
})

test('every menu page sits in exactly one group and has a description', () => {
  const groups = settingsGroupsFor(true, false)
  const listed = groups.flatMap((g) => g.pages.map((p) => p.to))
  assert.deepEqual(listed, SETTINGS_NAV.map((p) => p.to), 'the groups must hold every page once, in the sidebar order')
  for (const g of groups) assert.ok(g.blurb.trim().length > 0, `${g.key} has no description`)
  for (const p of SETTINGS_NAV) assert.ok(p.blurb.trim().length > 0, `${p.to} has no description`)
  // every page is in the search index, with its group as the trail
  const index = buildSettingsIndex(true, false)
  for (const g of groups) {
    for (const p of g.pages) {
      const e = index.find((x) => x.page && x.to === p.to)
      assert.ok(e, `${p.to} is not in the search index`)
      assert.equal(e.trail, g.label, `${p.to} has the wrong trail`)
    }
  }
  // accounts that may not open a page do not see it in any group
  for (const [admin, viewer] of [[false, false], [false, true]] as const) {
    const ok = new Set(SETTINGS_NAV.filter((p) => (admin || p.member) && !(viewer && p.notViewer)).map((p) => p.to))
    for (const g of settingsGroupsFor(admin, viewer)) for (const p of g.pages) assert.ok(ok.has(p.to), `${p.to} shown to a lower account`)
  }
})
