// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { offersCompleteSearch } from './completeSeries.ts'

test('an ended or cancelled show offers the complete-series search', () => {
  assert.equal(offersCompleteSearch('ended', 9), true)
  assert.equal(offersCompleteSearch('canceled', 1), true)
})

test('a show with no seasons has nothing to search for', () => {
  assert.equal(offersCompleteSearch('ended', 0), false)
  assert.equal(offersCompleteSearch(undefined, 0), false)
})

test('a running show offers it once an earlier season is finished', () => {
  assert.equal(offersCompleteSearch('continuing', 1), false)
  assert.equal(offersCompleteSearch('continuing', 4), true)
  assert.equal(offersCompleteSearch(undefined, 3), true)
})
