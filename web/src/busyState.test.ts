// Run with: npm test
import assert from 'node:assert/strict'
import { afterEach, beforeEach, mock, test } from 'node:test'
import { isBusy, markHidden, noteBusy, noteFine, resetBusy, SLOW_MS, startRead, subscribe } from './busyState.ts'

beforeEach(() => {
  resetBusy()
  mock.timers.enable({ apis: ['setTimeout', 'Date'] })
})
afterEach(() => {
  resetBusy()
  mock.timers.reset()
})

test('a read that answers in time never shows the notice', () => {
  const end = startRead()
  mock.timers.tick(SLOW_MS - 1)
  assert.equal(isBusy(), false)
  end()
  mock.timers.tick(SLOW_MS * 2)
  assert.equal(isBusy(), false)
})

test('a read still waiting after ten seconds turns it on, and its end turns it off', () => {
  const end = startRead()
  mock.timers.tick(SLOW_MS)
  assert.equal(isBusy(), true)
  end()
  assert.equal(isBusy(), false)
})

test('two slow reads keep it on until both are over', () => {
  const a = startRead()
  const b = startRead()
  mock.timers.tick(SLOW_MS)
  a()
  assert.equal(isBusy(), true)
  b()
  assert.equal(isBusy(), false)
})

test('a busy answer holds it on for a while, then it lets go by itself', () => {
  const heard: boolean[] = []
  const off = subscribe(() => heard.push(isBusy()))
  noteBusy()
  assert.equal(isBusy(), true)
  mock.timers.tick(19_000)
  assert.equal(isBusy(), true)
  mock.timers.tick(2_000)
  assert.equal(isBusy(), false)
  off()
  assert.deepEqual(heard, [true, false])
})

test('an answer that works clears a busy answer at once', () => {
  noteBusy()
  noteFine()
  assert.equal(isBusy(), false)
})

test('a normal answer changes nothing when the app was not busy', () => {
  let calls = 0
  const off = subscribe(() => calls++)
  noteFine()
  off()
  assert.equal(calls, 0)
})

test('a read that waited while the page was out of sight does not count as slow', () => {
  const end = startRead()
  mock.timers.tick(1000)
  markHidden()
  mock.timers.tick(SLOW_MS)
  assert.equal(isBusy(), false)
  end()
})
