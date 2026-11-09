// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { MusicHit } from './api.ts'
import { AnswerMemo, asAnswer, countHits, filterHits, Gate, hitKey, searchKey } from './musicSearch.ts'

function hit(kind: MusicHit['kind'], mbid: string, song?: string): MusicHit {
  return { kind, rank: 1, name: mbid, mbid, title: mbid, type: 'album', artistName: 'A', artistMbid: 'a', inLibrary: false, song }
}

const hits = [hit('album', '1'), hit('artist', '2'), hit('song', '3', 'x'), hit('album', '4')]

test('a filter keeps the order and only the kind asked for', () => {
  assert.equal(filterHits(hits, 'all').length, 4)
  assert.deepEqual(filterHits(hits, 'album').map((h) => h.mbid), ['1', '4'])
  assert.deepEqual(filterHits(hits, 'song').map((h) => h.mbid), ['3'])
  assert.deepEqual(filterHits([], 'artist'), [])
})

test('the filters count every kind', () => {
  assert.deepEqual(countHits(hits), { all: 4, artist: 1, album: 2, song: 1 })
})

test('the same album found as an album and through a song are two rows', () => {
  assert.notEqual(hitKey(hit('album', '1')), hitKey(hit('song', '1', 'x')))
})

test('an answer is read from the new shape, a plain list or nothing', () => {
  assert.equal(asAnswer([hit('album', '1')]).items.length, 1)
  assert.deepEqual(asAnswer({ items: [], note: 'n' }), { items: [], note: 'n' })
  assert.deepEqual(asAnswer(null).items, [])
})

test('the same search typed with other case, accents or spaces is the same search', () => {
  assert.equal(searchKey('  Beyoncé   LEMONADE '), 'beyonce lemonade')
})

test('an answer that comes back after a newer search started is dropped', () => {
  const g = new Gate()
  const first = g.next()
  const second = g.next()
  assert.equal(g.current(first), false) // the first answer arrives last: ignored
  assert.equal(g.current(second), true)
})

test('the same words give the same list again', () => {
  const m = new AnswerMemo()
  m.put('Gul Anuv Jain', { items: [hit('song', '1', 'Gul')] })
  assert.equal(m.get('gul  anuv jain')?.items.length, 1)
})

test('an incomplete or empty answer is not kept, so asking again can fill the gap', () => {
  const m = new AnswerMemo()
  m.put('a', { items: [hit('album', '1')], note: "Couldn't search songs" })
  m.put('b', { items: [] })
  assert.equal(m.get('a'), undefined)
  assert.equal(m.get('b'), undefined)
})

test('answers are forgotten after a while and when there are too many', () => {
  let now = 0
  const m = new AnswerMemo(1000, 2, () => now)
  m.put('a', { items: [hit('album', '1')] })
  now = 1500
  assert.equal(m.get('a'), undefined)
  m.put('b', { items: [hit('album', '2')] })
  m.put('c', { items: [hit('album', '3')] })
  m.put('d', { items: [hit('album', '4')] })
  assert.equal(m.get('b'), undefined)
  assert.ok(m.get('d'))
})
