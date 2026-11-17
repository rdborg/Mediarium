// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { albumOption, emptyTarget, matching, offeredFormats, targetFields, targetProblem } from './manualTarget.ts'

test('a movie needs only the movie', () => {
  assert.notEqual(targetProblem(emptyTarget('movie')), '')
  const g = { ...emptyTarget('movie'), movieId: 4 }
  assert.equal(targetProblem(g), '')
  assert.deepEqual(targetFields(g), [['movieId', '4']])
})

test('a show needs the show and a season, the episode may stay the whole season', () => {
  assert.notEqual(targetProblem(emptyTarget('show')), '')
  assert.notEqual(targetProblem({ ...emptyTarget('show'), seriesId: 2 }), '')
  const g = { ...emptyTarget('show'), seriesId: 2, season: '1' }
  assert.equal(targetProblem(g), '')
  assert.deepEqual(targetFields(g), [['seriesId', '2'], ['season', '1'], ['episode', '0']])
})

test('an album needs the album', () => {
  assert.notEqual(targetProblem(emptyTarget('album')), '')
  const g = { ...emptyTarget('album'), albumId: 9 }
  assert.equal(targetProblem(g), '')
  assert.deepEqual(targetFields(g), [['albumId', '9']])
})

test('a book needs the book and one of its formats', () => {
  assert.notEqual(targetProblem(emptyTarget('book')), '')
  assert.notEqual(targetProblem({ ...emptyTarget('book'), bookId: 3 }), '')
  const g = { ...emptyTarget('book'), bookId: 3, format: 'audiobook' as const }
  assert.equal(targetProblem(g), '')
  assert.deepEqual(targetFields(g), [['bookId', '3'], ['format', 'audiobook']])
})

test('only the formats a book is kept in, in a media type that is on, are offered', () => {
  const both = { ebook: { wanted: true }, audiobook: { wanted: true } }
  assert.deepEqual(offeredFormats(both, true, true), ['ebook', 'audiobook'])
  assert.deepEqual(offeredFormats(both, true, false), ['ebook'])
  assert.deepEqual(offeredFormats(both, false, true), ['audiobook'])
  assert.deepEqual(offeredFormats({ ebook: { wanted: false }, audiobook: { wanted: true } }, true, true), ['audiobook'])
  assert.deepEqual(offeredFormats(both, false, false), [])
})

test('the library list is filtered without caring about case or punctuation, and capped', () => {
  const items = ["Don't Stop", 'Stop Making Sense', 'Heat']
  assert.deepEqual(matching(items, 'dont stop', (s) => s), ["Don't Stop"])
  assert.deepEqual(matching(items, 'STOP', (s) => s), ["Don't Stop", 'Stop Making Sense'])
  assert.deepEqual(matching(items, '  ', (s) => s), items)
  assert.deepEqual(matching(items, '', (s) => s, 2), ["Don't Stop", 'Stop Making Sense'])
})

test('an album is listed with its year, and its type when it is not a plain album', () => {
  assert.equal(albumOption({ title: 'First Album', year: 1999, type: 'album' }), 'First Album (1999)')
  assert.equal(albumOption({ title: 'Small One', year: 2001, type: 'ep' }), 'Small One (2001) · EP')
  assert.equal(albumOption({ title: 'No Year' }), 'No Year')
})
