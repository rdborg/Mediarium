// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { originText, type Origin } from './origin.ts'

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
]

for (const [name, origin, text] of cases) {
  test(`originText: ${name}`, () => {
    assert.equal(originText(origin), text)
  })
}
