import assert from 'node:assert/strict'
import { test } from 'node:test'
import { addStart, keepBetterStart } from './addDefaults.ts'

test('both choices start on when nothing was chosen', () => {
  assert.deepEqual(addStart(), { monitored: true, monitor: 'all', searchNow: true })
  assert.deepEqual(addStart(null), { monitored: true, monitor: 'all', searchNow: true })
  assert.deepEqual(addStart({}), { monitored: true, monitor: 'all', searchNow: true })
})

test('both off starts the dialog unmonitored and not searching', () => {
  assert.deepEqual(addStart({ addMonitored: false, addSearchNow: false }), { monitored: false, monitor: 'none', searchNow: false })
})

test('each choice is read on its own', () => {
  assert.deepEqual(addStart({ addMonitored: false }), { monitored: false, monitor: 'none', searchNow: true })
  assert.deepEqual(addStart({ addSearchNow: false }), { monitored: true, monitor: 'all', searchNow: false })
  assert.deepEqual(addStart({ addMonitored: true, addSearchNow: true }), { monitored: true, monitor: 'all', searchNow: true })
})

test('better versions start off, as they always did, unless the setting turns them on', () => {
  assert.equal(keepBetterStart(), false)
  assert.equal(keepBetterStart(null, null), false)
  assert.equal(keepBetterStart({}), false)
  assert.equal(keepBetterStart({ addKeepBetter: false }), false)
  assert.equal(keepBetterStart({ addKeepBetter: true }), true)
})

test('what this browser remembered wins over the setting', () => {
  assert.equal(keepBetterStart({ addKeepBetter: true }, false), false)
  assert.equal(keepBetterStart({ addKeepBetter: false }, true), true)
  assert.equal(keepBetterStart({ addKeepBetter: true }, null), true)
})
