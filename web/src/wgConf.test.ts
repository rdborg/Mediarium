import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parseWireGuardConf } from './wgConf.ts'

const KEY = 'a'.repeat(43) + '='
const PUB = 'b'.repeat(43) + '='
const PSK = 'c'.repeat(43) + '='

test('a Windscribe-style config with a preshared key', () => {
  const conf = `[Interface]
PrivateKey = ${KEY}
Address = 100.64.12.34/32
DNS = 10.255.255.3

[Peer]
PublicKey = ${PUB}
AllowedIPs = 0.0.0.0/0
Endpoint = ams-123.whiskergalaxy.com:443
PresharedKey = ${PSK}
`
  assert.deepEqual(parseWireGuardConf(conf), {
    privateKey: KEY,
    localAddresses: ['100.64.12.34/32'],
    dns: ['10.255.255.3'],
    peerPublicKey: PUB,
    presharedKey: PSK,
    endpoint: 'ams-123.whiskergalaxy.com:443',
    allowedIps: ['0.0.0.0/0'],
  })
})

test('lists, IPv6, comments, Windows line endings and search domains', () => {
  const conf = '# made by my router\r\n[Interface]\r\nPrivateKey=' + KEY + '\r\nAddress = 10.2.0.2/32, fd00::2/128 ; mine\r\nDNS = 10.2.0.1, home.lan\r\n[Peer]\r\nPublicKey = ' + PUB + '\r\nAllowedIPs = 0.0.0.0/0, ::/0\r\nEndpoint = [2001:db8::1]:51820\r\n'
  const got = parseWireGuardConf(conf)!
  assert.deepEqual(got.localAddresses, ['10.2.0.2/32', 'fd00::2/128'])
  assert.deepEqual(got.dns, ['10.2.0.1'])
  assert.deepEqual(got.allowedIps, ['0.0.0.0/0', '::/0'])
  assert.equal(got.endpoint, '[2001:db8::1]:51820')
  assert.equal(got.presharedKey, '')
})

test('only the first peer is used', () => {
  const conf = `[Interface]\nPrivateKey = ${KEY}\n[Peer]\nPublicKey = ${PUB}\nEndpoint = a.example:1\n[Peer]\nPublicKey = ${PSK}\nEndpoint = b.example:2\n`
  const got = parseWireGuardConf(conf)!
  assert.equal(got.peerPublicKey, PUB)
  assert.equal(got.endpoint, 'a.example:1')
})

test('text that is not a config', () => {
  assert.equal(parseWireGuardConf('just some text'), null)
  assert.equal(parseWireGuardConf(''), null)
})
