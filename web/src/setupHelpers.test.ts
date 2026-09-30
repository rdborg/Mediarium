// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import * as h from './setupHelpers.ts'

test('composeLine', () => {
  const rows: [string, string, string][] = [
    ['/volume1/Media/Music', '/music', '- /volume1/Media/Music:/music'],
    ['  /volume1/Media  ', ' /data ', '- /volume1/Media:/data'],
    ['./data', '/data', '- ./data:/data'],
    ['/volume1/My Media', '/data', '- "/volume1/My Media:/data"'],
  ]
  for (const [host, inside, want] of rows) assert.equal(h.composeLine(host, inside), want)
  assert.equal(h.composeLineFor('ebooks', '/ebooks'), '- /volume1/Media/Ebooks:/ebooks')
})

test('commonParent', () => {
  const rows: [string[], string][] = [
    [['/data/Movies', '/data/downloads'], '/data'],
    [['/data/media/Movies', '/data/media/tv', '/data/media/downloads'], '/data/media'],
    [['/movies', '/downloads'], ''],
    [['/data/Movies', '/downloads'], ''],
    [['/data/movies'], '/data'],
    [['/data/movies/', '/data/tv/'], '/data'],
    [[], ''],
    [['movies', 'tv'], ''],
  ]
  for (const [paths, want] of rows) assert.equal(h.commonParent(paths), want, JSON.stringify(paths))
})

test('folder names follow the style of the folders already there', () => {
  assert.equal(h.pathInside('/data', 'music', '/data/Movies'), '/data/Music')
  assert.equal(h.pathInside('/data/', 'music', '/data/movies'), '/data/music')
  assert.equal(h.pathInside('/data', 'audiobooks', '/data/Movies'), '/data/Audiobooks')
  assert.equal(h.siblingName('ebooks', ''), 'ebooks')
})

test('the parent of the core folders', () => {
  assert.deepEqual(h.coreParent({ movies: '/data/Movies', tv: '/data/tv', downloads: '/data/downloads' }), { parent: '/data', sample: '/data/Movies' })
  assert.deepEqual(h.coreParent({ tv: '/data/tv', downloads: '/data/downloads' }), { parent: '/data', sample: '/data/tv' })
  assert.deepEqual(h.coreParent({ movies: '/movies', tv: '/tv', downloads: '/downloads' }), { parent: '', sample: '/movies' })
  assert.deepEqual(h.coreParent({}), { parent: '', sample: '' })
})

test('which folders are asked for', () => {
  const rows: [h.Chosen, h.FolderKind[]][] = [
    [{ movies: true, tv: true, music: false }, ['movies', 'tv', 'downloads']],
    [{ movies: true, tv: false, music: false }, ['movies', 'downloads']],
    [{ movies: false, tv: true, music: true }, ['tv', 'music', 'downloads']],
    [{ movies: true, tv: true, music: true }, ['movies', 'tv', 'music', 'downloads']],
    [{ movies: false, tv: false, music: true }, ['music', 'downloads']],
  ]
  for (const [c, want] of rows) assert.deepEqual(h.foldersToAsk(c), want, JSON.stringify(c))
})

test('phrases only name the chosen types', () => {
  const rows: [h.Chosen, string, string][] = [
    [{ movies: true, tv: true, music: false }, 'movies and TV shows', 'every movie and show'],
    [{ movies: true, tv: false, music: false }, 'movies', 'every movie'],
    [{ movies: false, tv: true, music: false }, 'TV shows', 'every show'],
    [{ movies: true, tv: true, music: true }, 'movies, TV shows and music', 'every movie and show'],
    [{ movies: false, tv: false, music: true }, 'music', ''],
  ]
  for (const [c, media, quality] of rows) {
    assert.equal(h.mediaPhrase(c), media)
    assert.equal(h.qualityPhrase(c), quality)
  }
  assert.equal(h.nothingChosen({ movies: false, tv: false, music: false }), true)
  assert.equal(h.nothingChosen({ movies: false, tv: false, music: true }), false)
  assert.equal(h.hasVideo({ movies: false, tv: false, music: true }), false)
  assert.equal(h.hasVideo({ movies: false, tv: true, music: false }), true)
})

test('the naming style follows the media player', () => {
  const rows: [h.MediaPlayer, string][] = [
    ['plex', 'plex'],
    ['jellyfin', 'jellyfin'],
    ['emby', 'jellyfin'],
    ['kodi', 'kodi'],
    ['other', 'plex'],
  ]
  for (const [player, want] of rows) assert.equal(h.namingPresetFor(player), want, player)
})

test('a known media server picks the player', () => {
  assert.equal(h.playerForServer([]), undefined)
  assert.equal(h.playerForServer(['jellyfin']), 'jellyfin')
  assert.equal(h.playerForServer(['emby', 'plex']), 'emby')
  assert.equal(h.playerForServer(['something']), undefined)
})

test('only changed folders are saved', () => {
  const inUse: h.FolderValues = { movies: '/movies', tv: '/data/tv', downloads: '/data/downloads' }
  const typed: h.FolderValues = { movies: '/data/Movies', tv: ' /data/tv ', downloads: '/data/downloads', music: '/data/Music' }
  assert.deepEqual(h.foldersToSave(['movies', 'tv', 'downloads'], typed, inUse), { movies: '/data/Movies' })
  // Music was not asked for here, so it is left alone; when it is asked for and new, it is saved.
  assert.deepEqual(h.foldersToSave(['movies', 'tv', 'music', 'downloads'], typed, inUse), { movies: '/data/Movies', music: '/data/Music' })
  // An empty box is never sent.
  assert.deepEqual(h.foldersToSave(['movies'], { movies: '  ' }, inUse), {})
})

test('a saved folder that differs from the mapped one is stale', () => {
  assert.equal(h.isStale('/movies', '/data/Movies'), true)
  assert.equal(h.isStale('/data/Movies', '/data/Movies'), false)
  assert.equal(h.isStale('', '/data/Movies'), false)
  assert.equal(h.isStale('/movies', undefined), false)
})
