// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { joinWords, requiredGaps, setupGaps, type SetupFacts } from './setupStatus.ts'

const done: SetupFacts = { indexers: 1, usenetServers: 1, torrentsReady: false, mediaServers: 1, foldersBroken: [] }

test('nothing is listed when everything is set up', () => {
  assert.deepEqual(setupGaps(done), [])
})

test('an empty install lists what is missing, in order', () => {
  const gaps = setupGaps({ indexers: 0, usenetServers: 0, torrentsReady: false, mediaServers: 0, foldersBroken: [] })
  assert.deepEqual(
    gaps.map((g) => g.key),
    ['sources', 'download', 'server'],
  )
  assert.deepEqual(
    gaps.map((g) => g.to),
    ['/settings/indexers', '/settings/downloads', '/settings/media-servers'],
  )
  // A media server is optional: it never counts as missing.
  assert.deepEqual(
    requiredGaps(gaps).map((g) => g.key),
    ['sources', 'download'],
  )
})

test('a working torrent setup counts as a download provider', () => {
  const gaps = setupGaps({ ...done, usenetServers: 0, torrentsReady: true })
  assert.deepEqual(gaps, [])
})

test('broken folders are named', () => {
  const gaps = setupGaps({ ...done, foldersBroken: ['Movies', 'TV shows'] })
  assert.equal(gaps.length, 1)
  assert.equal(gaps[0].key, 'folders')
  assert.match(gaps[0].detail, /^Movies and TV shows are missing/)
  assert.match(setupGaps({ ...done, foldersBroken: ['Movies'] })[0].detail, /^Movies is missing/)
})

test('joinWords', () => {
  assert.equal(joinWords([]), '')
  assert.equal(joinWords(['a']), 'a')
  assert.equal(joinWords(['a', 'b']), 'a and b')
  assert.equal(joinWords(['a', 'b', 'c']), 'a, b and c')
})
