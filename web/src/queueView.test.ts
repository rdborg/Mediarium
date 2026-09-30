// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bulkState, groupQueue, linePlace, sortQueue, stateNote, statusLabel, stillWaiting } from './queueView.ts'
import type { QueueItem } from './api.ts'

let nextId = 1
function item(over: Partial<QueueItem>): QueueItem {
  return {
    id: nextId++,
    movieId: 1,
    title: 'Some Movie',
    protocol: 'usenet',
    releaseTitle: 'Some.Movie.2020.1080p',
    sizeBytes: 1000,
    status: 'downloading',
    progressPct: 10,
    addedAt: '2026-01-01T10:00:00.000Z',
    ...over,
  }
}

test('the queue reads in progress, then failed, then the rest', () => {
  const list = [
    item({ id: 1, status: 'completed', completedAt: '2026-01-01T12:00:00Z' }),
    item({ id: 2, status: 'stopped', completedAt: '2026-01-01T11:00:00Z' }),
    item({ id: 3, status: 'failed', completedAt: '2026-01-01T10:30:00Z' }),
    item({ id: 4, status: 'paused' }),
    item({ id: 5, status: 'queued' }),
    item({ id: 6, status: 'importing' }),
    item({ id: 7, status: 'conflict' }),
    item({ id: 8, status: 'downloading' }),
  ]
  assert.deepEqual(
    sortQueue(list).map((q) => q.id),
    [6, 8, 5, 4, 3, 7, 2, 1],
  )
})

test('in progress: the one that started first is on top, and progress never changes the order', () => {
  const a = item({ id: 10, status: 'downloading', addedAt: '2026-01-01T09:00:00.000Z', progressPct: 5 })
  const b = item({ id: 11, status: 'downloading', addedAt: '2026-01-01T08:00:00.000Z', progressPct: 90 })
  const c = item({ id: 12, status: 'importing', addedAt: '2026-01-01T09:30:00.000Z', progressPct: 100 })
  assert.deepEqual(
    sortQueue([a, b, c]).map((q) => q.id),
    [11, 10, 12],
  )
  // The same items after a poll with different progress and in another order.
  const later = [{ ...c, progressPct: 100 }, { ...a, progressPct: 60 }, { ...b, progressPct: 95 }]
  assert.deepEqual(
    sortQueue(later).map((q) => q.id),
    [11, 10, 12],
  )
})

test('an item that has the same start time keeps a fixed place by id', () => {
  const list = [item({ id: 22, addedAt: '2026-01-01T09:00:00Z' }), item({ id: 21, addedAt: '2026-01-01T09:00:00Z' })]
  assert.deepEqual(
    sortQueue(list).map((q) => q.id),
    [21, 22],
  )
})

test('failed items: newest failure first', () => {
  const list = [
    item({ id: 30, status: 'failed', addedAt: '2026-01-01T01:00:00Z', completedAt: '2026-01-01T05:00:00Z' }),
    item({ id: 31, status: 'failed', addedAt: '2026-01-01T02:00:00Z', completedAt: '2026-01-01T09:00:00Z' }),
    item({ id: 32, status: 'failed', addedAt: '2026-01-01T07:00:00Z' }),
  ]
  assert.deepEqual(
    sortQueue(list).map((q) => q.id),
    [31, 32, 30],
  )
})

test('groups match the sections on the page', () => {
  const g = groupQueue([
    item({ id: 1, status: 'paused' }),
    item({ id: 2, status: 'failed' }),
    item({ id: 3, status: 'stopped' }),
    item({ id: 4, status: 'conflict' }),
    item({ id: 5, status: 'completed' }),
    item({ id: 6, status: 'downloading' }),
  ])
  assert.deepEqual(g.inProgress.map((q) => q.id), [6, 1])
  assert.deepEqual(g.failed.map((q) => q.id), [2])
  assert.deepEqual(g.decision.map((q) => q.id), [4])
  assert.deepEqual(g.stopped.map((q) => q.id), [3])
  assert.deepEqual(g.finished.map((q) => q.id), [5])
})

test('badge wording', () => {
  const rows: [Parameters<typeof statusLabel>[0], string][] = [
    [{ status: 'downloading' }, 'Downloading'],
    [{ status: 'queued' }, 'Waiting in line'],
    [{ status: 'importing' }, 'Importing'],
    [{ status: 'paused' }, 'Paused'],
    [{ status: 'paused', interrupted: true }, 'Partly downloaded, stopped'],
    [{ status: 'stopped' }, 'Stopped'],
    [{ status: 'failed' }, 'Failed'],
    [{ status: 'downloading', pending: 'pausing' }, 'Pausing…'],
    [{ status: 'importing', pending: 'stopping' }, 'Stopping…'],
  ]
  for (const [q, want] of rows) assert.equal(statusLabel(q), want, JSON.stringify(q))
})

test('notes under a paused or stopped download say what happened', () => {
  assert.match(stateNote({ status: 'paused', progressPct: 40, keptFiles: true }), /Press Resume to carry on/)
  assert.match(stateNote({ status: 'paused', interrupted: true, progressPct: 40, keptFiles: true }), /restarted/)
  assert.match(stateNote({ status: 'paused', interrupted: true, progressPct: 0, keptFiles: false }), /before this started/)
  assert.equal(stateNote({ status: 'stopped', progressPct: 20, keptFiles: false }), '')
  assert.match(stateNote({ status: 'stopped', progressPct: 20, keptFiles: true }), /still on disk/)
  assert.equal(stateNote({ status: 'downloading', progressPct: 20 }), '')
})

test('the two buttons at the top only work when there is something to act on', () => {
  assert.deepEqual(bulkState([]), { canPause: false, canResume: false })
  assert.deepEqual(bulkState([item({ status: 'downloading' })]), { canPause: true, canResume: false })
  assert.deepEqual(bulkState([item({ status: 'paused' })]), { canPause: false, canResume: true })
  assert.deepEqual(bulkState([item({ status: 'queued' }), item({ status: 'paused' })]), { canPause: true, canResume: true })
  assert.deepEqual(bulkState([item({ status: 'downloading', pending: 'pausing' })]), { canPause: false, canResume: false })
  assert.deepEqual(bulkState([item({ status: 'failed' }), item({ status: 'stopped' })]), { canPause: false, canResume: false })
})

test('a title with a download in the list is not also waiting for a release', () => {
  const waiting = [
    { title: 'Has a paused download', movieId: 1 },
    { title: 'Has a failed download only', movieId: 2 },
    { title: 'Nothing yet', movieId: 3 },
    { title: 'Album with a download', albumId: 9 },
    { title: 'Another album', albumId: 10 },
  ]
  const queue = [
    item({ movieId: 1, status: 'paused' }),
    item({ movieId: 2, status: 'failed' }),
    item({ movieId: 0, albumId: 9, status: 'downloading' }),
    item({ movieId: 3, status: 'completed' }),
  ]
  assert.deepEqual(
    stillWaiting(waiting, queue).map((w) => w.title),
    ['Has a failed download only', 'Nothing yet', 'Another album'],
  )
})

test('waiting downloads are listed in the order they will start, right after the ones running', () => {
  const list = [
    item({ id: 41, status: 'failed', completedAt: '2026-01-01T10:30:00Z' }),
    item({ id: 42, status: 'queued', queuePosition: 3, addedAt: '2026-01-01T08:00:00.000Z' }),
    item({ id: 43, status: 'queued', queuePosition: 1, addedAt: '2026-01-01T09:00:00.000Z' }),
    item({ id: 44, status: 'downloading' }),
    item({ id: 45, status: 'queued', queuePosition: 2, addedAt: '2026-01-01T07:00:00.000Z' }),
    item({ id: 46, status: 'paused' }),
  ]
  // A person's download added later can be next in line, so the order is the
  // position the server gives, not the time it was added.
  assert.deepEqual(
    sortQueue(list).map((q) => q.id),
    [44, 43, 45, 42, 46, 41],
  )
})

test('the place in line reads in plain words', () => {
  const rows: [number | undefined, string][] = [
    [undefined, 'Waiting in line'],
    [0, 'Waiting in line'],
    [1, 'Next in line'],
    [2, '2nd in line'],
    [3, '3rd in line'],
    [4, '4th in line'],
    [11, '11th in line'],
    [12, '12th in line'],
    [13, '13th in line'],
    [21, '21st in line'],
    [22, '22nd in line'],
    [23, '23rd in line'],
    [101, '101st in line'],
    [111, '111th in line'],
  ]
  for (const [n, want] of rows) assert.equal(linePlace(n), want, String(n))
  assert.equal(stateNote({ status: 'queued', progressPct: 0, queuePosition: 3 }), '3rd in line')
  assert.equal(stateNote({ status: 'queued', progressPct: 0 }), '')
})
