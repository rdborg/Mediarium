// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import * as s from './librarySelection.ts'

const items = (...keys: string[]) => keys.map((key) => ({ key }))

test('toggling adds and removes without touching the old set', () => {
  const before = new Set(['a'])
  const added = s.toggleKey(before, 'b')
  assert.deepEqual([...added].sort(), ['a', 'b'])
  assert.deepEqual([...before], ['a'])
  assert.deepEqual([...s.toggleKey(added, 'a')], ['b'])
})

test('shift-click ticks everything between the last tick and this one', () => {
  const list = items('a', 'b', 'c', 'd', 'e')
  assert.deepEqual([...s.selectRange(new Set(['b']), list, 'b', 'd')].sort(), ['b', 'c', 'd'])
  assert.deepEqual([...s.selectRange(new Set(['d']), list, 'd', 'b')].sort(), ['b', 'c', 'd'])
  assert.deepEqual([...s.selectRange(new Set(['a']), list, 'b', 'e')].sort(), ['a', 'b', 'c', 'd', 'e'], 'keeps what was ticked before')
  assert.deepEqual([...s.selectRange(new Set(), list, null, 'c')], ['c'], 'no anchor is a plain tick')
  assert.deepEqual([...s.selectRange(new Set(), list, 'gone', 'c')], ['c'], 'an anchor that left the list is a plain tick')
  assert.deepEqual([...s.selectRange(new Set(['c']), list, null, 'c')], [], 'a plain tick on a ticked item unticks it')
})

test('select all takes exactly what is in the view', () => {
  assert.deepEqual([...s.selectAllKeys(items('movie-1', 'movie-2'))], ['movie-1', 'movie-2'])
  assert.equal(s.selectAllKeys([]).size, 0)
})

test('chosen items come back in list order', () => {
  const list = [{ key: 'a', n: 1 }, { key: 'b', n: 2 }, { key: 'c', n: 3 }]
  assert.deepEqual(s.chosenOf(list, new Set(['c', 'a'])).map((i) => i.n), [1, 3])
  assert.deepEqual(s.chosenOf(list, new Set(['c', 'a', 'hidden'])).map((i) => i.n), [1, 3], 'a ticked title that is not in the view is never reached')
  assert.deepEqual(s.chosenOf([], new Set(['a'])), [])
})

test('counting words', () => {
  const rows: [number, s.Noun, string][] = [
    [0, 'movie', '0 movies'],
    [1, 'movie', '1 movie'],
    [12, 'show', '12 shows'],
    [1, 'artist', '1 artist'],
  ]
  for (const [n, noun, want] of rows) assert.equal(s.count(n, noun), want)
})

test('the bar says how many are selected and what select all reaches', () => {
  const rows: [string, { selected: number; shown: number; total: number; noun: s.Noun }, Partial<s.SelectionText>][] = [
    ['nothing yet, whole library in view', { selected: 0, shown: 48, total: 48, noun: 'movie' }, {
      count: 'Nothing selected', selectAll: 'Select all 48 in this view', allSelected: false, scope: 'Everything in your library is in this view (48 movies).',
    }],
    ['some selected', { selected: 12, shown: 48, total: 48, noun: 'movie' }, { count: '12 selected', allSelected: false }],
    ['all selected', { selected: 48, shown: 48, total: 48, noun: 'show' }, {
      count: '48 selected', selectAll: 'All 48 in this view are selected', allSelected: true, scope: 'Everything in your library is in this view (48 shows).',
    }],
    ['a filter is on', { selected: 0, shown: 7, total: 300, noun: 'show' }, {
      selectAll: 'Select all 7 in this view', scope: 'Filters are on: this view has 7 of the 300 shows in your library.',
    }],
    ['a filter is on, one match', { selected: 1, shown: 1, total: 20, noun: 'artist' }, {
      allSelected: true, scope: 'Filters are on: this view has 1 of the 20 artists in your library.',
    }],
    ['nothing in the view', { selected: 0, shown: 0, total: 5, noun: 'movie' }, { selectAll: '', scope: '', allSelected: false }],
  ]
  for (const [name, input, want] of rows) {
    const got = s.selectionText(input)
    for (const [k, v] of Object.entries(want)) assert.equal(got[k as keyof s.SelectionText], v, `${name}: ${k}`)
  }
})

test('names in a question about removing', () => {
  const rows: [string[], number | undefined, string][] = [
    [[], undefined, ''],
    [['Alpha'], undefined, 'Alpha'],
    [['Alpha', 'Beta'], undefined, 'Alpha and Beta'],
    [['Alpha', 'Beta', 'Gamma'], undefined, 'Alpha, Beta and Gamma'],
    [['A', 'B', 'C', 'D', 'E', 'F', 'G'], undefined, 'A, B, C, D, E and 2 more'],
    [['A', 'B', 'C'], 2, 'A, B and 1 more'],
  ]
  for (const [names, max, want] of rows) assert.equal(s.nameList(names, max), want)
})

test('what could not be done is listed with a name and a reason', () => {
  const failed = [
    { kind: 'movie', id: 1, title: 'Alpha', reason: 'It is outside the library folder.' },
    { kind: 'tv', id: 2, reason: "It isn't in your library any more." },
  ]
  const titleOf = (kind: string, id: number) => (kind === 'tv' && id === 2 ? 'Beta' : undefined)
  assert.deepEqual(s.failureLines(failed, titleOf), ['Alpha: It is outside the library folder.', "Beta: It isn't in your library any more."])
  assert.deepEqual(s.failureLines([{ kind: 'movie', id: 9, reason: 'Gone.' }], () => undefined), ['A title: Gone.'])
  const many = Array.from({ length: 10 }, (_, i) => ({ kind: 'movie', id: i, title: `T${i}`, reason: 'No.' }))
  const lines = s.failureLines(many, () => undefined, 3)
  assert.equal(lines.length, 4)
  assert.equal(lines[3], 'And 7 more.')
})
