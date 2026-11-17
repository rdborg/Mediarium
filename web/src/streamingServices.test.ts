import assert from 'node:assert/strict'
import { test } from 'node:test'
import { hideParam, hidingSummary, parseServices, reconcileServices, SERVICES, type SavedServices } from './streamingServices.ts'

test('a saved list keeps only known services, in the usual order, once', () => {
  assert.deepEqual(parseServices('hulu, netflix,netflix,bogus,'), ['netflix', 'hulu'])
  assert.deepEqual(parseServices(''), [])
  assert.deepEqual(parseServices(null), [])
})

test('nothing is asked for while the switch is off or no service is chosen', () => {
  assert.equal(hideParam(['netflix', 'disney'], false), '')
  assert.equal(hideParam([], true), '')
  assert.equal(hideParam(['netflix', 'disney'], true), 'netflix,disney')
  assert.equal(hideParam(['netflix', 'nope'], true), 'netflix')
})

test('the summary names the services only while hiding', () => {
  assert.equal(hidingSummary(['netflix', 'disney'], false), '')
  assert.equal(hidingSummary([], true), '')
  assert.equal(hidingSummary(['netflix', 'disney'], true), 'Hiding Netflix, Disney+')
})

test('every service has a distinct key and a name', () => {
  assert.equal(new Set(SERVICES.map((s) => s.key)).size, SERVICES.length)
  assert.ok(SERVICES.every((s) => s.key && s.name))
})

const server = (over: Partial<SavedServices>): SavedServices => ({ services: [], on: false, saved: false, account: true, ...over })

test('what the account saved wins over what the browser kept', () => {
  const r = reconcileServices(server({ saved: true, services: ['hulu', 'bogus', 'netflix'], on: true }), { keys: ['disney'], on: true })
  assert.deepEqual(r.next, { keys: ['netflix', 'hulu'], on: true })
  assert.equal(r.push, null)
  assert.equal(r.forgetLocal, true)
})

test('a saved choice with no service left is never on', () => {
  const r = reconcileServices(server({ saved: true, services: [], on: true }), { keys: [], on: false })
  assert.deepEqual(r.next, { keys: [], on: false })
})

test('the pick an older version kept in the browser moves to the account once', () => {
  const local = { keys: ['netflix', 'disney'], on: true }
  const r = reconcileServices(server({ saved: false }), local)
  assert.deepEqual(r.next, local)
  assert.deepEqual(r.push, local)
  assert.equal(r.forgetLocal, true)
})

test('nothing is sent when neither side has a choice', () => {
  const r = reconcileServices(server({ saved: false }), { keys: [], on: false })
  assert.deepEqual(r.next, { keys: [], on: false })
  assert.equal(r.push, null)
})

test('without an account the browser keeps the choice', () => {
  const local = { keys: ['prime'], on: true }
  const r = reconcileServices(server({ account: false }), local)
  assert.deepEqual(r.next, local)
  assert.equal(r.push, null)
  assert.equal(r.forgetLocal, false)
})
