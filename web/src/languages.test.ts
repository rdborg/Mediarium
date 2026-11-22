// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { AUDIO_LANGUAGES, SUBTITLE_LANGUAGES, languageName } from './languages.ts'

test('every subtitle language has its own code', () => {
  const codes = SUBTITLE_LANGUAGES.map((l) => l.code)
  assert.equal(new Set(codes).size, codes.length)
  for (const c of codes) assert.match(c, /^[a-z]{2,3}(-[A-Z]{2,8})?$/)
})

test('the south Slavic languages and their neighbours are offered, with the OpenSubtitles codes', () => {
  const want: Record<string, string> = {
    hr: 'Croatian', sr: 'Serbian', bs: 'Bosnian', sl: 'Slovenian', sk: 'Slovak', bg: 'Bulgarian', uk: 'Ukrainian',
    ca: 'Catalan', id: 'Indonesian', th: 'Thai', vi: 'Vietnamese', hi: 'Hindi', fa: 'Persian',
  }
  for (const [code, name] of Object.entries(want)) assert.equal(languageName(code), name)
})

test('an unknown code is shown as it is', () => {
  assert.equal(languageName('xx'), 'xx')
})

test('the audio languages are listed once each, English first', () => {
  assert.equal(AUDIO_LANGUAGES[0], 'English')
  assert.equal(new Set(AUDIO_LANGUAGES).size, AUDIO_LANGUAGES.length)
  for (const n of ['Croatian', 'Serbian', 'Bosnian', 'Slovenian', 'Bulgarian']) assert.ok(AUDIO_LANGUAGES.includes(n), n)
})
