// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { cleanAddress } from './manualAddress.ts'

const hash = 'urn:btih:0123456789abcdef0123456789abcdef01234567'

test('spaces in a magnet link become %20', () => {
  assert.equal(
    cleanAddress(`magnet:?xt=${hash}&dn=Some Movie (2023) 1080p WEB-DL&tr=udp://tracker.example:1337`),
    `magnet:?xt=${hash}&dn=Some%20Movie%20(2023)%201080p%20WEB-DL&tr=udp://tracker.example:1337`,
  )
  assert.equal(cleanAddress(`MAGNET:?xt=${hash}&dn=A B`), `MAGNET:?xt=${hash}&dn=A%20B`)
})

test('the ends are trimmed first, so only spaces inside the link are written', () => {
  assert.equal(cleanAddress(`  magnet:?xt=${hash}&dn=A B \n`), `magnet:?xt=${hash}&dn=A%20B`)
})

test('a magnet link that is already encoded is left as it was', () => {
  const link = `magnet:?xt=${hash}&dn=Some%20Movie+2023`
  assert.equal(cleanAddress(link), link)
})

test('a web address is only trimmed', () => {
  assert.equal(cleanAddress(' https://example.com/file.torrent '), 'https://example.com/file.torrent')
  assert.equal(cleanAddress('https://example.com/a b.torrent'), 'https://example.com/a b.torrent')
  assert.equal(cleanAddress('   '), '')
})
