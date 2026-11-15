// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { checkByHand, type ByHandForm } from './addByHand.ts'

const now = new Date('2026-11-15T12:00:00Z')
const book: ByHandForm = { kind: 'book', title: 'Home Brew', by: 'A. Writer', year: '', externalId: '', ebook: true, audiobook: false }
const album: ByHandForm = { kind: 'music', title: 'Handmade', by: 'Hand Band', year: '2003', externalId: '', ebook: false, audiobook: false }

test('a book with a title and a format can be sent, an album needs its artist too', () => {
  assert.equal(checkByHand(book, now), '')
  assert.equal(checkByHand(album, now), '')
  assert.notEqual(checkByHand({ ...album, by: '  ' }, now), '')
  assert.equal(checkByHand({ ...book, by: '' }, now), '') // the author is optional
})

test('the title is required', () => {
  assert.notEqual(checkByHand({ ...book, title: ' ' }, now), '')
  assert.notEqual(checkByHand({ ...album, title: '' }, now), '')
})

test('the year is empty or a believable one', () => {
  assert.equal(checkByHand({ ...book, year: '2031' }, now), '')
  for (const year of ['12', '2032', '20x1']) assert.notEqual(checkByHand({ ...book, year }, now), '', year)
})

test('an own ID has no odd characters', () => {
  assert.equal(checkByHand({ ...book, externalId: '9781234567897' }, now), '')
  assert.equal(checkByHand({ ...album, externalId: 'CAT-001' }, now), '')
  for (const externalId of ['a b', '../x', '-x']) assert.notEqual(checkByHand({ ...book, externalId }, now), '', externalId)
})

test('a book needs at least one format', () => {
  assert.notEqual(checkByHand({ ...book, ebook: false, audiobook: false }, now), '')
  assert.equal(checkByHand({ ...book, ebook: false, audiobook: true }, now), '')
})
