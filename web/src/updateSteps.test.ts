import assert from 'node:assert/strict'
import { test } from 'node:test'
import { checkedText, installSteps, programSummary, waitForApp } from './updateSteps.ts'

test('a Docker install gets the three sets of steps', () => {
  const { groups, notes } = installSteps({ kind: 'docker', full: false })
  assert.deepEqual(
    groups.map((g) => g.title),
    ['Synology Container Manager', 'Unraid', 'Docker Compose'],
  )
  assert.ok(groups.every((g) => g.steps.length > 0))
  assert.ok(groups[2].steps.some((s) => s.includes('docker compose pull')))
  assert.ok(!notes.join(' ').includes('-full'))
})

test('the -full image is told to keep its tag', () => {
  const { notes } = installSteps({ kind: 'docker', full: true })
  assert.ok(notes.some((n) => n.includes('-full') && n.includes('latest-full')))
})

test('a native install gets one plain list', () => {
  const { groups } = installSteps({ kind: 'native', full: false })
  assert.equal(groups.length, 1)
  assert.ok(groups[0].steps[0].toLowerCase().includes('backup'))
})

test('the program summary says where the running program came from', () => {
  assert.equal(programSummary({ running: '1.2.0', image: '', pushed: null }), 'You are running Mediarium 1.2.0.')
  assert.equal(programSummary({ running: '1.1.0', image: '1.1.0', pushed: null }), 'You are running Mediarium 1.1.0.')
  assert.match(programSummary({ running: '1.2.0', image: '1.1.0', pushed: { version: '1.2.0', running: true } }), /installed on top of the Docker image, which has 1\.1\.0/)
  assert.match(programSummary({ running: '1.1.0', image: '1.1.0', pushed: { version: '1.2.0', running: false } }), /waiting for the next restart/)
})

test('the last check is described without jargon', () => {
  const ago = (iso: string | undefined) => (iso ? '3 hours ago' : '')
  assert.equal(checkedText({ enabled: true, checkedAt: '2026-10-01T10:00:00Z' }, ago), 'Checked 3 hours ago.')
  assert.equal(checkedText({ enabled: true }, ago), 'Not checked yet.')
  assert.equal(checkedText({ enabled: false }, ago), 'Checking is switched off. Press Check now to look.')
  assert.equal(checkedText({ enabled: true, error: "Couldn't check just now." }, ago), "Couldn't check just now.")
  assert.equal(
    checkedText({ enabled: true, error: "Couldn't check just now.", checkedAt: '2026-10-01T10:00:00Z' }, ago),
    "Couldn't check just now. The last good check was 3 hours ago.",
  )
})

// A fake clock: sleeping moves time forward.
function clock() {
  let t = 0
  return { now: () => t, sleep: async (ms: number) => void (t += ms) }
}

test('waiting for the app: it is not believed before the minimum time', async () => {
  const c = clock()
  const seen: number[] = []
  const result = await waitForApp({
    ...c,
    fetchVersion: async () => {
      seen.push(c.now())
      return '1.2.0' // the old process still answers at first
    },
    minMs: 4000,
  })
  assert.equal(result, 'back')
  assert.ok(seen[0] < 4000 && c.now() >= 4000)
})

test('waiting for the app: it keeps waiting through the gap while it restarts', async () => {
  const c = clock()
  const answers = ['1.1.0', null, null, null, '1.2.0']
  let i = 0
  const result = await waitForApp({ ...c, expectVersion: '1.2.0', minMs: 0, fetchVersion: async () => answers[Math.min(i++, answers.length - 1)] })
  assert.equal(result, 'back')
  assert.equal(i, 5, 'the old version must not count as back')
})

test('waiting for the app: it gives up in the end', async () => {
  const c = clock()
  const result = await waitForApp({ ...c, fetchVersion: async () => null, maxMs: 20000 })
  assert.equal(result, 'timeout')
  assert.ok(c.now() > 20000 && c.now() <= 24000)
})
