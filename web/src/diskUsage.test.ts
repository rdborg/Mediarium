// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { diskSummary, removeOption } from './diskUsage.ts'

test('diskSummary counts files and size', () => {
  assert.equal(diskSummary(null), '')
  assert.equal(diskSummary({ files: 0, bytes: 0 }), 'no files')
  assert.equal(diskSummary({ files: 1, bytes: 2048 }), '1 file, 2.0 KB')
  assert.equal(diskSummary({ files: 3, bytes: 11 * 1024 ** 3 }), '3 files, 11.0 GB')
})

test('deleting files is never ticked by default', () => {
  for (const noun of ['movie', 'show', 'artist']) {
    for (const usage of [null, { files: 0, bytes: 0 }, { files: 4, bytes: 5e9 }]) {
      const o = removeOption(noun, usage)
      assert.equal(o.defaultChecked, false)
      assert.match(String(o.warning), new RegExp(`this ${noun} will be deleted for good`))
    }
  }
})

test('the hint shows the size and says it cannot be undone', () => {
  const o = removeOption('movie', { files: 3, bytes: 11 * 1024 ** 3 })
  assert.equal(o.hint, 'The files in your library: 3 files, 11.0 GB. This cannot be undone.')
  assert.equal(removeOption('artist', null).hint, "The artist's albums in your music folder. This cannot be undone.")
  assert.equal(removeOption('show', { files: 0, bytes: 0 }).hint, 'There are no files in your library for this show.')
})

test('with the recycle bin on, the hint says the files wait there first', () => {
  const o = removeOption('movie', { files: 1, bytes: 1024, trashDays: 7 })
  assert.equal(o.hint, 'The files in your library: 1 file, 1.0 KB. They wait in the recycle bin for 7 days first.')
  assert.match(o.warning ?? '', /recycle bin/)
})
