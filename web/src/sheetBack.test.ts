// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { sheetBack, type SheetEnv } from './sheetBack.ts'

// A pretend phone: a history of entries (true = a pop-up's) and pop-ups that
// open and close.
function phone(isPhone = true) {
  const h = { entries: [false], at: 0, open: 0, closed: 0, pops: 0 }
  const env: SheetEnv = {
    isPhone: () => isPhone,
    openCount: () => h.open,
    push: () => {
      h.entries = [...h.entries.slice(0, h.at + 1), true]
      h.at++
    },
    back: () => {
      h.at--
      h.pops++ // the browser answers with popstate
    },
    currentIsSheet: () => h.entries[h.at],
    close: () => {
      h.open--
      h.closed++
    },
  }
  return { h, sb: sheetBack(env) }
}

test('Back closes an open pop-up and stays on the page', () => {
  const { h, sb } = phone()
  h.open = 1
  sb.changed()
  assert.deepEqual(h.entries, [false, true])
  h.at-- // the person presses Back
  sb.popped()
  assert.equal(h.closed, 1)
  sb.changed() // the pop-up is gone
  assert.equal(h.at, 0, 'no second step back')
  assert.equal(h.pops, 0)
})

test('a pop-up closed with its own button takes its entry back out', () => {
  const { h, sb } = phone()
  h.open = 1
  sb.changed()
  h.open = 0
  sb.changed()
  assert.equal(h.at, 0)
  assert.equal(h.pops, 1)
  sb.popped() // the popstate that back() caused
  assert.equal(h.closed, 0)
  assert.equal(h.pops, 1, 'nothing more happens')
})

test('a pop-up closed by following a link leaves the new page alone', () => {
  const { h, sb } = phone()
  h.open = 1
  sb.changed()
  h.entries.push(false) // the link adds the new page
  h.at++
  h.open = 0
  sb.changed()
  assert.equal(h.at, 2, 'still on the new page')
  // Back from the new page lands on the old pop-up's entry, which is stepped over.
  h.at--
  sb.popped()
  assert.equal(h.at, 0)
})

test('nothing happens on a wide screen', () => {
  const { h, sb } = phone(false)
  h.open = 1
  sb.changed()
  assert.deepEqual(h.entries, [false])
  h.open = 0
  sb.changed()
  assert.equal(h.pops, 0)
})

test('a second pop-up on top of the first adds no entry', () => {
  const { h, sb } = phone()
  h.open = 1
  sb.changed()
  h.open = 2
  sb.changed()
  assert.deepEqual(h.entries, [false, true])
})
