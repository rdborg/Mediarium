// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { commandsFor, matching } from './commands.ts'

const settings = [
  { to: '/settings/indexers', label: 'Indexers & Search' },
  { to: '/settings/quality', label: 'Quality' },
  { to: '/settings/media', label: 'Folders' },
]

test('pages, settings pages and actions are found by name, best first', () => {
  const cmds = commandsFor(settings, true)
  assert.equal(matching(cmds, 'index')[0].to, '/settings/indexers')
  assert.equal(matching(cmds, 'qual')[0].to, '/settings/quality')
  assert.equal(matching(cmds, 'pause')[0].action, 'pause-all')
  assert.equal(matching(cmds, 'queue')[0].to, '/queue', 'found by its extra words')
  assert.deepEqual(matching(cmds, 'zzzz'), [])
  assert.ok(matching(cmds, '').length > 0, 'nothing typed lists the first commands')
})

test('actions are only offered to administrators', () => {
  const cmds = commandsFor(settings, false)
  assert.ok(!cmds.some((c) => c.action), 'no actions for other accounts')
  assert.equal(matching(cmds, 'pause').length, 0)
})
