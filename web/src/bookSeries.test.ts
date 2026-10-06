import assert from 'node:assert/strict'
import { test } from 'node:test'
import { audioDetails, entryMeta } from './bookSeries.ts'

test('the line under a book of a series', () => {
  const today = '2026-10-05'
  const cases: [Parameters<typeof entryMeta>[0], string][] = [
    [{ position: '2', year: 1998 }, 'Book 2 · 1998'],
    [{ position: '1' }, 'Book 1'],
    [{ position: '3', year: 2020, releaseDate: '2020-05-01' }, 'Book 3 · 2020'],
    [{ position: '4', year: 2027, releaseDate: '2027-03-01' }, 'Book 4 · out 1 Mar 2027'],
    [{ position: '5', year: 2026, releaseDate: 'soon' }, 'Book 5 · 2026'],
  ]
  for (const [entry, want] of cases) assert.equal(entryMeta(entry, today, 'en-GB'), want)
})

test('the line about an audiobook', () => {
  const cases: [string | undefined, number | undefined, string][] = [
    ['Ray Porter', 970, 'Read by Ray Porter · 16 h 10 min'],
    ['Kate Reading, Michael Kramer', 2729, 'Read by Kate Reading, Michael Kramer · 45 h 29 min'],
    ['A, B, C, D, E', 274, 'Read by A, B, C and others · 4 h 34 min'],
    ['', 45, '45 min'],
    [undefined, 120, '2 h'],
    ['Ray Porter', 0, 'Read by Ray Porter'],
    [undefined, undefined, ''],
  ]
  for (const [n, m, want] of cases) assert.equal(audioDetails(n, m), want)
})
