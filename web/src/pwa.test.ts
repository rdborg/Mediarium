import assert from 'node:assert/strict'
import { test } from 'node:test'
import { installKind, type InstallEnv } from './pwa.ts'

const SAFARI_MAC = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15'
const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1'
const IPAD = 'Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1'
const CHROME_MAC = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'
const FIREFOX = 'Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0'

const base: InstallEnv = { userAgent: FIREFOX, maxTouchPoints: 0, standalone: false, secure: true, canPrompt: false }

test('installKind picks the help that fits the browser', () => {
  const cases: [string, Partial<InstallEnv>, string][] = [
    ['already an app', { standalone: true, canPrompt: true }, 'installed'],
    ['plain http', { secure: false, userAgent: IPHONE }, 'insecure'],
    ['browser offers a prompt', { canPrompt: true, userAgent: CHROME_MAC }, 'prompt'],
    ['iPhone', { userAgent: IPHONE, maxTouchPoints: 5 }, 'iphone'],
    ['iPad', { userAgent: IPAD, maxTouchPoints: 5 }, 'ipad'],
    ['iPad posing as a Mac', { userAgent: SAFARI_MAC, maxTouchPoints: 5 }, 'ipad'],
    ['Safari on a Mac', { userAgent: SAFARI_MAC }, 'mac-safari'],
    ['Chrome on a Mac without a prompt', { userAgent: CHROME_MAC }, 'menu'],
    ['Firefox', {}, 'menu'],
  ]
  for (const [name, env, want] of cases) assert.equal(installKind({ ...base, ...env }), want, name)
})
