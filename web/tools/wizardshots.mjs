// Retakes the setup wizard screenshots in docs/images (onboarding-*). Not part of the build or CI.
//
//   cd web
//   MEDIARIUM_URL=http://127.0.0.1:8264 SHOT_USER=mediarium SHOT_PASSWORD=... node tools/wizardshots.mjs
//
// Point it at a throwaway copy that has an administrator called "Mediarium" and has NOT
// finished the setup wizard yet (the wizard only shows until it is done). It walks the
// steps and takes the Media types, Library paths, Quality & naming and Media server ones.
import { chromium } from '@playwright/test'
import { mkdirSync } from 'node:fs'

const base = process.env.MEDIARIUM_URL ?? 'http://127.0.0.1:8264'
const user = process.env.SHOT_USER ?? 'mediarium'
const password = process.env.SHOT_PASSWORD
const out = process.env.SHOT_OUT ?? '../docs/images'
if (!password) throw new Error('Set SHOT_PASSWORD (and SHOT_USER) to the administrator of the throwaway copy')

const browser = await chromium.launch({ channel: process.env.PW_CHANNEL || undefined })
const context = await browser.newContext({ viewport: { width: 1440, height: 1300 }, colorScheme: 'dark', timezoneId: 'UTC', baseURL: base })
await context.addInitScript(() => localStorage.setItem('mediarium-theme', 'dark'))
const login = await context.request.post('/api/auth/login', { data: { username: user, password } })
if (!login.ok()) throw new Error(`sign-in failed: ${login.status()}`)
const page = await context.newPage()
await page.goto('/')
await page.addStyleTag({ content: '.side-version,.demo-chip{display:none !important}' })
mkdirSync(out, { recursive: true })

const wanted = { 'Media types': 'onboarding-media-types', 'Library paths': 'onboarding-library-paths', 'Quality & naming': 'onboarding-quality-naming', 'Media server': 'onboarding-media-server' }
// How much of the page each shot keeps (the rest is empty backdrop).
const heights = { 'Media types': 840, 'Library paths': 900, 'Quality & naming': 1250, 'Media server': 640 }
const next = page.getByRole('button', { name: /^(Continue anyway|Skip for now|Continue|Finish)/ }).first()
const current = async () => (await page.locator('h1.sr-only').first().textContent().catch(() => '')) ?? ''
for (let i = 0; i < 12; i++) {
  await page.waitForTimeout(1500)
  const h = await current()
  const key = Object.keys(wanted).find((k) => h.endsWith(k))
  if (h.endsWith('Media types')) {
    // Movies and TV shows start ticked; the shot shows the others ticked too.
    for (const n of ['Music', 'Ebooks', 'Audiobooks']) await page.getByRole('checkbox', { name: new RegExp(`^${n}`) }).click()
    await page.waitForTimeout(500)
  }
  if (key) {
    await page.mouse.move(2, 2)
    await page.screenshot({ path: `${out}/${wanted[key]}.png`, clip: { x: 0, y: 0, width: 1440, height: heights[key] } })
    console.log('wrote', wanted[key])
  }
  if (!h) break
  await next.click()
  await page.waitForTimeout(500)
  const dialog = page.locator('[role="dialog"], [role="alertdialog"]').first()
  if (await dialog.isVisible()) await dialog.getByRole('button', { name: /^(Continue anyway|Skip|Continue)/ }).first().click()
}
console.log('buttons on the last step:', await page.getByRole('button').allTextContents())
await browser.close()
