// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import * as t from './importText.ts'
import { trailerLabels } from './trailerLabels.ts'

const batch = (over: Partial<t.BatchCounts> = {}): t.BatchCounts => ({
  kind: 'tv',
  elapsedMs: 0,
  total: 25,
  done: 4,
  added: 25,
  already: 0,
  problems: 0,
  monitor: false,
  noUpgrade: true,
  monitorMissing: false,
  ...over,
})

test('banner and report wording', () => {
  const rows: [string, string, string][] = [
    ['progress', t.progressText(batch()), 'Importing your library: 4 of 25 shows.'],
    ['progress, one movie', t.progressText(batch({ kind: 'movie', total: 1, done: 0 })), 'Importing your library: 0 of 1 movie.'],
    ['finished', t.finishedText(batch({ done: 25 })), 'Import finished. 25 shows added, 0 problems.'],
    ['finished, one problem', t.finishedText(batch({ kind: 'movie', added: 188, problems: 1 })), 'Import finished. 188 movies added, 1 problem.'],
    ['total line', t.totalLine(batch({ kind: 'movie', added: 188, already: 2 })), '188 movies added, 2 already in library, 0 problems'],
    ['total line, singular', t.totalLine(batch({ added: 1, already: 1, problems: 1 })), '1 show added, 1 already in library, 1 problem'],
  ]
  for (const [name, got, want] of rows) assert.equal(got, want, name)
})

test('time left', () => {
  const rows: [string, Partial<t.BatchCounts>, string][] = [
    ['nothing to go on yet', { done: 1, elapsedMs: 20_000 }, ''],
    ['too early', { done: 4, elapsedMs: 1_000 }, ''],
    ['already finished', { done: 25, elapsedMs: 60_000 }, ''],
    ['almost done', { done: 24, elapsedMs: 24_000 }, 'Almost done.'],
    ['seconds', { done: 10, total: 25, elapsedMs: 20_000 }, 'About 30 seconds left.'],
    ['a minute', { done: 10, total: 25, elapsedMs: 40_000 }, 'About 1 minute left.'],
    ['minutes', { done: 20, total: 188, elapsedMs: 60_000 }, 'About 9 minutes left.'],
    ['a long time', { done: 2, total: 400, elapsedMs: 600_000 }, 'More than an hour left.'],
  ]
  for (const [name, over, want] of rows) assert.equal(t.etaText(batch(over)), want, name)
})

test('what happens after the import', () => {
  const rows: [string, t.ImportKind, boolean, boolean, RegExp][] = [
    ['movies, safe defaults', 'movie', false, false, /^Nothing will be downloaded/],
    ['movies, watched', 'movie', true, false, /better version/],
    ['shows, safe defaults', 'tv', false, false, /^Nothing will be downloaded for these shows/],
    ['shows, missing wanted', 'tv', false, true, /missing will be searched for.*left alone/],
    ['shows, watched', 'tv', true, false, /New episodes.*replace.*Episodes you are missing now are left alone/],
    ['shows, everything', 'tv', true, true, /missing will be searched for.*also replace/],
  ]
  for (const [name, kind, monitor, monitorMissing, want] of rows) {
    assert.match(t.afterText(kind, monitor, monitorMissing), want, name)
  }
})

test('banner note about what starts afterwards', () => {
  const rows: [string, Partial<t.BatchCounts>, string][] = [
    ['shows, safe defaults', {}, 'Nothing will be downloaded.'],
    ['shows, missing episodes', { monitorMissing: true }, "When it's done, Mediarium starts looking for the episodes you are missing."],
    ['shows, watched', { monitor: true, noUpgrade: false }, "When it's done, Mediarium starts looking for new episodes and better versions of what you already have."],
    ['shows, everything', { monitor: true, noUpgrade: false, monitorMissing: true }, "When it's done, Mediarium starts looking for the episodes you are missing and better versions of what you already have."],
    ['movies, safe defaults', { kind: 'movie' }, 'Nothing will be downloaded.'],
    ['movies, watched', { kind: 'movie', monitor: true, noUpgrade: false }, "When it's done, Mediarium starts looking for better versions of what you already have."],
    ['movies, watched but better versions off', { kind: 'movie', monitor: true }, 'Nothing will be downloaded.'],
  ]
  for (const [name, over, want] of rows) assert.equal(t.upNextText(batch(over)), want, name)
  assert.equal(t.upNextText(batch({ monitorMissing: true }), true), 'From now on, Mediarium looks for the episodes you are missing.')
  assert.equal(t.upNextText(batch(), true), 'They are not monitored, so nothing will be downloaded.')
})

test('what the report says was set', () => {
  const rows: [string, Partial<t.BatchCounts>, RegExp][] = [
    ['shows, safe defaults', {}, /^These shows were added without monitoring.*Nothing will be downloaded until you start monitoring them\.$/],
    ['movies, safe defaults', { kind: 'movie' }, /^These movies were added without monitoring/],
    ['movies, watched', { kind: 'movie', monitor: true, noUpgrade: false }, /^These movies are monitored\. .*better version/],
    ['shows, missing episodes only', { monitorMissing: true }, /^These shows are monitored\. Episodes you are missing/],
  ]
  for (const [name, over, want] of rows) assert.match(t.setText(batch(over)), want, name)
})

test('trailer labels', () => {
  const rows: [string, (string | undefined)[], string[]][] = [
    ['one of each', ['Trailer', 'Teaser', 'Clip'], ['Trailer', 'Teaser', 'Clip']],
    ['two trailers and a teaser', ['Trailer', 'Trailer', 'Teaser'], ['Trailer 1', 'Trailer 2', 'Teaser']],
    ['two of two kinds', ['Trailer', 'Teaser', 'Trailer', 'Teaser'], ['Trailer 1', 'Teaser 1', 'Trailer 2', 'Teaser 2']],
    ['no type', [undefined, undefined], ['Video 1', 'Video 2']],
    ['lower case and other kinds', ['trailer', 'Behind the Scenes', 'Bloopers'], ['Trailer', 'Behind the scenes', 'Bloopers']],
    ['none', [], []],
  ]
  for (const [name, types, want] of rows) assert.deepEqual(trailerLabels(types.map((type) => ({ type }))), want, name)
})
