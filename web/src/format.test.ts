// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { qualityText } from './format.ts'

test('a quality is shown as it is stored', () => {
  assert.equal(qualityText('WEBDL-1080p'), 'WEBDL-1080p')
  assert.equal(qualityText('Bluray-2160p'), 'Bluray-2160p')
})

test('a file that does not say its quality reads Not stated', () => {
  assert.equal(qualityText('Unknown'), 'Not stated')
})

test('a missing quality is a dash', () => {
  assert.equal(qualityText(''), '—')
  assert.equal(qualityText(undefined), '—')
  assert.equal(qualityText(null), '—')
})
