// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ageOf, defaultOrder, filterReleases, indexersIn, isFreeleech, qualitiesIn, resolutionOf, scoreOf, sortReleases, sortSubtitles, subtitleFit, subtitleRating, subtitleSite, type SortableRelease } from './releaseSort.ts'

const base = { protocol: 'usenet', sizeBytes: 0 }
const list: SortableRelease[] = [
  { ...base, title: 'Film.2024.1080p.WEB-DL', indexerName: 'Beta', resolution: '1080p', sizeBytes: 8e9 },
  { ...base, title: 'Film.2024.2160p.BluRay', indexerName: 'Alpha', resolution: '2160p', sizeBytes: 40e9 },
  { ...base, title: 'Film.2024.720p.HDTV', indexerName: 'Beta', resolution: '720p', sizeBytes: 2e9, protocol: 'torrent', seeders: 50 },
  { ...base, title: 'Film.2024.CAMRip', indexerName: 'Alpha', sizeBytes: 1e9, protocol: 'torrent', seeders: 3 },
]

test('quality sorts by resolution, biggest first, and keeps ties in order', () => {
  assert.deepEqual(sortReleases(list, 'quality', -1).map((r) => resolutionOf(r)), [2160, 1080, 720, 0])
  assert.deepEqual(sortReleases(list, 'quality', 1).map((r) => resolutionOf(r)), [0, 720, 1080, 2160])
  const same = [list[0], { ...list[0], title: 'Second' }]
  assert.deepEqual(sortReleases(same, 'quality', -1).map((r) => r.title), [list[0].title, 'Second'])
})

test('size, seeders and names sort the way people read them', () => {
  assert.deepEqual(sortReleases(list, 'size', -1).map((r) => r.sizeBytes), [40e9, 8e9, 2e9, 1e9])
  // Usenet releases have no seeders and go last when the most seeded come first.
  assert.deepEqual(sortReleases(list, 'seeders', -1).map((r) => r.seeders ?? null), [50, 3, null, null])
  assert.equal(sortReleases(list, 'indexer', 1)[0].indexerName, 'Alpha')
  assert.equal(sortReleases(list, null, 1), list)
})

test('the list starts with the best match, then the highest score, then the order the server gave', () => {
  const r = (title: string, formatScore?: number, best?: boolean): SortableRelease => ({ ...base, title, indexerName: 'A', formatScore, best })
  const scored = [r('a', 0), r('b', 300), r('c', -50), r('d', 300), r('e', 100, true)]
  assert.deepEqual(defaultOrder(scored).map((x) => x.title), ['e', 'b', 'd', 'a', 'c'])
  // A list that is not scored and has no marked release is left alone.
  assert.equal(defaultOrder(list), list)
})

test('the freeleech filter keeps only releases the tracker counts as free', () => {
  const r = (title: string, discount?: number): SortableRelease => ({ ...base, title, indexerName: 'A', discount })
  const mixed = [r('a', 100), r('b', 50), r('c'), r('d', 0), r('e', 100)]
  assert.deepEqual(filterReleases(mixed, { text: '', indexer: '', quality: '', freeleech: true }).map((x) => x.title), ['a', 'e'])
  assert.equal(filterReleases(mixed, { text: '', indexer: '', quality: '', freeleech: false }).length, 5)
  assert.equal(filterReleases(mixed, { text: '', indexer: '', quality: '' }).length, 5)
  assert.deepEqual(filterReleases(mixed, { text: 'e', indexer: '', quality: '', freeleech: true }).map((x) => x.title), ['e'])
  assert.equal(isFreeleech({ discount: 99 }), false)
  assert.equal(isFreeleech({}), false)
})

test('filters combine: indexer, quality and words in the title', () => {
  assert.equal(filterReleases(list, { text: '', indexer: 'Beta', quality: '' }).length, 2)
  assert.equal(filterReleases(list, { text: '', indexer: '', quality: '2160p' }).length, 1)
  assert.equal(filterReleases(list, { text: '', indexer: '', quality: 'other' }).length, 1)
  assert.equal(filterReleases(list, { text: 'film 1080P', indexer: '', quality: '' }).length, 1)
  assert.equal(filterReleases(list, { text: 'nothing like this', indexer: '', quality: '' }).length, 0)
})

test('the filter choices come from the results', () => {
  assert.deepEqual(indexersIn(list), ['Alpha', 'Beta'])
  assert.deepEqual(qualitiesIn(list), ['2160p', '1080p', '720p', 'other'])
})

test('subtitles sort by fit, rating and name; add-on and SubDL ratings count as none', () => {
  const subs = [
    { release: 'b', rating: 8, score: 70 },
    { release: 'a', rating: 5, score: 90, source: 'subdl' },
    { release: 'c', rating: 9, score: 10 },
  ]
  assert.deepEqual(sortSubtitles(subs, 'fit', -1).map((s) => s.release), ['a', 'b', 'c'])
  assert.deepEqual(sortSubtitles(subs, 'rating', -1).map((s) => s.release), ['c', 'b', 'a'])
  assert.deepEqual(sortSubtitles(subs, 'release', 1).map((s) => s.release), ['a', 'b', 'c'])
})

test('age sorts by when the release was posted, newest first, and unknown last', () => {
  const now = Date.parse('2026-10-12T12:00:00Z')
  const rel = (title: string, publishDate?: string): SortableRelease => ({ ...base, title, indexerName: 'X', publishDate })
  const mixed = [rel('old', '2026-09-01T00:00:00Z'), rel('none'), rel('new', '2026-10-12T09:00:00Z'), rel('zero', '0001-01-01T00:00:00Z'), rel('mid', '2026-10-10T00:00:00Z')]
  assert.deepEqual(sortReleases(mixed, 'age', -1).map((r) => r.title), ['new', 'mid', 'old', 'none', 'zero'])
  assert.deepEqual(sortReleases(mixed, 'age', 1).map((r) => r.title), ['none', 'zero', 'old', 'mid', 'new'])
  assert.equal(ageOf(rel('a', '2026-10-12T11:30:00Z'), now), '<1h')
  assert.equal(ageOf(rel('a', '2026-10-12T07:00:00Z'), now), '5h')
  assert.equal(ageOf(rel('a', '2026-10-09T12:00:00Z'), now), '3d')
  assert.equal(ageOf(rel('a', '2026-07-12T12:00:00Z'), now), '3mo')
  assert.equal(ageOf(rel('a', '2024-01-01T00:00:00Z'), now), '2y')
  assert.equal(ageOf(rel('a'), now), '')
  assert.equal(ageOf(rel('a', '0001-01-01T00:00:00Z'), now), '')
})

test('a subtitle shows its own site or add-on name, and only the rating and fit it has', () => {
  const os = { release: 'a', rating: 7.5, score: 61.4 }
  const sdl = { release: 'b', rating: 0, score: 130, source: 'subdl' }
  const addon = { release: 'c', rating: 0, score: 130.2, source: 'addon', sourceName: 'YIFY Subtitles' }
  const nameless = { release: 'd', rating: 0, score: 0, source: 'addon' }
  assert.equal(subtitleSite(os), 'OpenSubtitles')
  assert.equal(subtitleSite(sdl), 'SubDL')
  assert.equal(subtitleSite(addon), 'YIFY Subtitles')
  assert.equal(subtitleSite(nameless), '')
  assert.equal(subtitleRating(os), '7.5')
  assert.equal(subtitleRating(addon), '')
  assert.equal(subtitleFit(addon), '130')
  assert.equal(subtitleFit(nameless), '')
})

test('the score column sorts by the whole score the pick uses, not the formats alone', () => {
  const r = (title: string, score?: number, formatScore?: number): SortableRelease => ({ ...base, title, indexerName: 'A', score, formatScore })
  // "b" has fewer format points but a preferred term lifts it above "a".
  const rows = [r('a', 100, 100), r('b', 130, 80), r('c', 40, 40)]
  assert.deepEqual(sortReleases(rows, 'score', -1).map((x) => x.title), ['b', 'a', 'c'])
  assert.deepEqual(defaultOrder(rows).map((x) => x.title), ['b', 'a', 'c'])
  assert.equal(scoreOf(r('x', undefined, 7)), 7)
})
