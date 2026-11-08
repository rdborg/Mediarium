// One live event stream for every tab of the browser.
//
// A browser keeps at most six connections to one address open on HTTP/1.1, and
// an open event stream uses one for as long as the page lives. With a stream
// per tab, a few tabs use them all and the app stops answering. So the tabs
// elect a leader: the one that holds a Web Lock owns the single EventSource and
// passes each event to the other tabs over a BroadcastChannel. When the leader
// closes (or crashes) the browser frees its lock and the next tab takes over.
//
// A tab that has been hidden for a long time lets go (the stream closes if it
// was the leader) and joins again when it is shown, with a "resync" event so
// the page loads what it missed. Where Web Locks or BroadcastChannel are
// missing the tab opens its own stream, as before; where there is no
// EventSource nothing is delivered and the pages simply keep polling.
//
// The environment is passed in (see browserEnv) so the logic is tested without
// a browser.

export interface LiveEvent {
  name: string // "activity", "resync", ...
  data: unknown
}

export type LiveListener = (ev: LiveEvent) => void

export interface SourceLike {
  readyState: number
  close(): void
  addEventListener(name: string, fn: (e: { data?: string }) => void): void
  onerror: ((e: unknown) => void) | null
}

export interface ChannelLike {
  postMessage(m: unknown): void
  close(): void
  onmessage: ((e: { data: unknown }) => void) | null
}

export interface LocksLike {
  request(name: string, opts: { signal?: AbortSignal }, cb: () => Promise<unknown>): Promise<unknown>
}

export interface LiveEnv {
  makeSource?: (url: string) => SourceLike
  makeChannel?: (name: string) => ChannelLike
  locks?: LocksLike
  hidden: () => boolean
  onVisibility: (fn: () => void) => () => void
  setTimeout: (fn: () => void, ms: number) => unknown
  clearTimeout: (id: unknown) => void
  now: () => number
}

export interface LiveOptions {
  url?: string
  lockName?: string
  channelName?: string
  // How long a tab may stay hidden before it lets go of the stream.
  hiddenMs?: number
  // The waits before a stream that failed is opened again.
  retryMs?: number[]
}

const CLOSED = 2 // EventSource.CLOSED

// The names the server sends. Anything else is ignored.
const KNOWN = ['hello', 'activity', 'resync']

export interface LiveStream {
  subscribe(fn: LiveListener): () => void
  // Whether this tab holds the stream right now (for tests and the log).
  isLeader(): boolean
  stop(): void
}

export function createLiveStream(env: LiveEnv, opts: LiveOptions = {}): LiveStream {
  const url = opts.url ?? '/api/events'
  const lockName = opts.lockName ?? 'mediarium-live-events'
  const channelName = opts.channelName ?? 'mediarium-live-events'
  const hiddenMs = opts.hiddenMs ?? 5 * 60 * 1000
  const retryMs = opts.retryMs ?? [5000, 15000, 30000, 60000]
  const freshMs = 3000

  const listeners = new Set<LiveListener>()
  let channel: ChannelLike | undefined
  let source: SourceLike | undefined
  let leader = false
  let active = false // this tab wants the stream (it is shown, or was shown lately)
  let running = false // started: there are listeners
  let release: (() => void) | undefined // ends the lock this tab holds
  let abortWait: AbortController | undefined // ends a lock request that is still waiting
  let idleTimer: unknown
  let retryTimer: unknown
  let failures = 0
  let unwatch: (() => void) | undefined
  let born = 0
  let hellos = 0

  const deliver = (ev: LiveEvent) => {
    for (const fn of [...listeners]) {
      try {
        fn(ev)
      } catch (e) {
        console.error('live event listener failed', e)
      }
    }
  }

  const parse = (raw: string | undefined): unknown => {
    if (!raw) return null
    try {
      return JSON.parse(raw)
    } catch {
      return null
    }
  }

  // The leader got an event from the server: deliver it here and tell the others.
  const fromServer = (name: string, raw: string | undefined) => {
    const ev: LiveEvent = { name, data: parse(raw) }
    channel?.postMessage({ live: 1, ...ev })
    deliver(ev)
  }

  const closeSource = () => {
    if (retryTimer !== undefined) env.clearTimeout(retryTimer)
    retryTimer = undefined
    if (source) {
      source.onerror = null
      source.close()
    }
    source = undefined
  }

  const openSource = () => {
    if (!env.makeSource || source || !leader) return
    const s = env.makeSource(url)
    source = s
    for (const name of KNOWN) {
      s.addEventListener(name, (e) => {
        if (name === 'hello') {
          // Connected. Whatever happened while it was not is missed, so the
          // pages load again. Not for the first connection of a page that
          // has only just been opened: it has loaded what it shows already.
          const missed = failures > 0 || hellos > 0 || env.now() - born > freshMs
          failures = 0
          hellos++
          if (missed) fromServer('resync', '{}')
          return
        }
        fromServer(name, e.data)
      })
    }
    s.onerror = () => {
      // The browser reconnects by itself after a dropped connection. A closed
      // source is one the server turned away (too many streams) or an answer
      // that was not a stream: open it again later, a little slower each time.
      if (s.readyState !== CLOSED || source !== s) return
      closeSource()
      const wait = retryMs[Math.min(failures, retryMs.length - 1)]
      failures++
      retryTimer = env.setTimeout(() => {
        retryTimer = undefined
        openSource()
      }, wait)
    }
  }

  const becomeLeader = () => {
    leader = true
    openSource()
  }

  const loseLeadership = () => {
    leader = false
    closeSource()
  }

  // Ask for the lock. The request waits until the lock is free (no other tab
  // holds the stream) and then runs for as long as this tab keeps it.
  const joinElection = () => {
    if (!env.locks || release || abortWait) return
    const ctl = new AbortController()
    abortWait = ctl
    env.locks
      .request(lockName, { signal: ctl.signal }, () => {
        abortWait = undefined
        if (!running || !active) return Promise.resolve() // no longer wanted
        becomeLeader()
        return new Promise<void>((resolve) => {
          release = resolve
        }).finally(() => {
          release = undefined
          loseLeadership()
        })
      })
      .catch(() => {
        // The wait was cancelled (AbortError) or locks are not usable here.
      })
      .finally(() => {
        if (abortWait === ctl) abortWait = undefined
      })
  }

  const leaveElection = () => {
    abortWait?.abort()
    abortWait = undefined
    release?.()
  }

  // Without locks or a channel every tab owns a stream, as before.
  const solo = () => !env.locks || !env.makeChannel

  const activate = () => {
    if (active) return
    active = true
    if (solo()) {
      leader = true
      openSource()
    } else {
      joinElection()
    }
  }

  const deactivate = () => {
    if (!active) return
    active = false
    if (solo()) loseLeadership()
    else leaveElection()
  }

  const onVisibility = () => {
    if (!running) return
    if (idleTimer !== undefined) env.clearTimeout(idleTimer)
    idleTimer = undefined
    if (env.hidden()) {
      idleTimer = env.setTimeout(() => {
        idleTimer = undefined
        deactivate()
      }, hiddenMs)
    } else if (!active) {
      activate()
      // It was away, and the tab that holds the stream did not send it what it
      // missed: the page may be out of date.
      if (!solo()) deliver({ name: 'resync', data: null })
    }
  }

  const start = () => {
    running = true
    born = env.now()
    hellos = 0
    if (env.makeChannel && env.locks) {
      channel = env.makeChannel(channelName)
      channel.onmessage = (e) => {
        const m = e.data as { live?: number; name?: string; data?: unknown } | null
        if (m && m.live === 1 && typeof m.name === 'string') deliver({ name: m.name, data: m.data ?? null })
      }
    }
    unwatch = env.onVisibility(onVisibility)
    if (env.hidden()) {
      // Opened in the background: wait to be shown, but not for ever.
      idleTimer = env.setTimeout(() => {
        idleTimer = undefined
        deactivate()
      }, hiddenMs)
    }
    activate()
  }

  const stop = () => {
    running = false
    if (idleTimer !== undefined) env.clearTimeout(idleTimer)
    idleTimer = undefined
    unwatch?.()
    unwatch = undefined
    deactivate()
    closeSource()
    if (channel) {
      channel.onmessage = null
      channel.close()
    }
    channel = undefined
    failures = 0
  }

  return {
    subscribe(fn) {
      listeners.add(fn)
      if (!running) start()
      return () => {
        listeners.delete(fn)
        if (listeners.size === 0 && running) stop()
      }
    },
    isLeader: () => leader,
    stop,
  }
}

// browserEnv is the real environment of a page, or undefined where there is no
// EventSource to use.
export function browserEnv(): LiveEnv | undefined {
  if (typeof window === 'undefined' || typeof window.EventSource === 'undefined') return undefined
  const nav = window.navigator as Navigator & { locks?: LocksLike }
  return {
    makeSource: (url) => new EventSource(url, { withCredentials: true }) as unknown as SourceLike,
    makeChannel: typeof BroadcastChannel === 'undefined' ? undefined : (name) => new BroadcastChannel(name) as unknown as ChannelLike,
    locks: nav.locks as LocksLike | undefined,
    hidden: () => document.visibilityState === 'hidden',
    onVisibility: (fn) => {
      document.addEventListener('visibilitychange', fn)
      return () => document.removeEventListener('visibilitychange', fn)
    },
    setTimeout: (fn, ms) => window.setTimeout(fn, ms),
    clearTimeout: (id) => window.clearTimeout(id as number),
    now: () => Date.now(),
  }
}

let shared: LiveStream | undefined

// liveEvents subscribes to the stream shared by this browser's tabs. It
// returns the function that stops listening. Where the browser cannot do
// event streams it listens to nothing.
export function liveEvents(fn: LiveListener): () => void {
  if (!shared) {
    const env = browserEnv()
    if (!env) return () => {}
    shared = createLiveStream(env)
  }
  return shared.subscribe(fn)
}
