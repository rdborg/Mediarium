import assert from 'node:assert/strict'
import { test } from 'node:test'
import { outsideService, slowServiceMessage } from './outsideService.ts'

test('reads that wait on an outside service are named', () => {
  const rows: [string, string][] = [
    ['/discover/trending', 'The movie database (TMDB)'],
    ['/discover/list?kind=movie&list=similar', 'The movie database (TMDB)'],
    ['/discover/import-list?url=x', 'Trakt'],
    ['/tmdb/movies/603', 'The movie database (TMDB)'],
    ['/music/discover?list=popular', 'MusicBrainz'],
    ['/music/albums/4/cover', 'The Cover Art Archive'],
    ['/books/discover?list=trending', 'Open Library'],
    ['/books/search?q=dune', 'Open Library'],
    ['/book-authors/OL1A/works', 'Open Library'],
    ['/search?q=x', 'Your indexers'],
    ['/movies/12/search', 'Your indexers'],
    ['/books/3/releases?format=ebook', 'Your indexers'],
    ['/books/3/links', 'Your media server'],
    ['/movies/5/subtitles', 'OpenSubtitles'],
  ]
  for (const [path, name] of rows) assert.equal(outsideService(path), name, path)
})

test("Mediarium's own reads are not", () => {
  for (const path of ['/queue', '/movies', '/movies/12', '/books', '/books/3', '/books/3/progress?format=ebook', '/books/progress', '/dashboard', '/music/artists', '/searchable', '/subtitles/quota']) {
    assert.equal(outsideService(path), '', path)
  }
})

test('the message names the service', () => {
  assert.match(slowServiceMessage('Open Library'), /^Open Library is slow to answer/)
})
