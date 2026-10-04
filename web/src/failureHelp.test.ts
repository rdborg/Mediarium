// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { failureHelp } from './failureHelp.ts'

test('each kind of failure gets its own explanation', () => {
  const cases: [string, 'usenet' | 'torrent', RegExp][] = [
    ["repairing the download failed: 2567 articles couldn't be found on your Usenet servers and the release has no PAR2 files to repair it", 'usenet', /no longer complete/],
    ['the torrent got no data for 2 hours: no one seems to be sharing it', 'torrent', /Nobody is sharing/],
    ['extract movie.rar: password protected', 'usenet', /password/],
    ['write segment x: the disk is full', 'usenet', /ran out of space/],
    ['something nobody has seen before', 'usenet', /could not be finished/],
  ]
  for (const [msg, proto, want] of cases) {
    const h = failureHelp(msg, proto)
    assert.ok(h, msg)
    assert.match(h.why, want, msg)
    assert.ok(h.tips.length > 0)
  }
})

test('missing parts suggest another release, a wider quality profile and a second provider', () => {
  const h = failureHelp("12 articles couldn't be found on your Usenet servers", 'usenet')!
  const all = h.tips.join(' ')
  assert.match(all, /Blocklist & search again/)
  assert.match(all, /quality profile/)
  assert.match(all, /second Usenet provider/)
})

test('no message, no help', () => {
  assert.equal(failureHelp('', 'usenet'), null)
  assert.equal(failureHelp(undefined, undefined), null)
})
