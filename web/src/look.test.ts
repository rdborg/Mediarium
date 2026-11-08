import test from 'node:test'
import assert from 'node:assert/strict'
import { themeColor } from './look.ts'

test('themeColor is black only for the dark look with pure black on', () => {
  assert.equal(themeColor('dark', true), '#000000')
  assert.equal(themeColor(undefined, true), '#000000')
  assert.equal(themeColor('dark', false), '#0b0d18')
  assert.equal(themeColor('light', true), '#0b0d18')
  assert.equal(themeColor('light', false), '#0b0d18')
})
