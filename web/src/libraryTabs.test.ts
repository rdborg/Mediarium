// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { activeLibraryTab, libraryEntries, libraryTabPath, libraryTabs, type LibraryModule } from './libraryTabs.ts'

const only =
  (...keys: LibraryModule[]) =>
  (key: LibraryModule) =>
    keys.includes(key)

test('the tabs follow the switches, in a fixed order', () => {
  const rows: [string, LibraryModule[], string[]][] = [
    ['everything on', ['movies', 'tv', 'music', 'ebooks', 'audiobooks'], ['movie', 'tv', 'music', 'ebook', 'audiobook']],
    ['the usual start', ['movies', 'tv'], ['movie', 'tv']],
    ['music switched off', ['movies', 'tv', 'ebooks'], ['movie', 'tv', 'ebook']],
    ['books switched off', ['movies', 'tv', 'music'], ['movie', 'tv', 'music']],
    ['only TV', ['tv'], ['tv']],
    ['the anime library', ['movies', 'tv', 'anime'], ['movie', 'tv', 'anime']],
    ['anime without TV has no tab', ['movies', 'anime'], ['movie']],
    ['only music', ['music'], ['music']],
    ['only books', ['ebooks', 'audiobooks'], ['ebook', 'audiobook']],
    ['only audiobooks', ['audiobooks'], ['audiobook']],
    ['nothing on', [], ['movie']],
  ]
  for (const [name, on, want] of rows) assert.deepEqual(libraryTabs(only(...on)), want, name)
})

test('the open tab comes from the address and falls back to the first one that is on', () => {
  const tabs = libraryTabs(only('tv', 'music'))
  const rows: [string | null, string][] = [
    ['music', 'music'],
    ['tv', 'tv'],
    [null, 'tv'], // no kind in the address: Movies, which is off, so the first that is on
    ['movie', 'tv'], // switched off
    ['ebook', 'tv'], // switched off
    ['nonsense', 'tv'],
    ['', 'tv'],
  ]
  for (const [param, want] of rows) assert.equal(activeLibraryTab(tabs, param), want, String(param))
  assert.equal(activeLibraryTab(libraryTabs(only('movies', 'tv')), null), 'movie')
})

test('each tab has an address that opens the Library on it', () => {
  assert.equal(libraryTabPath('movie'), '/library?kind=movie')
  assert.equal(libraryTabPath('audiobook'), '/library?kind=audiobook')
})

test('the menu lists only the tabs that are on, and marks the open one', () => {
  const on = only('movies', 'tv', 'ebooks')
  const entries = libraryEntries(on, '/library', '?kind=tv')
  assert.deepEqual(
    entries.map((e) => [e.tab, e.to, e.active]),
    [
      ['movie', '/library?kind=movie', false],
      ['tv', '/library?kind=tv', true],
      ['ebook', '/library?kind=ebook', false],
    ],
  )
})

test('the plain /library address marks Movies, and an old link to a switched-off tab marks the first one', () => {
  assert.deepEqual(libraryEntries(only('movies', 'tv'), '/library', '').filter((e) => e.active).map((e) => e.tab), ['movie'])
  assert.deepEqual(libraryEntries(only('tv', 'music'), '/library', '?kind=movie').filter((e) => e.active).map((e) => e.tab), ['tv'])
})

test('other filters in the address do not change the open tab', () => {
  const entries = libraryEntries(only('movies', 'tv'), '/library', '?kind=tv&st=pending&q=alien')
  assert.deepEqual(entries.filter((e) => e.active).map((e) => e.tab), ['tv'])
})

test('nothing is marked open away from the Library page', () => {
  for (const path of ['/', '/series/4', '/queue', '/settings/media', '/libraryx']) {
    assert.equal(libraryEntries(only('movies', 'tv'), path, '?kind=tv').some((e) => e.active), false, path)
  }
  assert.equal(libraryEntries(only('movies', 'tv'), '/library/', '?kind=tv').some((e) => e.active), true)
})
