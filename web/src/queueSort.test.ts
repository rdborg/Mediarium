import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { QueueItem } from './api.ts'
import { DEFAULT_QUEUE_SORT, nextSamples, paceOf, readQueueSort, rememberQueueSort, sortQueueBy, transferBasis, type KeyValueStore, type Pace, type QueueSortKey } from './queueSort.ts'

const item = (id: number, o: Partial<QueueItem> = {}): QueueItem =>
  ({ id, movieId: id, title: `T${id}`, protocol: 'usenet', releaseTitle: `r${id}`, sizeBytes: 1000, status: 'downloading', progressPct: 0, ...o }) as QueueItem

const a = item(1, { title: 'The Zebra', sizeBytes: 300, progressPct: 20, addedAt: '2026-01-01T10:00:00Z' })
const b = item(2, { title: 'Apple', sizeBytes: 900, progressPct: 80, addedAt: '2026-01-03T10:00:00Z' })
const c = item(3, { title: 'mango', sizeBytes: 600, progressPct: 50, addedAt: '2026-01-02T10:00:00Z' })
const d = item(4, { title: 'Paused one', status: 'paused', sizeBytes: 0, progressPct: 10 }) // no size, no date, no speed
const list = [a, b, c, d]
const pace = new Map<number, Pace>([
  [1, { rate: 5, etaSec: 100 }],
  [2, { rate: 50, etaSec: 10 }],
  [3, { rate: 20, etaSec: 40 }],
])

test('sortQueueBy: each key in its own order, and turned round', () => {
  const cases: { key: QueueSortKey; own: number[]; reversed: number[] }[] = [
    { key: 'speed', own: [2, 3, 1, 4], reversed: [1, 3, 2, 4] }, // fastest first; no speed is last either way
    { key: 'progress', own: [2, 3, 1, 4], reversed: [4, 1, 3, 2] }, // most done first
    { key: 'size', own: [2, 3, 1, 4], reversed: [1, 3, 2, 4] }, // largest first; no size is last
    { key: 'eta', own: [2, 3, 1, 4], reversed: [1, 3, 2, 4] }, // finishing soonest first
    { key: 'added', own: [2, 3, 1, 4], reversed: [1, 3, 2, 4] }, // newest first; no date is last
    { key: 'name', own: [2, 3, 4, 1], reversed: [1, 4, 3, 2] }, // A to Z, ignoring case and a leading "The"
    { key: 'default', own: [1, 2, 3, 4], reversed: [1, 2, 3, 4] }, // the page's own order, reversing does nothing
  ]
  for (const t of cases) {
    assert.deepEqual(sortQueueBy(list, { key: t.key, reverse: false }, pace).map((q) => q.id), t.own, `${t.key}`)
    assert.deepEqual(sortQueueBy(list, { key: t.key, reverse: true }, pace).map((q) => q.id), t.reversed, `${t.key} reversed`)
  }
})

test('sortQueueBy: peers puts the most connected first, and a download that says nothing last', () => {
  const p = [item(1, { peers: 5 }), item(2, { peers: 12 }), item(3, { peers: 0 }), item(4)]
  assert.deepEqual(sortQueueBy(p, { key: 'peers', reverse: false }).map((q) => q.id), [2, 1, 3, 4])
  assert.deepEqual(sortQueueBy(p, { key: 'peers', reverse: true }).map((q) => q.id), [3, 1, 2, 4])
})

test('sortQueueBy: the default keeps the order it was given, and the input is never changed', () => {
  const input = [c, a, b]
  assert.deepEqual(sortQueueBy(input, DEFAULT_QUEUE_SORT).map((q) => q.id), [3, 1, 2])
  sortQueueBy(input, { key: 'size', reverse: false })
  assert.deepEqual(input.map((q) => q.id), [3, 1, 2])
})

test('sortQueueBy: ties keep the incoming order, in both directions', () => {
  const x = item(10, { progressPct: 40 })
  const y = item(11, { progressPct: 40 })
  const z = item(12, { progressPct: 40 })
  assert.deepEqual(sortQueueBy([y, z, x], { key: 'progress', reverse: false }).map((q) => q.id), [11, 12, 10])
  assert.deepEqual(sortQueueBy([y, z, x], { key: 'progress', reverse: true }).map((q) => q.id), [11, 12, 10])
})

test('sortQueueBy: speed and time left with no figures yet leave the order alone', () => {
  assert.deepEqual(sortQueueBy(list, { key: 'speed', reverse: false }).map((q) => q.id), [1, 2, 3, 4])
  assert.deepEqual(sortQueueBy(list, { key: 'eta', reverse: true }).map((q) => q.id), [1, 2, 3, 4])
})

test('transferBasis and paceOf: a download, an archive being unpacked, and one that is not running', () => {
  assert.deepEqual(transferBasis(item(1, { sizeBytes: 1000, progressPct: 25 })), { running: true, size: 1000, done: 250 })
  const unpacking = item(2, { status: 'importing', unpack: { index: 1, count: 1, doneBytes: 300, totalBytes: 900 } })
  assert.deepEqual(transferBasis(unpacking), { running: true, size: 900, done: 300 })
  assert.equal(transferBasis(item(3, { status: 'paused' })).running, false)
  assert.equal(transferBasis(item(4, { sizeBytes: 0 })).running, false)
  assert.deepEqual(paceOf({ running: true, size: 1000, done: 400 }, { at: 0, bytes: 400, rate: 100 }), { rate: 100, etaSec: 6 })
  assert.equal(paceOf({ running: true, size: 1000, done: 400 }, { at: 0, bytes: 400, rate: 0 }), null)
  assert.equal(paceOf({ running: false, size: 1000, done: 400 }, { at: 0, bytes: 400, rate: 100 }), null)
})

test('nextSamples follows running downloads only and has a speed after two looks', () => {
  const run = item(1, { sizeBytes: 10_000_000, progressPct: 0 })
  const idle = item(2, { status: 'paused' })
  const first = nextSamples(new Map(), [run, idle], 0)
  assert.deepEqual([...first.keys()], [1])
  const second = nextSamples(first, [{ ...run, progressPct: 20 }, idle], 2000)
  assert.equal(second.get(1)?.rate, 1_000_000)
})

function memory(initial?: string): KeyValueStore & { saved?: string } {
  const m: KeyValueStore & { saved?: string } = {
    getItem: () => m.saved ?? initial ?? null,
    setItem: (_k, v) => {
      m.saved = v
    },
  }
  return m
}

test('the choice is remembered and read back', () => {
  const store = memory()
  assert.deepEqual(readQueueSort(store), DEFAULT_QUEUE_SORT)
  rememberQueueSort({ key: 'speed', reverse: true }, store)
  assert.deepEqual(readQueueSort(store), { key: 'speed', reverse: true })
})

test('a damaged or unknown choice, and storage that throws, give the default', () => {
  for (const raw of ['', 'nope', '{"key":"colour"}', '{"key":7}', 'null']) assert.deepEqual(readQueueSort(memory(raw)), DEFAULT_QUEUE_SORT, raw)
  const refusing: KeyValueStore = {
    getItem: () => {
      throw new Error('blocked')
    },
    setItem: () => {
      throw new Error('blocked')
    },
  }
  assert.deepEqual(readQueueSort(refusing), DEFAULT_QUEUE_SORT)
  assert.doesNotThrow(() => rememberQueueSort({ key: 'size', reverse: false }, refusing))
  assert.deepEqual(readQueueSort(null), DEFAULT_QUEUE_SORT)
})
