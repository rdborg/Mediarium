// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { albumOriginText, originText, type Origin } from './origin.ts'

const user = { id: 3, name: 'Anna' }
const cases: [string, Origin | null | undefined, string][] = [
  ['nothing recorded', undefined, ''],
  ['null', null, ''],
  ['empty kind', { kind: '' }, ''],
  ['a kind from a newer version', { kind: 'teleport' }, ''],
  ['manual with a person', { kind: 'manual', user }, 'Added by Anna (manual)'],
  ['manual without a person', { kind: 'manual' }, 'Added by hand'],
  ['a search a person started', { kind: 'search', user }, 'Added by Anna via automated search'],
  ['a search started by hand, person unknown', { kind: 'search' }, 'Added via a search started by hand'],
  ['the scheduled search', { kind: 'auto', name: 'search' }, 'Added by Mediarium via automated search'],
  ['an auto grab with no job named', { kind: 'auto' }, 'Added by Mediarium via automated search'],
  ['the RSS check', { kind: 'auto', name: 'rss' }, 'Added by Mediarium via the RSS check'],
  ['an announce channel', { kind: 'auto', name: 'announce' }, 'Added by Mediarium via an IRC announce channel'],
  ['a list', { kind: 'list', name: 'Trending' }, 'Added by Mediarium via list Trending'],
  ['a list with no name', { kind: 'list' }, 'Added by Mediarium via an import list'],
  ['a folder scan', { kind: 'import' }, 'Added by Mediarium from a library folder scan'],
  ['the API', { kind: 'api' }, 'Added by an app through the API'],
  ['a request', { kind: 'request', user }, 'Added by Mediarium after Anna asked for it'],
  ['a request, person gone', { kind: 'request' }, 'Added by Mediarium after a request'],
  ['a watchlist', { kind: 'watchlist', user }, "Added by Mediarium from Anna's Plex watchlist"],
  ['a followed artist, author or series', { kind: 'auto', name: 'follow' }, 'Added by Mediarium via a followed artist, author or series'],
]

for (const [name, origin, text] of cases) {
  test(`originText: ${name}`, () => {
    assert.equal(originText(origin), text)
  })
}

const albumCases: [string, Origin | undefined, Origin | undefined, string][] = [
  ['came with its artist', { kind: 'manual', user }, { kind: 'manual', user }, ''],
  ['found by following the artist', { kind: 'auto', name: 'follow' }, { kind: 'manual', user }, 'Added by Mediarium via a followed artist, author or series'],
  ['added later by someone else', { kind: 'manual', user: { id: 4, name: 'Sam' } }, { kind: 'request', user }, 'Added by Sam (manual)'],
  ['nothing recorded for the album', undefined, { kind: 'manual', user }, ''],
  ['nothing recorded for the artist', { kind: 'import' }, undefined, 'Added by Mediarium from a library folder scan'],
]

for (const [name, album, artist, text] of albumCases) {
  test(`albumOriginText: ${name}`, () => {
    assert.equal(albumOriginText(album, artist), text)
  })
}
