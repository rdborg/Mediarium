// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { indexerChoices, resultText, seconds, summaryLine, why, type Searched } from './searched.ts'

const line = (name: string, status: Searched['status'], extra: Partial<Searched> = {}): Searched => ({
  indexerId: 1,
  name,
  kind: 'torrent',
  status,
  count: 0,
  ms: 0,
  ...extra,
})

test('the summary line counts each kind of answer and leaves out the empty ones', () => {
  const eight = [
    line('A', 'found', { count: 3 }),
    line('B', 'found', { count: 9 }),
    line('C', 'empty'),
    line('D', 'empty'),
    line('E', 'empty'),
    line('F', 'empty'),
    line('G', 'empty'),
    line('H', 'failed', { code: 'daily_limit' }),
  ]
  assert.equal(summaryLine(eight), 'Searched 8 indexers: 2 had releases, 5 had nothing for this title, 1 did not answer.')
})

test('the summary line uses the singular for one indexer and mentions skipped ones', () => {
  assert.equal(summaryLine([line('A', 'empty')]), 'Searched 1 indexer: 1 had nothing for this title.')
  assert.equal(
    summaryLine([line('A', 'found', { count: 1 }), line('B', 'skipped', { code: 'no_category' })]),
    'Searched 2 indexers: 1 had releases, 1 skipped.',
  )
})

test('each result says what happened in plain words', () => {
  assert.equal(resultText(line('A', 'found', { count: 12 })), '12 releases')
  assert.equal(resultText(line('A', 'found', { count: 1 })), '1 release')
  assert.equal(resultText(line('A', 'empty')), 'Nothing found')
  assert.equal(resultText(line('A', 'failed', { code: 'timeout' })), 'Did not answer: timed out')
  assert.equal(resultText(line('A', 'failed', { code: 'daily_limit' })), 'Did not answer: daily limit used up')
  assert.equal(resultText(line('A', 'failed', { code: 'error' })), 'Did not answer: returned an error')
  assert.equal(resultText(line('A', 'skipped', { code: 'no_category' })), 'Skipped: does not list this kind of release')
  assert.equal(resultText(line('A', 'skipped', { code: 'disabled' })), 'Skipped: turned off')
  assert.equal(resultText(line('A', 'skipped', { code: 'paused' })), 'Skipped: paused after repeated failures')
})

test('a failure without a known code still gets a reason', () => {
  assert.equal(why(line('A', 'failed')), 'returned an error')
  assert.equal(why(line('A', 'skipped', { reason: 'Because.' })), 'Because.')
})

test('the time is in seconds, and empty when the indexer was not asked', () => {
  assert.equal(seconds(0), '')
  assert.equal(seconds(420), '0.4 s')
  assert.equal(seconds(1500), '1.5 s')
  assert.equal(seconds(12400), '12 s')
})

test('the filter lists every indexer that was asked, with counts', () => {
  const releases = [{ indexerName: 'Lime' }, { indexerName: 'Lime' }, { indexerName: 'Extra' }]
  const searched = [line('YTS', 'empty'), line('Lime', 'found', { count: 2 }), line('Slug', 'failed'), line('Extra', 'found', { count: 1 }), line('Off', 'skipped')]
  assert.deepEqual(
    indexerChoices(releases, searched).map((c) => [c.value, c.count, c.failed]),
    [
      ['Extra', 1, false],
      ['Lime', 2, false],
      ['Slug', 0, true],
      ['YTS', 0, false],
    ],
  )
})

test('without the search details the filter lists the indexers of the releases', () => {
  const releases = [{ indexerName: 'B' }, { indexerName: 'A' }, { indexerName: 'B' }]
  assert.deepEqual(
    indexerChoices(releases).map((c) => [c.value, c.count]),
    [
      ['A', 1],
      ['B', 2],
    ],
  )
})

test('a FlareSolverr that does not answer and a page that is gone have their own words', () => {
  assert.equal(resultText(line('1337x', 'failed', { code: 'flaresolverr' })), "Did not answer: FlareSolverr isn't reachable")
  assert.equal(resultText(line('52BT', 'failed', { code: 'gone' })), 'Did not answer: the site says the page is gone')
  assert.equal(why(line('X', 'failed', { code: 'other' })), 'returned an error')
})

test('Soulseek has its own words and is named after the indexers in the summary', () => {
  const soul = (status: Searched['status'], extra: Partial<Searched> = {}) => line('Soulseek (slskd)', status, { kind: 'soulseek', ...extra })
  assert.equal(resultText(soul('found', { count: 3 })), '3 folders found')
  assert.equal(resultText(soul('found', { count: 1 })), '1 folder found')
  assert.equal(resultText(soul('empty')), 'Nothing found')
  assert.equal(resultText(soul('skipped', { code: 'music_off', reason: 'Music is switched off.' })), 'Music is switched off.')
  assert.equal(
    resultText(soul('skipped', { code: 'not_set_up', reason: 'Soulseek is not set up yet: the downloads folder is missing.' })),
    'Soulseek is not set up yet: the downloads folder is missing.',
  )
  assert.equal(resultText(soul('failed', { code: 'timeout', reason: 'it took too long' })), 'slskd did not answer: it took too long')
  assert.equal(
    resultText(soul('failed', { code: 'not_connected', reason: 'slskd is not connected to the Soulseek network (state: Disconnected).' })),
    'slskd is not connected to the Soulseek network (state: Disconnected).',
  )
  assert.equal(summaryLine([line('IPTorrents', 'empty'), soul('empty')]), 'Searched 1 indexer and Soulseek: 2 had nothing for this title.')
  assert.equal(summaryLine([line('A', 'empty'), line('B', 'empty'), soul('found', { count: 2 })]), 'Searched 2 indexers and Soulseek: 1 had releases, 2 had nothing for this title.')
  assert.equal(summaryLine([soul('empty')]), 'Searched Soulseek: 1 had nothing for this title.')
  assert.equal(summaryLine([line('A', 'empty'), soul('skipped', { code: 'not_set_up', reason: 'x' })]), 'Searched 1 indexer: 1 had nothing for this title, 1 skipped.')
  assert.equal(summaryLine([line('A', 'empty'), soul('failed', { code: 'error', reason: 'x' })]), 'Searched 1 indexer and Soulseek: 1 had nothing for this title, 1 did not answer.')
})

test('the indexer filter leaves out the Soulseek line', () => {
  const soul: Searched = line('Soulseek (slskd)', 'found', { kind: 'soulseek', count: 2 })
  const choices = indexerChoices([{ indexerName: 'Soulseek · alice' }], [line('A', 'empty'), soul])
  assert.deepEqual(choices.map((c) => c.value), ['A', 'Soulseek · alice'])
})
