// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { redoAnswerText, redoRequest, shortFileText, shortFileTip } from './shortFile.ts'

test('the chip says how long the file is', () => {
  assert.equal(shortFileText({ minutes: 1, expected: 45 }), 'Only 1 min long')
  assert.equal(shortFileText({ minutes: 12 }), 'Only 12 min long')
})

test('the hover text names the length the title should have, when it is known', () => {
  assert.equal(shortFileTip({ minutes: 1, expected: 45 }), 'This file is 1 min long, but it should be about 45 min. It may be a fake or a cut-off download.')
  assert.equal(shortFileTip({ minutes: 2 }), 'This file is much shorter than a real episode or movie. It may be a fake or a cut-off download.')
})

test('an archive saved under a video name says what it is', () => {
  assert.equal(shortFileText({ minutes: 0, archive: 'rar' }), 'Not a video (RAR archive)')
  assert.equal(shortFileTip({ minutes: 0, archive: '7z' }), 'This file is a 7Z archive that was never unpacked, saved under a video name. No player can open it.')
})

test('only titles with a file that looks incomplete are reached, and their files are counted', () => {
  const r = redoRequest([
    { kind: 'movie', id: 1, shortFiles: 1 },
    { kind: 'movie', id: 2, shortFiles: 0 },
    { kind: 'tv', id: 7, shortFiles: 3 },
    { kind: 'tv', id: 8, shortFiles: 0 },
  ])
  assert.deepEqual(r, { movieIds: [1], seriesIds: [7], files: 4 })
})

test('an empty list reaches nothing', () => {
  assert.deepEqual(redoRequest([]), { movieIds: [], seriesIds: [], files: 0 })
})

test('the answer says what was deleted and searched', () => {
  assert.equal(redoAnswerText({ files: 1, titles: 1, searching: 1, skipped: 0 }), 'Deleted the file of 1 title. It is wanted again. Searching for 1 title now.')
  assert.equal(redoAnswerText({ files: 4, titles: 3, searching: 0, skipped: 0 }), 'Deleted the files of 3 titles. They are wanted again.')
  assert.equal(redoAnswerText({ files: 0, titles: 0, searching: 0, skipped: 2 }), 'Nothing was deleted. The files no longer look incomplete.')
})
