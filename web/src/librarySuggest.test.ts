import { test } from 'node:test'
import assert from 'node:assert/strict'
import { hitCommands, hitHint, hitPath, type LibraryHit } from './librarySuggest.ts'

test('each kind opens its own page', () => {
  const cases: [LibraryHit, string][] = [
    [{ kind: 'movie', id: 4, tmdbId: 11, title: 'Star Wars' }, '/title/11'],
    [{ kind: 'tv', id: 7, tmdbId: 1396, title: 'Breaking Bad' }, '/series/7'],
    [{ kind: 'book', id: 3, title: 'Dune' }, '/book/3'],
    [{ kind: 'artist', id: 9, title: 'Björk' }, '/music/artist/9'],
    [{ kind: 'album', id: 12, title: 'Homogenic' }, '/music/album/12'],
    [{ kind: 'movie', id: 4, title: 'No TMDB id' }, ''],
  ]
  for (const [hit, want] of cases) assert.equal(hitPath(hit), want, hit.title)
})

test('the hint says what it is and the year or the author', () => {
  assert.equal(hitHint({ kind: 'movie', id: 1, tmdbId: 1, title: 'x', year: 1977 }), 'Movie · 1977')
  assert.equal(hitHint({ kind: 'book', id: 1, title: 'x', subtitle: 'Frank Herbert' }), 'Book · Frank Herbert')
  assert.equal(hitHint({ kind: 'artist', id: 1, title: 'x' }), 'Artist')
  assert.equal(hitHint({ kind: 'tv', id: 1, title: 'x', year: 0 }), 'TV show')
})

test('rows keep the order of the answer and leave out what cannot be opened', () => {
  const rows = hitCommands([
    { kind: 'movie', id: 1, tmdbId: 5, title: 'Alien', year: 1979 },
    { kind: 'movie', id: 2, title: 'Broken' },
    { kind: 'album', id: 3, title: 'Homogenic', subtitle: 'Björk' },
    { kind: 'book', id: 4, title: '' },
  ])
  assert.deepEqual(
    rows.map((r) => [r.id, r.label, r.to]),
    [
      ['lib-movie-1', 'Alien', '/title/5'],
      ['lib-album-3', 'Homogenic', '/music/album/3'],
    ],
  )
})
