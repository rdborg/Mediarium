// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { isTypingTarget } from './shortcuts.ts'

test('fields you type in are typing', () => {
  assert.equal(isTypingTarget({ tagName: 'INPUT', type: 'text' } as unknown as EventTarget), true)
  assert.equal(isTypingTarget({ tagName: 'INPUT', type: 'search' } as unknown as EventTarget), true)
  assert.equal(isTypingTarget({ tagName: 'INPUT', type: 'password' } as unknown as EventTarget), true)
  assert.equal(isTypingTarget({ tagName: 'INPUT' } as unknown as EventTarget), true)
  assert.equal(isTypingTarget({ tagName: 'TEXTAREA' } as unknown as EventTarget), true)
  assert.equal(isTypingTarget({ tagName: 'SELECT' } as unknown as EventTarget), true)
  assert.equal(isTypingTarget({ tagName: 'DIV', isContentEditable: true } as unknown as EventTarget), true)
})

test('buttons, tick boxes and the page itself are not typing', () => {
  assert.equal(isTypingTarget({ tagName: 'INPUT', type: 'checkbox' } as unknown as EventTarget), false)
  assert.equal(isTypingTarget({ tagName: 'INPUT', type: 'radio' } as unknown as EventTarget), false)
  assert.equal(isTypingTarget({ tagName: 'BUTTON' } as unknown as EventTarget), false)
  assert.equal(isTypingTarget({ tagName: 'A' } as unknown as EventTarget), false)
  assert.equal(isTypingTarget({ tagName: 'BODY' } as unknown as EventTarget), false)
  assert.equal(isTypingTarget(null), false)
  assert.equal(isTypingTarget({} as EventTarget), false)
})
