// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { portStatus } from './torrentPort.ts'

const base = { listenPort: 58264, blockedByVpn: false }

test('nothing to show before the status has loaded or with torrents off', () => {
  assert.equal(portStatus(null), null)
  assert.equal(portStatus({ ...base, state: 'disabled' }), null)
})

test('an idle client says the port is closed for now', () => {
  const s = portStatus({ ...base, state: 'ready', listening: false })
  assert.equal(s?.title, 'Closed for now.')
  assert.equal(s?.tone, 'info')
})

test('listening, nobody has connected yet: explains port forwarding', () => {
  const s = portStatus({ ...base, state: 'ready', listening: true, activePort: 58264, incomingSeen: false })
  assert.equal(s?.title, 'Listening on port 58264.')
  assert.match(s?.text ?? '', /No peer has connected yet/)
  assert.match(s?.text ?? '', /forward this port/)
})

test('listening, a peer has connected: the port works', () => {
  const s = portStatus({ ...base, state: 'ready', listening: true, activePort: 58264, incomingSeen: true })
  assert.equal(s?.tone, 'good')
  assert.match(s?.text ?? '', /works from the internet/)
})

test('a busy port is called out', () => {
  const s = portStatus({ ...base, state: 'ready', listening: true, activePort: 41234, incomingSeen: false })
  assert.equal(s?.title, 'Listening on port 41234.')
  assert.equal(s?.tone, 'warn')
  assert.match(s?.text ?? '', /Port 58264 was already in use, so Mediarium is using 41234 instead/)
})

test('with a VPN on, the port is not used', () => {
  for (const vpnState of ['connected', 'connecting']) {
    const s = portStatus({ ...base, state: 'ready', vpnState, listening: true, activePort: 58264 })
    assert.equal(s?.title, 'Not used while your VPN is on.')
  }
})

test('a VPN that is down holds torrents back', () => {
  const s = portStatus({ ...base, state: 'blocked', vpnState: 'down', blockedByVpn: true })
  assert.equal(s?.title, 'Waiting for your VPN.')
  assert.equal(s?.tone, 'warn')
})
