import { expect, test } from '@playwright/test'
import { menuToggle, openNav } from './helpers'

// Nothing pushes the page wider than the screen, at any of the three sizes
// (run with --project app, app-tablet or app-phone). Russian has the longest
// button labels, so it is the one that shows a button running off the card.
const PAGES = ['/series/101', '/title/9010', '/settings/media', '/settings/system', '/settings/services', '/queue', '/library?kind=tv']

for (const lang of ['en', 'ru']) {
  for (const path of PAGES) {
    test(`${path} does not scroll sideways (${lang})`, async ({ page }) => {
      await page.addInitScript((l) => {
        localStorage.setItem('mediarium-demo', '1')
        localStorage.setItem('mediarium-lang', l)
      }, lang)
      await page.goto(path)
      const content = page.locator('main.content')
      await expect(content).toBeVisible()
      await page.waitForTimeout(800)
      const over = await content.evaluate((el) => el.scrollWidth - el.clientWidth)
      expect(over, `main is ${over}px wider than the screen`).toBeLessThanOrEqual(0)
    })
  }
}

// The same check with a wide fallback font, the way a Linux machine without
// Edge's fonts draws Cyrillic and accented text (DejaVu Sans is much wider than
// Segoe UI). Every settings page is walked in the languages with long words.
const WIDE_FONT = '*{font-family: Verdana, "DejaVu Sans", sans-serif !important}'
const SETTINGS = [
  'modules', 'media', 'naming', 'quality', 'automation', 'indexers', 'usenet', 'torrents', 'download-rules', 'vpn',
  'subtitles', 'metadata', 'lists', 'media-servers', 'other-apps', 'addons', 'migrate', 'notifications', 'profile',
  'system', 'backup', 'accounts', 'requests', 'sign-in', 'general', 'security', 'services', 'logs', 'about', 'statistics',
].map((s) => `/settings/${s}`)

for (const lang of ['en', 'ru', 'de', 'fr', 'pl']) {
  for (const path of [...PAGES.filter((p) => !p.startsWith('/settings')), ...SETTINGS]) {
    test(`${path} does not scroll sideways with a wide font (${lang})`, async ({ page }) => {
      await page.addInitScript((l) => {
        localStorage.setItem('mediarium-demo', '1')
        localStorage.setItem('mediarium-lang', l)
      }, lang)
      await page.goto(path)
      const content = page.locator('main.content')
      await expect(content).toBeVisible()
      await page.addStyleTag({ content: WIDE_FONT })
      await page.waitForTimeout(800)
      const { over, culprits } = await content.evaluate((el) => {
        const edge = el.getBoundingClientRect().right
        const culprits: string[] = []
        for (const child of Array.from(el.querySelectorAll('*'))) {
          const r = child.getBoundingClientRect()
          if (r.width > 0 && r.right > edge + 1 && !child.closest('table, .table-scroll, .ep-table, .lib-list')) {
            culprits.push(`${child.tagName.toLowerCase()}.${String(child.className).split(' ').join('.')} +${Math.round(r.right - edge)}px "${(child.textContent ?? '').trim().slice(0, 30)}"`)
          }
        }
        return { over: el.scrollWidth - el.clientWidth, culprits: culprits.slice(0, 8) }
      })
      expect(over, `main is ${over}px wider than the screen: ${culprits.join(' | ')}`).toBeLessThanOrEqual(0)
    })
  }
}

// The indexer list is sorted by priority (lower first), then by name, and the
// 15 demo indexers fit without the page scrolling sideways in either language.
for (const lang of ['en', 'ru']) {
  test(`/settings/indexers lists 15 demo indexers by priority (${lang})`, async ({ page }) => {
    await page.addInitScript((l) => {
      localStorage.setItem('mediarium-demo', '1')
      localStorage.setItem('mediarium-lang', l)
    }, lang)
    await page.goto('/settings/indexers')
    const cards = page.locator('.indexer-card')
    await expect(cards).toHaveCount(15)
    await expect(cards.first().locator('.indexer-title strong')).toHaveText('Fathom')
    await expect(cards.last().locator('.indexer-title strong')).toHaveText('Stonebridge')
    await expect(page.locator('.indexer-filter')).toBeVisible()
    const over = await page.locator('main.content').evaluate((el) => el.scrollWidth - el.clientWidth)
    expect(over).toBeLessThanOrEqual(0)
  })
}

// Indexer cards in one grid row are all the same height, so no card leaves a
// ragged gap under it however long its status lines are.
for (const width of [1440, 750]) {
  test(`indexer cards in a grid row have the same height (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
    await page.goto('/settings/indexers')
    await expect(page.locator('.indexer-card')).toHaveCount(15)
    const rows = await page.locator('.indexer-card').evaluateAll((els) => {
      const byTop = new Map<number, number[]>()
      for (const e of els) {
        const r = e.getBoundingClientRect()
        const top = Math.round(r.top + window.scrollY)
        byTop.set(top, [...(byTop.get(top) ?? []), Math.round(r.height)])
      }
      return Array.from(byTop.values())
    })
    expect(rows.length).toBeGreaterThan(1)
    for (const heights of rows) expect(new Set(heights).size, `row heights ${heights.join(', ')}`).toBe(1)
  })
}

// The header buttons are big enough for a finger on a tablet or phone.
test('the header buttons are at least 40px on a touch screen', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'phone only')
  await page.goto('/')
  // (on a phone the menu button is More in the bottom bar)
  for (const button of [menuToggle(page), page.locator('button.avatar')]) {
    const box = await button.boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(40)
    expect(box?.height).toBeGreaterThanOrEqual(40)
  }
})

// More tap targets on a phone: the drawer's theme button, the calendar's month
// arrows and the "Browse more" links in Discover.
test('the drawer theme button, month arrows and Browse more are at least 40px on a phone', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'phone only')
  await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
  await page.goto('/calendar')
  for (const name of ['Previous month', 'Next month']) {
    const box = await page.getByRole('button', { name }).first().boundingBox()
    expect(box?.height, `${name} is ${box?.height}px high`).toBeGreaterThanOrEqual(40)
    expect(box?.width, `${name} is ${box?.width}px wide`).toBeGreaterThanOrEqual(40)
  }
  await openNav(page)
  const theme = await page.locator('.sidebar .side-tools .top-icon').first().boundingBox()
  expect(theme?.height).toBeGreaterThanOrEqual(40)
  expect(theme?.width).toBeGreaterThanOrEqual(40)
  await page.goto('/discover')
  const more = page.locator('a.rail-more').first()
  await expect(more).toBeVisible()
  expect((await more.boundingBox())?.height).toBeGreaterThanOrEqual(40)
})

// Cards (and indexer cards) whose boxes overlap, ignoring anything inside a
// closed <details> (its content keeps a box but is not drawn).
const CLASHES = (sel: string) => {
  const els = Array.from(document.querySelectorAll(sel)).filter((e) => {
    if ((e as HTMLElement).offsetParent === null) return false
    const d = e.closest('details')
    return !d || d.open || e === d
  })
  const out: string[] = []
  for (let i = 0; i < els.length; i++) {
    for (let j = i + 1; j < els.length; j++) {
      const a = els[i], b = els[j]
      if (a.contains(b) || b.contains(a)) continue
      const r = a.getBoundingClientRect(), q = b.getBoundingClientRect()
      const w = Math.min(r.right, q.right) - Math.max(r.left, q.left)
      const h = Math.min(r.bottom, q.bottom) - Math.max(r.top, q.top)
      if (w > 1 && h > 1) out.push(`${a.className} / ${b.className}`)
    }
  }
  const pinned = Array.from(document.querySelectorAll('.settings-content *')).filter((e) => ['sticky', 'fixed'].includes(getComputedStyle(e).position))
  return out.concat(pinned.map((e) => `pinned: ${e.className}`))
}

// On Settings > Indexers & Search no card sits on top of another one, however
// far the page is scrolled and at every width from a phone to a wide screen
// (the add card used to be sticky and slid over the sections below it, and the
// two columns used to overlap at about 750px). Run with --project app, app-tablet or app-phone.
for (const width of [750, 900, 1100, 1440]) {
  test(`indexers page: no card overlaps another at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
    await page.goto('/settings/indexers')
    const content = page.locator('main.content')
    await expect(content).toBeVisible()
    await page.waitForTimeout(800)
    const sel = '.settings-content fieldset.group, .settings-content .indexer-card'
    const height = await page.evaluate(() => Math.max(document.documentElement.scrollHeight, document.querySelector('main.content')!.scrollHeight))
    for (let y = 0; y <= height; y += 400) {
      await content.evaluate((el, top) => { el.scrollTo(0, top); window.scrollTo(0, top) }, y)
      expect(await page.evaluate(CLASHES, sel), `overlap at scroll ${y}`).toEqual([])
    }
    const over = await content.evaluate((el) => el.scrollWidth - el.clientWidth)
    expect(over).toBeLessThanOrEqual(0)
  })
}

// Every settings page at 750px: no card's box overlaps another card's box.
test('all settings pages: no card overlaps another at 750px', async ({ page }) => {
  test.setTimeout(240_000)
  await page.setViewportSize({ width: 750, height: 900 })
  await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
  const bad: string[] = []
  for (const path of SETTINGS) {
    await page.goto(path)
    await expect(page.locator('main.content')).toBeVisible()
    await page.waitForTimeout(600)
    const clashes = await page.evaluate(CLASHES, '.settings-content fieldset.group, .settings-content section.card, .settings-content .settings-stack > .card')
    if (clashes.length) bad.push(`${path}: ${clashes.slice(0, 3).join(' | ')}`)
  }
  expect(bad).toEqual([])
})
