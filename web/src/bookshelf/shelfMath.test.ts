import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatAudioPosition, formatClock, nextSpeed, overallPercent, parseAudioPosition, trackLabel } from './shelfMath.ts'

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
