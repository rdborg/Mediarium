// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { releaseHint } from './releaseHint.ts'

const down = [{ name: 'Example NZB Site', message: 'Could not reach Example NZB Site.' }]

test('no indexer at all', () => {
  const h = releaseHint(0, [], 0)
  assert.equal(h?.text, 'No indexer is set up yet. Add one under')
  assert.equal(h?.link, true)
  assert.equal(h?.empty, true)
})

test('an indexer that did not answer is named, not blamed on missing setup', () => {
  const h = releaseHint(1, down, 0)
  assert.equal(h?.text, 'The indexer did not answer: Example NZB Site. Check it under')
  assert.equal(h?.link, true)
  assert.doesNotMatch(h?.text ?? '', /set up/)
  const two = releaseHint(2, [...down, { name: 'B', message: '' }], 0)
  assert.equal(two?.text, 'The indexers did not answer: Example NZB Site, B. Check them under')
})

test('some answered, none had the title', () => {
  const h = releaseHint(3, down, 0)
  assert.equal(h?.text, 'No matching releases. 1 indexer did not answer: Example NZB Site. Check it under')
})

test('everything answered and nothing matched', () => {
  const h = releaseHint(2, [], 0)
  assert.match(h?.text ?? '', /^No matching releases right now/)
  assert.equal(h?.link, false)
})

test('releases found: only a note when someone did not answer', () => {
  assert.equal(releaseHint(2, [], 5), null)
  const h = releaseHint(2, down, 5)
  assert.equal(h?.empty, false)
  assert.equal(h?.text, '1 indexer did not answer: Example NZB Site. The list may be missing some releases.')
})
