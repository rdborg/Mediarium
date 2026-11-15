import assert from 'node:assert/strict'
import { test } from 'node:test'
import { hideParam, hidingSummary, parseServices, SERVICES } from './streamingServices.ts'

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
