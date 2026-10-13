// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { flareSolverrSetUp } from './vpnIndexers.ts'

test('no FlareSolverr: no warning needed', () => {
  assert.equal(flareSolverrSetUp({}), false)
  assert.equal(flareSolverrSetUp({ flareSolverrUrl: '  ' }), false)
})

test('a saved address or the built-in one counts', () => {
  assert.equal(flareSolverrSetUp({ flareSolverrUrl: 'http://flaresolverr:8191' }), true)
  assert.equal(flareSolverrSetUp({ flareSolverrBundled: true }), true)
})
