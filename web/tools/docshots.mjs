// Retakes the screenshots the docs use (docs/images). Not part of the build or CI.
//
//   cd web
//   MEDIARIUM_URL=http://127.0.0.1:8264 SHOT_USER=mediarium SHOT_PASSWORD=... node tools/docshots.mjs [name ...]
//
// Run it against a throwaway copy with demo-style data: an administrator whose display
// name is "Mediarium", a couple of media servers and notifications added with made-up
// addresses, and nothing personal anywhere. Pages that show the library use the app's
// demo mode (made-up titles). Screenshots are dark, 1440 px wide, without the version
// bubble. Give names (for example `settings-menu`) to retake only those.
import { chromium } from '@playwright/test'
import { mkdirSync } from 'node:fs'

const base = process.env.MEDIARIUM_URL ?? 'http://127.0.0.1:8264'
const user = process.env.SHOT_USER ?? 'mediarium'
const password = process.env.SHOT_PASSWORD
const out = process.env.SHOT_OUT ?? '../docs/images'
if (!password) throw new Error('Set SHOT_PASSWORD (and SHOT_USER) to the administrator of the throwaway copy')

const DESKTOP = { width: 1440, height: 900 }
const PHONE = { width: 390, height: 844 }

// Settings pages are shown with the Settings group opened in the sidebar, as a person sees them.
const openSettings = async (page) => {
  await page.getByRole('navigation', { name: 'Main' }).getByText('Settings', { exact: true }).click()
  await page.waitForTimeout(500)
}
// Scrolls the page content until the card titled `title` is near the top of the screen.
const scrollToCard = (title, top = 150) => async (page) => {
  const handle = await page.getByText(title).first().elementHandle()
  await page.evaluate((el) => {
    const main = document.querySelector('main.content')
    if (main) main.scrollTop += el.getBoundingClientRect().top - top
  }, handle)
}
// A build made from the source says "dev" where a release says its number. Public screenshots
// show no version at all, so the words are taken out of the page before the shot.
const hideVersion = async (page) => {
  await page.evaluate(() => {
    const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      if (n.textContent.trim() === 'dev') n.textContent = ''
      const text = n.textContent.replace(/\s\(dev\)/g, '').replace(/Mediarium dev\b/g, 'Mediarium').replace(/version dev\b/g, 'version')
      if (text !== n.textContent) n.textContent = text
    }
  })
}
const SIDEBAR = { x: 0, y: 0, width: 260, height: 720 }

const shots = [
  { name: 'settings-menu', path: '/settings/media-servers', prep: openSettings, clip: SIDEBAR },
  { name: 'settings-usenet', path: '/settings/usenet', prep: openSettings },
  { name: 'settings-torrents', path: '/settings/torrents', prep: openSettings },
  { name: 'system-updates', path: '/settings/general', prep: async (p) => { await openSettings(p); await p.getByText('Advanced: updates pushed through the API').click(); await scrollToCard(/^Updates\??$/)(p) } },
  { name: 'settings-media-servers', path: '/settings/media-servers', prep: openSettings },
  { name: 'settings-notifications', path: '/settings/notifications', prep: openSettings },
  { name: 'settings-logs', path: '/settings/logs', prep: async (p) => { await openSettings(p); await p.getByText('A notification was not sent').first().click() } },
  { name: 'settings-system', path: '/settings/system', prep: openSettings },
  { name: 'settings-indexers', path: '/settings/indexers', demo: true, prep: openSettings },
  // Library and discover pages use the demo mode (made-up titles).
  { name: 'library-bulk', path: '/library?kind=movie', demo: true, prep: async (p) => {
    await p.getByRole('button', { name: 'Select', exact: true }).click()
    for (let i = 0; i < 5; i++) await p.locator('article.pcard').nth(i).click({ position: { x: 60, y: 60 } })
  } },
  { name: 'upcoming-wanted', path: '/wanted', demo: true },
  { name: 'music-library', path: '/library?kind=music', demo: true, clip: { x: 0, y: 0, width: 1440, height: 740 } },
  { name: 'discover-music', path: '/discover?kind=music', demo: true },
  { name: 'book-page', path: '/book/1', demo: true, size: 'tall' },
  { name: 'book-info', path: '/books/work/OLP1W', demo: true },
  { name: 'import-report', path: '/import?kind=tv&batch=1', demo: true, size: 'tall', clip: { x: 0, y: 0, width: 1440, height: 820 } },
  { name: 'import-review', path: '/import', demo: true, size: 'tall', prep: async (p) => { await p.getByRole('button', { name: /Scan folder|Scan/ }).first().click() } },
  { name: 'discover-books', path: '/discover?kind=book', demo: true },
  { name: 'settings-quality', path: '/settings/quality', prep: openSettings },
  { name: 'settings-modules', path: '/settings/modules', prep: openSettings },
  { name: 'other-apps', path: '/settings/other-apps', prep: async (p) => {
    await openSettings(p)
    // The throwaway copy listens on a test port; the shot shows the usual address and port.
    await p.evaluate(() => {
      const fix = (t) => t.replace(/127\.0\.0\.1/g, 'mediarium.local').replace(new RegExp('\\b' + location.port + '\\b', 'g'), '8264')
      const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      for (let n = walk.nextNode(); n; n = walk.nextNode()) { const f = fix(n.textContent); if (f !== n.textContent) n.textContent = f }
      for (const el of document.querySelectorAll('input')) if (el.value) el.value = fix(el.value)
    })
  } },
  // Settings > Finding releases > Automatic searching: the "When you add a title" card.
  { name: 'settings-automatic-searching', path: '/settings/automation', prep: openSettings },
  // The Custom formats card on the Quality page, opened (advanced options show by themselves once there are formats).
  { name: 'custom-formats', path: '/settings/quality', size: 'tall', prep: async (p) => {
    await openSettings(p)
    const bar = p.getByRole('button', { name: /\d+ formats?$/ }).first()
    if ((await bar.getAttribute('aria-expanded')) === 'false') await bar.click()
    await p.waitForTimeout(500)
    const heading = await p.getByRole('heading', { name: /^Custom formats/ }).first().elementHandle()
    await p.evaluate((el) => {
      const main = document.querySelector('main.content')
      if (main) main.scrollTop += el.getBoundingClientRect().top - 100
    }, heading)
  } },
  { name: 'quality-editor', path: '/settings/quality', size: 'tall', prep: async (p) => { await p.getByRole('button', { name: 'Edit', exact: true }).nth(2).click() }, wait: 2500 },
]
const only = process.argv.slice(2)

const browser = await chromium.launch({ channel: process.env.PW_CHANNEL || undefined })
async function take(s) {
  const phone = s.size === 'phone'
  const context = await browser.newContext({
    viewport: phone ? PHONE : s.size === 'tall' ? { width: 1440, height: 1100 } : DESKTOP,
    deviceScaleFactor: phone ? 2 : 1,
    isMobile: phone,
    hasTouch: phone,
    colorScheme: 'dark',
    timezoneId: 'UTC',
    baseURL: base,
  })
  await context.addInitScript((demo) => {
    localStorage.setItem('mediarium-theme', 'dark')
    if (demo) localStorage.setItem('mediarium-demo', '1')
  }, !!s.demo)
  const login = await context.request.post('/api/auth/login', { data: { username: user, password } })
  if (!login.ok()) throw new Error(`sign-in failed: ${login.status()}`)
  const page = await context.newPage()
  await page.goto(s.path)
  await page.addStyleTag({ content: '.side-version,.demo-chip{display:none !important}' })
  await page.waitForLoadState('networkidle').catch(() => undefined)
  if (s.prep) await s.prep(page)
  await page.waitForTimeout(s.wait ?? 1200)
  await hideVersion(page)
  await page.mouse.move(2, 2)
  await page.waitForTimeout(300)
  mkdirSync(out, { recursive: true })
  await page.screenshot({ path: `${out}/${s.name}.png`, fullPage: false, clip: s.clip })
  await context.close()
  console.log('wrote', s.name)
}

for (const s of shots) {
  if (only.length && !only.includes(s.name)) continue
  await take(s).catch((e) => console.error('failed', s.name, String(e.message).split('\n')[0]))
}
await browser.close()
