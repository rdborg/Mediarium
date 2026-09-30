import assert from 'node:assert/strict'
import { test } from 'node:test'
import { signalFor } from './requestSignal.ts'

test('a read with no signal of its own gets the time limit', () => {
  const s = signalFor(undefined, 5000)
  assert.ok(s)
  assert.equal(s.aborted, false)
})

test('a write with no signal of its own gets none', () => {
  assert.equal(signalFor(undefined, undefined), undefined)
  assert.equal(signalFor(null, undefined), undefined)
})

test('the caller\'s signal is used when there is no time limit', () => {
  const own = new AbortController()
  assert.equal(signalFor(own.signal, undefined), own.signal)
})

test('cancelling the caller\'s signal stops a read that also has a time limit', () => {
  const own = new AbortController()
  const s = signalFor(own.signal, 60_000)
  assert.ok(s)
  assert.notEqual(s, own.signal)
  assert.equal(s.aborted, false)
  own.abort()
  assert.equal(s.aborted, true)
})

test('the time limit still stops a read that has a signal of its own', async () => {
  const own = new AbortController()
  const s = signalFor(own.signal, 10)
  assert.ok(s)
  await new Promise((r) => setTimeout(r, 60))
  assert.equal(s.aborted, true)
  assert.equal(own.signal.aborted, false)
})
