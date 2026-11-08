import assert from 'node:assert/strict'
import { test } from 'node:test'
import { artworkThroughServer, cleanBase, pictureSrcSet, prefixed, rebasePictures, setArtworkThroughServer, unprefixed, viaServer, BASE_PATH, withBase } from './base.ts'

test('cleanBase keeps a path prefix and drops anything else', () => {
  assert.equal(cleanBase('/mediarium'), '/mediarium')
  assert.equal(cleanBase(' /mediarium/ '), '/mediarium')
  assert.equal(cleanBase('/apps/media///'), '/apps/media')
  assert.equal(cleanBase(''), '')
  assert.equal(cleanBase('/'), '')
  assert.equal(cleanBase(null), '')
  assert.equal(cleanBase(undefined), '')
  assert.equal(cleanBase('mediarium'), '')
  assert.equal(cleanBase('https://evil.example/x'), '')
})

test('prefixed adds the prefix to addresses on this server only', () => {
  assert.equal(prefixed('/m', '/api/version'), '/m/api/version')
  assert.equal(prefixed('/m', '/'), '/m/')
  assert.equal(prefixed('/m', '/push-sw.js'), '/m/push-sw.js')
  assert.equal(prefixed('/m', 'relative/path'), 'relative/path')
  assert.equal(prefixed('/m', '//cdn.example.com/x'), '//cdn.example.com/x')
  assert.equal(prefixed('/m', 'https://example.com/x'), 'https://example.com/x')
  assert.equal(prefixed('', '/api/version'), '/api/version')
})

test('unprefixed takes the prefix off again', () => {
  assert.equal(unprefixed('/m', '/m/api/version'), '/api/version')
  assert.equal(unprefixed('/m', '/m'), '/')
  assert.equal(unprefixed('/m', '/m2/x'), '/m2/x')
  assert.equal(unprefixed('/m', '/other'), '/other')
  assert.equal(unprefixed('', '/api/version'), '/api/version')
})

test('rebasePictures puts the prefix on picture addresses from this server', () => {
  const data = {
    coverUrl: '/api/book-covers/12-M.jpg',
    posterUrl: 'https://image.tmdb.org/t/p/w342/x_abc1.jpg',
    title: '/api/not-a-picture',
    items: [{ imageUrl: '/api/music/artists/3/cover' }, { coverUrl: '/api/books/4/file-cover', entries: [{ coverUrl: '/api/music/covers/release-group/abc' }] }],
    empty: null,
    noCover: '',
  }
  const out = rebasePictures('/m', data)
  assert.equal(out, data)
  assert.equal(out.coverUrl, '/m/api/book-covers/12-M.jpg')
  assert.equal(out.posterUrl, '/m/api/img/tmdb/w342/x_abc1.jpg')
  assert.equal(out.title, '/api/not-a-picture')
  assert.equal(out.items[0].imageUrl, '/m/api/music/artists/3/cover')
  assert.equal(out.items[1].coverUrl, '/m/api/books/4/file-cover')
  assert.equal(out.items[1].entries?.[0].coverUrl, '/m/api/music/covers/release-group/abc')
  assert.deepEqual(rebasePictures('', { coverUrl: '/api/x' }), { coverUrl: '/api/x' })
  assert.equal(rebasePictures('/m', null), null)
  assert.deepEqual(rebasePictures('/m', [{ posterUrl: '/api/p' }]), [{ posterUrl: '/m/api/p' }])
})

test('TMDB pictures are sent through this server, whatever the field is called', () => {
  assert.equal(artworkThroughServer(), true)
  const data = {
    posterUrl: 'https://image.tmdb.org/t/p/w500/abcd1234.jpg',
    poster: 'https://image.tmdb.org/t/p/original/abcd1234.png',
    nested: [{ profileUrl: 'https://image.tmdb.org/t/p/w185/prof_01-x.jpg' }],
    list: ['https://image.tmdb.org/t/p/w92/small.jpg', 'https://example.com/a.jpg'],
    other: 'https://example.com/t/p/w500/abcd1234.jpg',
    odd: 'https://image.tmdb.org/t/p/w500/../../secret.jpg',
    wrongSize: 'https://image.tmdb.org/t/p/w999/abcd1234.jpg',
    http: 'http://image.tmdb.org/t/p/w500/abcd1234.jpg',
    withQuery: 'https://image.tmdb.org/t/p/w500/abcd1234.jpg?x=1',
  }
  const out = rebasePictures('', data)
  assert.equal(out.posterUrl, '/api/img/tmdb/w500/abcd1234.jpg')
  assert.equal(out.poster, '/api/img/tmdb/original/abcd1234.png')
  assert.equal(out.nested[0].profileUrl, '/api/img/tmdb/w185/prof_01-x.jpg')
  assert.deepEqual(out.list, ['/api/img/tmdb/w92/small.jpg', 'https://example.com/a.jpg'])
  for (const k of ['other', 'odd', 'wrongSize', 'http', 'withQuery'] as const) assert.equal(out[k], data[k], k)
  assert.equal(viaServer('/m', 'https://image.tmdb.org/t/p/w780/abcd1234.jpg'), '/m/api/img/tmdb/w780/abcd1234.jpg')
})

test('switching artwork through the server off restores direct loading', () => {
  try {
    setArtworkThroughServer(false)
    assert.equal(artworkThroughServer(), false)
    const out = rebasePictures('/m', { posterUrl: 'https://image.tmdb.org/t/p/w342/x_abc.jpg', coverUrl: '/api/book-covers/1-M.jpg' })
    assert.equal(out.posterUrl, 'https://image.tmdb.org/t/p/w342/x_abc.jpg')
    assert.equal(out.coverUrl, '/m/api/book-covers/1-M.jpg')
  } finally {
    setArtworkThroughServer(true)
  }
  assert.equal(artworkThroughServer(), true)
})

test('pictureSrcSet offers the sizes of a picture this server keeps', () => {
  assert.equal(pictureSrcSet('/api/img/tmdb/w1280/abcd1234.jpg', [780, 1280]), '/api/img/tmdb/w780/abcd1234.jpg 780w, /api/img/tmdb/w1280/abcd1234.jpg 1280w')
  assert.equal(pictureSrcSet('/m/api/img/tmdb/w500/abcd1234.jpg', [342, 500]), '/m/api/img/tmdb/w342/abcd1234.jpg 342w, /m/api/img/tmdb/w500/abcd1234.jpg 500w')
  assert.equal(pictureSrcSet('https://image.tmdb.org/t/p/w500/abcd1234.jpg', [342, 500]), undefined)
  assert.equal(pictureSrcSet('/api/book-covers/1-M.jpg', [342]), undefined)
})

test('book edition thumbnails are prefixed by the dialog itself', () => {
  // The editions come in a field called "url", which rebasePictures leaves alone,
  // so ChangeCoverDialog puts the prefix on with withBase (here the same as prefixed).
  const editions = { editions: [{ coverId: 1, url: '/api/book-covers/7-S.jpg' }, { coverId: 2, url: 'https://covers.openlibrary.org/b/id/2-S.jpg' }] }
  assert.deepEqual(rebasePictures('/m', structuredClone(editions)), editions)
  assert.equal(prefixed('/m', editions.editions[0].url), '/m/api/book-covers/7-S.jpg')
  assert.equal(prefixed('/m', editions.editions[1].url), editions.editions[1].url)
})

test('with no page around there is no prefix', () => {
  assert.equal(BASE_PATH, '')
  assert.equal(withBase('/api/x'), '/api/x')
})
