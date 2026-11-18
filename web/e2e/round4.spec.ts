import { expect, test, type Page } from '@playwright/test'
import { expectNoErrors, openNav, watchErrors } from './helpers'

// What came with the 4.1 clean-up: the Settings overview and the regrouped menu,
// the "Keep looking for better versions" choices, adding a download by hand for
// an album or a book, the Long wanted box, the quality switch for extra
// languages, the Paused tag on an indexer card, demo mode and the languages.

const demo = (page: Page) => page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))

test('the Settings overview lists every group and each card opens its page', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/settings')
  const overview = page.locator('.settings-overview')
  await expect(overview).toBeVisible()
  const links = overview.locator('a[href^="/settings/"]')
  expect(await links.count()).toBeGreaterThan(10)
  const hrefs = await links.evaluateAll((els) => els.map((e) => e.getAttribute('href') ?? ''))
  for (const want of ['/settings/general', '/settings/quality', '/settings/indexers', '/settings/backup']) expect(hrefs).toContain(want)
  await links.filter({ hasText: /Quality/ }).first().click()
  await expect(page).toHaveURL(/\/settings\/quality$/)
  // the page is not wider than the screen
  const over = await page.locator('main.content').evaluate((el) => el.scrollWidth - el.clientWidth)
  expect(over).toBeLessThanOrEqual(0)
  await expectNoErrors(errors)
})

test('the Settings group in the menu lists the regrouped pages', async ({ page }) => {
  await page.goto('/')
  await openNav(page)
  await page.locator('#sn-head-settings').click()
  await openNav(page)
  const nav = page.getByRole('navigation', { name: 'Main' })
  // seven groups, in the order of the menu
  const names = ['Basics', 'Library and files', 'Finding releases', 'Downloading', 'Media servers and apps', 'People and access', 'System']
  for (const name of names) await expect(nav.getByRole('button', { name, exact: true })).toBeVisible()
  await nav.getByRole('button', { name: 'Finding releases', exact: true }).click()
  await expect(page).toHaveURL(/\/settings\//)
  await expect(page.locator('h1').first()).toContainText('Finding releases')
})

test('the add dialog starts from the setting, and remembers what you change', async ({ page }) => {
  const errors = watchErrors(page)
  await demo(page)
  await page.goto('/discover?kind=movie')
  const open = async () => {
    await page.getByRole('button', { name: /^Add/ }).first().click()
    return page.getByLabel(/Keep looking for better versions after it is downloaded/)
  }
  // The demo setting is on, so a browser that remembers nothing starts ticked.
  let box = await open()
  await expect(box).toBeChecked()
  await box.uncheck()
  await page.keyboard.press('Escape')
  box = await open()
  await expect(box).not.toBeChecked()
  expect(await page.evaluate(() => localStorage.getItem('mediarium.add.keepLookingForBetter'))).toBe('0')
  await expectNoErrors(errors)
})

test('Add a download by hand: a book page names the book and its format', async ({ page }) => {
  const errors = watchErrors(page)
  await demo(page)
  await page.goto('/book/2')
  await page.getByRole('button', { name: 'Add a download by hand' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add a download by hand' })
  await expect(dialog).toContainText('The Long Tide Ledger')
  await expect(dialog.getByRole('button', { name: 'Add download' })).toBeVisible()
  // nothing is asked about which book: it is already chosen
  await expect(dialog.getByRole('button', { name: 'Episode or season' })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toHaveCount(0)
  await expectNoErrors(errors)
})

test('Add a download by hand on Activity offers albums and books when they are on', async ({ page }) => {
  const errors = watchErrors(page)
  // switch the two media types on for this page only
  await page.route(/\/api\/modules$/, async (route) => {
    const response = await route.fetch()
    const json = await response.json()
    json.music = { enabled: true, available: true }
    json.ebooks = { enabled: true, available: true }
    await route.fulfill({ response, json })
  })
  await page.goto('/queue')
  await page.getByRole('button', { name: 'Add a download by hand' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add a download by hand' })
  await expect(dialog.getByText('Movie', { exact: true }).first()).toBeVisible()
  await expect(dialog.getByText('Album', { exact: true }).first()).toBeVisible()
  await expect(dialog.getByText('Book', { exact: true }).first()).toBeVisible()
  await dialog.getByText('Album', { exact: true }).first().click()
  await expect(dialog.getByText(/Nothing in your library matches/)).toBeVisible()
  await dialog.getByText('Book', { exact: true }).first().click()
  await expect(dialog.getByRole('button', { name: 'Add download' })).toBeVisible()
  await expectNoErrors(errors)
})

test('Long wanted: removing asks about the files, unticked by default', async ({ page }) => {
  const errors = watchErrors(page)
  await page.route(/\/api\/wanted\/review\?/, (route) =>
    route.fulfill({
      json: [
        { kind: 'movie', id: 1, tmdbId: 9001, title: 'Old Missing Film', year: 2001, outSince: '2001-05-01', addedAt: '2024-01-02', origin: { kind: 'list', name: 'My list' } },
        { kind: 'tv', id: 2, tmdbId: 9002, title: 'Old Missing Show', year: 2003, outSince: '2003-02-02', missing: 4, origin: { kind: '' } },
      ],
    }),
  )
  await page.goto('/wanted?tab=review')
  await expect(page.getByText('Old Missing Film')).toBeVisible()
  await page.getByLabel('Old Missing Film').check()
  await page.getByRole('button', { name: 'Remove', exact: true }).click()
  const dialog = page.getByRole('alertdialog').or(page.getByRole('dialog')).last()
  const files = dialog.getByLabel(/Also delete its files from the disk/)
  await expect(files).toBeVisible()
  await expect(files).not.toBeChecked()
  await expect(dialog).toContainText('cannot be undone')
  await dialog.getByRole('button', { name: 'Go back' }).click()
  await expect(files).toHaveCount(0)
  await expectNoErrors(errors)
})

test('Quality: "Extra languages don\'t change the ranking" is off, saves and comes back', async ({ page, request }) => {
  const errors = watchErrors(page)
  await page.goto('/settings/quality')
  const sw = page.getByRole('switch', { name: "Extra languages don't change the ranking" })
  await expect(sw).toBeVisible()
  await expect(sw).not.toBeChecked()
  const saved = () => page.waitForResponse((r) => r.request().method() === 'PUT' && /\/api\/settings$/.test(new URL(r.url()).pathname))
  try {
    const turnedOn = saved()
    await page.locator('.switch-row', { has: sw }).locator('.switch-track').click()
    await turnedOn
    await expect(sw).toBeChecked()
    expect((await (await request.get('/api/settings')).json()).qualityLanguageIgnoreExtra).toBe(true)
    await page.reload()
    await expect(page.getByRole('switch', { name: "Extra languages don't change the ranking" })).toBeChecked()
  } finally {
    const sw2 = page.getByRole('switch', { name: "Extra languages don't change the ranking" })
    if (await sw2.isChecked()) {
      const turnedOff = saved()
      await page.locator('.switch-row', { has: sw2 }).locator('.switch-track').click()
      await turnedOff
    }
  }
  expect((await (await request.get('/api/settings')).json()).qualityLanguageIgnoreExtra).toBe(false)
  await expectNoErrors(errors)
})

test('a paused indexer shows a Paused tag with the reason; a healthy one does not', async ({ page }) => {
  const errors = watchErrors(page)
  const until = new Date(Date.now() + 40 * 60_000).toISOString()
  await page.route(/\/api\/indexers$/, (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    return route.fulfill({
      json: [
        { id: 1, name: 'Sleepy NZB', definitionId: '', baseUrl: 'https://api.sleepy.example.com', protocol: 'usenet', enabled: true, kind: 'newznab', priority: 25, failures: 3, pausedUntil: until, pausedWhy: 'the key was refused' },
        { id: 2, name: 'Awake NZB', definitionId: '', baseUrl: 'https://api.awake.example.com', protocol: 'usenet', enabled: true, kind: 'newznab', priority: 25 },
      ],
    })
  })
  await page.goto('/settings/indexers')
  const sleepy = page.locator('.indexer-card', { hasText: 'Sleepy NZB' })
  const tag = sleepy.locator('.badge.paused')
  await expect(tag).toHaveText('Paused')
  await expect(tag).toHaveAttribute('title', /the key was refused/)
  await expect(sleepy).toContainText('Paused until')
  await expect(page.locator('.indexer-card', { hasText: 'Awake NZB' }).locator('.badge.paused')).toHaveCount(0)
  await expectNoErrors(errors)
})

test('demo mode: the main pages open with made-up data and no errors', async ({ page }) => {
  const errors = watchErrors(page)
  const failed: string[] = []
  page.on('requestfailed', (r) => failed.push(`${r.method()} ${r.url()}`))
  await demo(page)
  for (const path of ['/', '/library?kind=movie', '/library?kind=tv', '/discover', '/calendar', '/wanted', '/queue', '/stats', '/settings/indexers', '/settings/quality']) {
    await page.goto(path)
    await expect(page.getByText('Something went wrong')).toHaveCount(0)
    await expect(page.locator('main.content').first()).toBeVisible()
  }
  // (the chip is hidden on a narrow screen, so it only has to be there)
  await expect(page.locator('.demo-chip')).toHaveCount(1)
  await expectNoErrors(errors)
  expect(failed.filter((u) => !/\/(events|stream)/.test(u)), failed.join('\n')).toEqual([])
})

// The app in German, Russian and Chinese: the menu is translated and no page
// shows an untranslated key or an error.
for (const [code, library, quality] of [
  ['de', 'Bibliothek', 'Releases finden'],
  ['ru', 'Библиотека', 'Поиск релизов'],
  ['zh-CN', '媒体库', '查找资源'],
] as const) {
  test(`the language switch: ${code}`, async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/settings/general')
    const language = page.getByRole('combobox', { name: /language|sprache|язык|语言/i })
    await language.selectOption(code)
    await expect(page.locator('html')).toHaveAttribute('lang', code)
    await openNav(page)
    await expect(page.getByRole('navigation').first().getByText(library).first()).toBeVisible()
    await page.goto('/settings/quality')
    await expect(page.locator('h1').first()).not.toContainText('Finding releases')
    await expect(page.locator('h1').first()).toContainText(quality)
    // no sideways scroll in a longer language
    const over = await page.locator('main.content').evaluate((el) => el.scrollWidth - el.clientWidth)
    expect(over).toBeLessThanOrEqual(0)
    await page.goto('/settings/general')
    await language.selectOption('en')
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
    await expectNoErrors(errors)
  })
}

// Touch targets on a phone: the small round buttons in cards and rows, and the
// season chips, are big enough for a finger.
test('small buttons have a 40px touch target on a phone', async ({ page, isMobile }) => {
  test.skip(!isMobile || (page.viewportSize()?.width ?? 0) > 700, 'phone only')
  await demo(page)
  await page.goto('/discover?kind=movie')
  const hide = page.locator('.pcard-footer .not-interested').first()
  await expect(hide).toBeVisible()
  const box = await hide.boundingBox()
  expect(box?.width).toBeGreaterThanOrEqual(40)
  expect(box?.height).toBeGreaterThanOrEqual(40)
  await page.goto('/library?kind=tv')
  await page.getByText('Ember Road').first().click()
  const chip = page.locator('.season-bar .chip').first()
  await expect(chip).toBeVisible()
  expect((await chip.boundingBox())?.height).toBeGreaterThanOrEqual(38)
})
