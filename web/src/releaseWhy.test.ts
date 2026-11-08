// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { loadShowWhy, saveShowWhy, signed, whyOf } from './releaseWhy.ts'
import { scoreOf } from './releaseSort.ts'

test('a score is shown with its sign, zero plain', () => {
  assert.deepEqual([100, -50, 0].map(signed), ['+100', '-50', '0'])
})

test('a release with nothing to explain has no details', () => {
  assert.equal(whyOf({}), null)
  assert.equal(whyOf({ formats: [], reasons: [] }), null)
})

test('the matched formats, the total and the profile limits come together', () => {
  const w = whyOf({ formatScore: 50, formats: [{ name: 'Remux', score: 100 }, { name: 'x265', score: -50 }], minScore: 0, upgradeUntil: 200 })
  assert.ok(w)
  assert.equal(w.score, 50)
  assert.equal(w.formats.length, 2)
  assert.equal(w.minScore, 0)
  assert.equal(w.upgradeUntil, 200)
  assert.equal(w.belowMin, false)
})

test('a total under the minimum is flagged, a total at the minimum is not', () => {
  assert.equal(whyOf({ formatScore: -50, formats: [{ name: 'x265', score: -50 }], minScore: 0 })?.belowMin, true)
  assert.equal(whyOf({ formatScore: 0, minScore: 0, formats: [] })?.belowMin, false)
  assert.equal(whyOf({ formatScore: 90, minScore: 100, formats: [] })?.belowMin, true)
})

test('the reasons come from the server, or from the old rejections when it sent none', () => {
  const a = whyOf({ reasons: [{ kind: 'size', text: 'too big' }], rejections: ['ignored'] })
  assert.deepEqual(a?.reasons, [{ kind: 'size', text: 'too big' }])
  const b = whyOf({ rejections: ['not an upgrade over WEBDL-1080p'] })
  assert.deepEqual(b?.reasons, [{ kind: 'rule', text: 'not an upgrade over WEBDL-1080p' }])
})

test('a score without any formats still has details: the total and the limits', () => {
  const w = whyOf({ formatScore: 0, minScore: 0 })
  assert.ok(w)
  assert.deepEqual(w.formats, [])
})

test('the "Show why" choice is remembered, and storage that throws changes nothing', () => {
  const data = new Map<string, string>()
  const store = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) }
  assert.equal(loadShowWhy(store), false)
  saveShowWhy(true, store)
  assert.equal(loadShowWhy(store), true)
  saveShowWhy(false, store)
  assert.equal(loadShowWhy(store), false)
  const broken = {
    getItem: () => {
      throw new Error('blocked')
    },
    setItem: () => {
      throw new Error('blocked')
    },
  }
  assert.equal(loadShowWhy(broken), false)
  assert.doesNotThrow(() => saveShowWhy(true, broken))
})

test('matched preferred terms add to the total next to the formats', () => {
  const w = whyOf({ formatScore: 50, formats: [{ name: 'Remux', score: 50 }], terms: [{ term: 'x265-FuN', score: 100 }, { term: 'x265', score: 50 }] })
  assert.ok(w)
  assert.equal(w.total, 200)
  assert.equal(w.score, 50)
  const only = whyOf({ terms: [{ term: 'x265', score: -20 }] })
  assert.ok(only)
  assert.equal(only.score, undefined)
  assert.equal(only.total, -20)
  assert.equal(only.belowMin, false)
})

test('why a release was not the best match is kept apart from the reasons it is skipped', () => {
  const w = whyOf({ reasons: [{ kind: 'notBest', text: 'The best match fits your preferred language better.' }] })
  assert.ok(w)
  assert.deepEqual(w.notBest, ['The best match fits your preferred language better.'])
  assert.deepEqual(w.reasons, [])
  const both = whyOf({ reasons: [{ kind: 'notBest', text: 'a' }, { kind: 'size', text: 'b' }] })
  assert.deepEqual(both?.notBest, ['a'])
  assert.deepEqual(both?.reasons, [{ kind: 'size', text: 'b' }])
})

test('the Score column equals the sum of the parts listed in the details, fix bonus included', () => {
  const rows = [
    { score: 150, formatScore: 100, formats: [{ name: 'HDR', score: 100 }], terms: [{ term: 'x265', score: 50 }] },
    { score: 5, fixScore: 5, formatScore: 0, formats: [] },
    { score: -20, terms: [{ term: 'GRP', score: -20 }] },
    { score: 155, fixScore: 5, formatScore: 100, formats: [{ name: 'HDR', score: 100 }], terms: [{ term: 'x265', score: 50 }] },
  ]
  for (const r of rows) {
    const w = whyOf(r)
    assert.ok(w)
    assert.equal(scoreOf(r), w.total)
    assert.equal(w.total, w.formats.reduce((n, f) => n + f.score, 0) + w.terms.reduce((n, x) => n + x.score, 0) + w.fix)
  }
})

test('the checks and the ranking note come through, and ranking is no reason to skip', () => {
  const w = whyOf({
    checks: [{ kind: 'quality', state: 'ok', text: 'Quality: WEBDL-1080p.' }, { kind: 'size', state: 'bad', text: 'too big' }],
    reasons: [{ kind: 'rank', text: 'It ranks above the next release.' }, { kind: 'size', text: 'too big' }],
  })
  assert.ok(w)
  assert.equal(w.checks.length, 2)
  assert.deepEqual(w.rank, ['It ranks above the next release.'])
  assert.deepEqual(w.reasons.map((r) => r.kind), ['size'])
})

test('a release with only checks still has details', () => {
  const w = whyOf({ checks: [{ kind: 'quality', state: 'ok', text: 'Quality: WEBDL-1080p.' }] })
  assert.ok(w)
  assert.equal(w.reasons.length, 0)
  assert.equal(whyOf({ rejections: [], checks: [] }), null)
})
