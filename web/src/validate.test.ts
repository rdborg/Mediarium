// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import * as v from './validate.ts'

// Each row: input, whether it should pass.
function table(name: string, fn: (s: string) => v.Message, rows: [string, boolean][]) {
  test(name, () => {
    for (const [input, ok] of rows) {
      const got = fn(input)
      assert.equal(got === null, ok, `${JSON.stringify(input)} -> ${got}`)
    }
  })
}

table('email', v.email, [
  ['', true],
  ['name@example.com', true],
  ['first.last+tag@sub.example.co.uk', true],
  ['  name@example.com  ', true],
  ['name@example', false],
  ['name@.com', false],
  ['name@example.', false],
  ['@example.com', false],
  ['name@@example.com', false],
  ['na me@example.com', false],
  ['a@b.com, c@d.com', false],
  ['Name <name@example.com>', false],
])

table('username', v.username, [
  ['', true],
  ['ryan', true],
  ['ryan.b-1_x', true],
  ['abc', true],
  ['ab', false],
  ['a'.repeat(32), true],
  ['a'.repeat(33), false],
  ['has space', false],
  ['semi;colon', false],
  ['émile', false],
])

table('password', v.password, [
  ['', true],
  ['12345678', true],
  ['short', false],
  ['a'.repeat(72), true],
  ['a'.repeat(73), false],
  ['é'.repeat(37), false],
])

table('url', (s) => v.url(s), [
  ['', true],
  ['http://192.168.1.10:32400', true],
  ['https://plex.example.com/web', true],
  ['http://[::1]:8080', true],
  ['http://localhost', true],
  ['192.168.1.10:8080', true],
  ['plex.local', true],
  ['ftp://example.com', false],
  ['javascript:alert(1)', false],
  ['http://', false],
  ['http://exa mple.com', false],
  ['http://host:99999', false],
  ['http://300.1.1.1', false],
  ['//example.com', false],
])

table('url with scheme required', (s) => v.url(s, { requireScheme: true }), [
  ['', true],
  ['http://a.example', true],
  ['a.example', false],
  ['192.168.1.10:8080', false],
])

table('hostOrIP', (s) => v.hostOrIP(s), [
  ['', true],
  ['news.example.com', true],
  ['localhost', true],
  ['192.168.1.10', true],
  ['::1', true],
  ['[fe80::1]', true],
  ['2001:db8::8a2e:370:7334', true],
  ['my-host_1', true],
  ['999.1.1.1', false],
  ['1.2.3', false],
  ['http://news.example.com', false],
  ['news.example.com:563', false],
  ['news.example.com/path', false],
  ['-bad.example.com', false],
  ['bad host', false],
  ['a..b', false],
])

test('port', () => {
  const rows: [unknown, boolean][] = [
    ['', true],
    [1, true],
    [563, true],
    ['8080', true],
    [65535, true],
    [65536, false],
    [0, false],
    [-1, false],
    ['80.5', false],
    ['abc', false],
    [NaN, false],
  ]
  for (const [input, ok] of rows) assert.equal(v.port(input) === null, ok, String(input))
  assert.equal(v.port(0), 'Port must be a number between 1 and 65535.')
})

test('numberRange', () => {
  const rows: [unknown, v.RangeOptions, boolean][] = [
    ['', {}, true],
    ['', { allowBlank: false }, false],
    [5, { min: 1, max: 10 }, true],
    [0, { min: 1 }, false],
    [11, { min: 1, max: 10 }, false],
    [2.5, { min: 1 }, false],
    [2.5, { min: 1, decimal: true }, true],
    ['abc', {}, false],
    [-1, { min: 0 }, false],
  ]
  for (const [input, opts, ok] of rows) assert.equal(v.numberRange(input, 'Value', opts) === null, ok, `${input} ${JSON.stringify(opts)}`)
  assert.equal(v.numberRange(0, 'Connections', { min: 1, max: 100 }), 'Connections must be a whole number between 1 and 100.')
  assert.equal(v.positiveInt(0, 'Limit'), 'Limit must be a whole number of 1 or more.')
})

test('minMax', () => {
  assert.equal(v.minMax(1, 5, 'x'), null)
  assert.equal(v.minMax(5, 5, 'x'), null)
  assert.equal(v.minMax(6, 5, 'x'), 'x')
  assert.equal(v.minMax('', 5, 'x'), null)
  assert.equal(v.minMax('a', 5, 'x'), null)
})

table('folderPath', (s) => v.folderPath(s), [
  ['', true],
  ['/media/movies', true],
  ['/', true],
  ['D:\\Media\\Movies', true],
  ['d:/media', true],
  ['\\\\nas\\share\\tv', true],
  ['media/movies', false],
  ['./movies', false],
  ['D:', false],
  ['/media/mov\u0000ies', false],
  ['~/movies', false],
])

table('apiKey', v.apiKey, [
  ['', true],
  ['abc123DEF', true],
  ['  abc123  ', true],
  ['abc 123', false],
  ['abc\n123', false],
  ['x'.repeat(513), false],
])

test('required and firstError', () => {
  assert.equal(v.required('   ', 'Add a name.'), 'Add a name.')
  assert.equal(v.required('x'), null)
  assert.equal(v.required(0), null)
  assert.equal(v.firstError(null, false, 'first', 'second'), 'first')
  assert.equal(v.firstError(null, undefined), null)
})

test('passwordsMatch', () => {
  assert.equal(v.passwordsMatch('a', ''), null)
  assert.equal(v.passwordsMatch('a', 'a'), null)
  assert.notEqual(v.passwordsMatch('a', 'b'), null)
})

table('hostPort', (s) => v.hostPort(s), [
  ['', true],
  ['vpn.example.com:51820', true],
  ['185.213.154.66:51820', true],
  ['[2001:db8::1]:51820', true],
  ['vpn.example.com', false],
  ['vpn.example.com:0', false],
  ['vpn.example.com:70000', false],
  ['http://vpn.example.com:51820', false],
  ['999.1.1.1:51820', false],
])

table('wireguardKey', (s) => v.wireguardKey(s), [
  ['', true],
  ['yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=', true],
  ['yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk', false],
  ['not a key', false],
  ['PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=', false],
])

table('ipWithPrefix', (s) => v.ipWithPrefix(s), [
  ['', true],
  ['10.2.0.2/32', true],
  ['10.2.0.2', true],
  ['fd00::2/128', true],
  ['10.2.0.2/33', false],
  ['10.2.0.256', false],
  ['vpn.example.com', false],
  ['10.2.0.2/32/1', false],
])

test('messages read like a person wrote them', () => {
  const all = [
    v.email('x'),
    v.username('a'),
    v.password('a'),
    v.url('ftp://x'),
    v.hostOrIP('http://x'),
    v.port(0),
    v.folderPath('x'),
    v.apiKey('a b'),
    v.required(''),
    v.hostPort('x'),
    v.wireguardKey('x'),
    v.ipWithPrefix('x'),
  ]
  for (const m of all) {
    assert.ok(m && m.length > 15, String(m))
    assert.ok(!/invalid input|validation failed|field is required/i.test(m!), String(m))
  }
})
