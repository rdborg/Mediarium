import assert from 'node:assert/strict'
import { test } from 'node:test'
import { buildChapters, chapterAt, formatAudioPosition, formatClock, nextSpeed, overallPercent, parseAudioPosition, trackLabel } from './shelfMath.ts'

test('audiobook positions read and write', () => {
  assert.deepEqual(parseAudioPosition('2:93.5'), { track: 2, seconds: 93.5 })
  assert.deepEqual(parseAudioPosition(''), { track: 0, seconds: 0 })
  assert.deepEqual(parseAudioPosition('epubcfi(/6/2)'), { track: 0, seconds: 0 })
  assert.equal(formatAudioPosition({ track: 1, seconds: 12.345 }), '1:12.3')
})

test('how far through the book', () => {
  assert.equal(overallPercent([100, 100], 0, 0.5), 25)
  assert.equal(overallPercent([100, 300], 1, 0), 25)
  assert.equal(overallPercent([100, 300], 1, 1), 100)
  assert.equal(overallPercent([], 0, 0.5), 0)
  assert.equal(overallPercent([100], 3, 0.5), 0)
  assert.equal(overallPercent([100], 0, Number.NaN), 0)
})

test('clock times', () => {
  assert.equal(formatClock(75), '1:15')
  assert.equal(formatClock(3725), '1:02:05')
  assert.equal(formatClock(Number.NaN), '0:00')
})

test('speeds go round', () => {
  assert.equal(nextSpeed(1), 1.2)
  assert.equal(nextSpeed(2), 0.8)
  assert.equal(nextSpeed(1.1), 1.2)
})

test('track names', () => {
  assert.equal(trackLabel('01 - Chapter One', 0), 'Chapter One')
  assert.equal(trackLabel('07', 6), 'Part 7')
  assert.equal(trackLabel('The Hobbit', 0), 'The Hobbit')
})

test('chapters from files and from marks inside a file', () => {
  const files = buildChapters([{ name: '01 - Opening' }, { name: '02 - The End' }])
  assert.deepEqual(
    files.map((c) => [c.track, c.start, c.end, c.title]),
    [
      [0, 0, undefined, 'Opening'],
      [1, 0, undefined, 'The End'],
    ],
  )
  const one = buildChapters([
    {
      name: 'Book',
      chapters: [
        { title: 'Two', start: 600 },
        { title: 'Credits', start: 0.4 },
        { title: '', start: 1800 },
      ],
    },
  ])
  assert.deepEqual(
    one.map((c) => [c.track, c.start, c.end, c.title]),
    [
      [0, 0, 600, 'Credits'],
      [0, 600, 1800, 'Two'],
      [0, 1800, undefined, 'Chapter 3'],
    ],
  )
  // A file with a single mark counts as one chapter.
  assert.equal(buildChapters([{ name: '01 - Solo', chapters: [{ title: 'Only', start: 0 }] }])[0].title, 'Solo')
})

test('which chapter is playing', () => {
  const ch = buildChapters([{ name: 'a' }, { name: 'b', chapters: [{ title: 'x', start: 0 }, { title: 'y', start: 100 }] }, { name: 'c' }])
  assert.equal(chapterAt(ch, 0, 50), 0)
  assert.equal(chapterAt(ch, 1, 0), 1)
  assert.equal(chapterAt(ch, 1, 99.9), 2)
  assert.equal(chapterAt(ch, 1, 150), 2)
  assert.equal(chapterAt(ch, 2, 10), 3)
  assert.equal(chapterAt(ch, 9, 10), 0)
})
