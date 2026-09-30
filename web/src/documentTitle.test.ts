// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { titleFor } from './documentTitle.ts'

test('titleFor puts the page first and the app second', () => {
  assert.equal(titleFor('Library'), 'Library · Mediarium')
  assert.equal(titleFor('Server and backup'), 'Server and backup · Mediarium')
  assert.equal(titleFor('  Dashboard '), 'Dashboard · Mediarium')
})

test('titleFor never says Mediarium twice', () => {
  assert.equal(titleFor(''), 'Mediarium')
  assert.equal(titleFor('Mediarium'), 'Mediarium')
})
