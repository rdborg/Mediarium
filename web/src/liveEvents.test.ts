import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createLiveStream, type ChannelLike, type LiveEnv, type LiveEvent, type LocksLike, type SourceLike } from './liveEvents.ts'

// A fake browser: the locks, the channels and the sources are shared by every
// tab made from it, the way they are in a real browser.
class FakeSource implements SourceLike {
  readyState = 0
  onerror: ((e: unknown) => void) | null = null
  closed = false
  handlers = new Map<string, ((e: { data?: string }) => void)[]>()
  url: string
  constructor(url: string) {
    this.url = url
  }
  close() {
    this.closed = true
    this.readyState = 2
  }
  addEventListener(name: string, fn: (e: { data?: string }) => void) {
    this.handlers.set(name, [...(this.handlers.get(name) ?? []), fn])
  }
  emit(name: string, data?: string) {
    for (const fn of this.handlers.get(name) ?? []) fn({ data })
  }
}

class FakeBrowser {
  sources: FakeSource[] = []
  clock = 0
  timers: { id: number; at: number; fn: () => void }[] = []
  nextTimer = 1
  held = false
  waiting: { cb: () => Promise<unknown>; resolve: () => void; signal?: AbortSignal }[] = []
  channels: FakeChannel[] = []

  locks: LocksLike = {
    request: (_name, opts, cb) =>
      new Promise((resolve, reject) => {
        const entry = { cb, resolve: () => resolve(undefined), signal: opts.signal }
        opts.signal?.addEventListener('abort', () => {
          const i = this.waiting.indexOf(entry)
          if (i >= 0) {
            this.waiting.splice(i, 1)
            reject(new DOMException('aborted', 'AbortError'))
          }
        })
        this.waiting.push(entry)
        this.grant()
      }),
  }

  grant() {
    if (this.held) return
    const next = this.waiting.shift()
    if (!next) return
    this.held = true
    void next.cb().finally(() => {
      this.held = false
      next.resolve()
      this.grant()
    })
  }

  // A tab: its own visibility, sharing everything else.
  tab(opts: { locks?: boolean; channel?: boolean } = {}) {
    const state = { hidden: false, watchers: new Set<() => void>() }
    const env: LiveEnv = {
      makeSource: (url) => {
        const s = new FakeSource(url)
        this.sources.push(s)
        return s
      },
      makeChannel: opts.channel === false ? undefined : (name) => new FakeChannel(this, name),
      locks: opts.locks === false ? undefined : this.locks,
      hidden: () => state.hidden,
      onVisibility: (fn) => {
        state.watchers.add(fn)
        return () => state.watchers.delete(fn)
      },
      setTimeout: (fn, ms) => {
        const id = this.nextTimer++
        this.timers.push({ id, at: this.clock + ms, fn })
        return id
      },
      clearTimeout: (id) => {
        this.timers = this.timers.filter((t) => t.id !== id)
      },
      now: () => this.clock,
    }
    return {
      env,
      setHidden(h: boolean) {
        state.hidden = h
        for (const fn of [...state.watchers]) fn()
      },
    }
  }

  advance(ms: number) {
    this.clock += ms
    for (;;) {
      const due = this.timers.filter((t) => t.at <= this.clock).sort((a, b) => a.at - b.at)[0]
      if (!due) return
      this.timers = this.timers.filter((t) => t !== due)
      due.fn()
    }
  }

  open() {
    return this.sources.filter((s) => !s.closed)
  }
}

class FakeChannel implements ChannelLike {
  onmessage: ((e: { data: unknown }) => void) | null = null
  b: FakeBrowser
  name: string
  constructor(b: FakeBrowser, name: string) {
    this.b = b
    this.name = name
    b.channels.push(this)
  }
  postMessage(m: unknown) {
    for (const c of this.b.channels) if (c !== this && c.name === this.name) c.onmessage?.({ data: m })
  }
  close() {
    this.b.channels = this.b.channels.filter((c) => c !== this)
  }
}

// Let queued promise callbacks run.
const settle = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve()
}

function collect(s: ReturnType<typeof createLiveStream>) {
  const got: LiveEvent[] = []
  const off = s.subscribe((e) => got.push(e))
  return { got, off }
}

test('three tabs share one stream and all see its events', async () => {
  const b = new FakeBrowser()
  const streams = [b.tab(), b.tab(), b.tab()].map((t) => createLiveStream(t.env))
  const seen = streams.map(collect)
  await settle()
  assert.equal(b.open().length, 1, 'one EventSource for three tabs')
  b.open()[0].emit('activity', '{"type":"imported"}')
  for (const { got } of seen) assert.deepEqual(got, [{ name: 'activity', data: { type: 'imported' } }])
  assert.equal(streams.filter((s) => s.isLeader()).length, 1)
})

test('the stream is handed to another tab when the leader goes away', async () => {
  const b = new FakeBrowser()
  const a = createLiveStream(b.tab().env)
  const c = createLiveStream(b.tab().env)
  const ca = collect(a)
  const cc = collect(c)
  await settle()
  assert.equal(a.isLeader(), true)
  assert.equal(c.isLeader(), false)
  ca.off() // the tab closes
  await settle()
  assert.equal(c.isLeader(), true)
  assert.equal(b.open().length, 1, 'the old source is closed and one new one is open')
  b.open()[0].emit('hello')
  // The new leader's stream is new to this tab and the page is not fresh: it reloads.
  b.clock = 60_000
  b.open()[0].emit('activity', '{}')
  assert.equal(cc.got.at(-1)?.name, 'activity')
})

test('a hidden tab lets go after a long time and comes back with a resync', async () => {
  const b = new FakeBrowser()
  const tab = b.tab()
  const s = createLiveStream(tab.env, { hiddenMs: 1000 })
  const c = collect(s)
  await settle()
  assert.equal(b.open().length, 1)
  tab.setHidden(true)
  b.advance(999)
  assert.equal(b.open().length, 1, 'still open before the time is up')
  b.advance(2)
  await settle()
  assert.equal(b.open().length, 0, 'closed after being hidden long enough')
  assert.equal(s.isLeader(), false)
  tab.setHidden(false)
  await settle()
  assert.equal(b.open().length, 1, 'opened again when shown')
  assert.equal(c.got.some((e) => e.name === 'resync'), true)
})

test('being hidden for a short time keeps the stream', async () => {
  const b = new FakeBrowser()
  const tab = b.tab()
  const s = createLiveStream(tab.env, { hiddenMs: 1000 })
  collect(s)
  await settle()
  tab.setHidden(true)
  b.advance(500)
  tab.setHidden(false)
  b.advance(5000)
  await settle()
  assert.equal(b.open().length, 1)
  assert.equal(s.isLeader(), true)
})

test('the stream closes when the last listener leaves', async () => {
  const b = new FakeBrowser()
  const s = createLiveStream(b.tab().env)
  const one = collect(s)
  const two = collect(s)
  await settle()
  assert.equal(b.open().length, 1)
  one.off()
  assert.equal(b.open().length, 1)
  two.off()
  await settle()
  assert.equal(b.open().length, 0)
  assert.equal(b.held, false, 'the lock is free again')
})

test('without Web Locks each tab opens its own stream', async () => {
  const b = new FakeBrowser()
  const a = createLiveStream(b.tab({ locks: false }).env)
  const c = createLiveStream(b.tab({ locks: false }).env)
  const ca = collect(a)
  collect(c)
  await settle()
  assert.equal(b.open().length, 2)
  b.sources[0].emit('activity', '{}')
  assert.equal(ca.got.length, 1)
})

test('without a broadcast channel each tab opens its own stream', async () => {
  const b = new FakeBrowser()
  createLiveStream(b.tab({ channel: false }).env).subscribe(() => {})
  createLiveStream(b.tab({ channel: false }).env).subscribe(() => {})
  await settle()
  assert.equal(b.open().length, 2)
})

test('a stream the server turned away is opened again, a little later each time', async () => {
  const b = new FakeBrowser()
  const s = createLiveStream(b.tab().env, { retryMs: [100, 200, 400] })
  const c = collect(s)
  await settle()
  const first = b.sources[0]
  first.readyState = 2 // the server answered 429: the browser gives up on it
  first.onerror?.({})
  assert.equal(b.open().length, 0)
  b.advance(99)
  assert.equal(b.sources.length, 1)
  b.advance(2)
  assert.equal(b.sources.length, 2, 'retried after the first wait')
  b.sources[1].readyState = 2
  b.sources[1].onerror?.({})
  b.advance(150)
  assert.equal(b.sources.length, 2, 'the second wait is longer')
  b.advance(60)
  assert.equal(b.sources.length, 3)
  // When it finally connects, the pages load what they missed.
  b.sources[2].emit('hello')
  assert.equal(c.got.some((e) => e.name === 'resync'), true)
})

test('a connection the browser re-opens by itself is not treated as closed', async () => {
  const b = new FakeBrowser()
  const s = createLiveStream(b.tab().env)
  collect(s)
  await settle()
  b.sources[0].readyState = 0 // connecting again
  b.sources[0].onerror?.({})
  b.advance(120_000)
  assert.equal(b.sources.length, 1)
  assert.equal(b.open().length, 1)
})

test('the first hello of a page that was just opened does not reload it, a later one does', async () => {
  const b = new FakeBrowser()
  const s = createLiveStream(b.tab().env)
  const c = collect(s)
  await settle()
  b.sources[0].emit('hello')
  assert.equal(c.got.length, 0)
  b.sources[0].emit('hello') // the browser reconnected
  assert.deepEqual(c.got.map((e) => e.name), ['resync'])
})

test('events with bad data are still delivered, with null data', async () => {
  const b = new FakeBrowser()
  const s = createLiveStream(b.tab().env)
  const c = collect(s)
  await settle()
  b.sources[0].emit('activity', 'not json')
  assert.deepEqual(c.got, [{ name: 'activity', data: null }])
})

test('a listener that throws does not stop the others', async () => {
  const b = new FakeBrowser()
  const s = createLiveStream(b.tab().env)
  s.subscribe(() => {
    throw new Error('boom')
  })
  const c = collect(s)
  await settle()
  const log = console.error
  console.error = () => {}
  try {
    b.sources[0].emit('activity', '{}')
  } finally {
    console.error = log
  }
  assert.equal(c.got.length, 1)
})
