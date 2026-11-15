// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { releasePageUrl } from './releaseLink.ts'

test('an http or https page becomes the link', () => {
  assert.equal(releasePageUrl({ infoUrl: 'https://tracker.example/details/1', protocol: 'torrent' }), 'https://tracker.example/details/1')
  assert.equal(releasePageUrl({ infoUrl: 'http://nzb.example:8080/d/2', protocol: 'usenet' }), 'http://nzb.example:8080/d/2')
})

test('no address, or another kind of address, gives no link', () => {
  for (const infoUrl of [undefined, '', 'javascript:alert(1)', 'data:text/html,hi', 'magnet:?xt=urn:btih:abc', 'ftp://x.example/a', 'not a url', '//x.example/a', 'https://user:pw@x.example/a']) {
    assert.equal(releasePageUrl({ infoUrl, protocol: 'torrent' }), undefined, String(infoUrl))
  }
})

test('a Soulseek row never has a page', () => {
  assert.equal(releasePageUrl({ infoUrl: 'https://x.example/a', protocol: 'soulseek' }), undefined)
})
