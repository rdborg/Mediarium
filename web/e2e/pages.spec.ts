import { expect, test } from '@playwright/test'
import { expectNoErrors, menuToggle, openNav, watchErrors } from './helpers'

// Every main page opens without an error on a new, empty install.
const PAGES: [string, RegExp][] = [
  ['/', /Dashboard/],
  ['/discover', /Discover/],
  ['/library', /Library/],
  ['/calendar', /Upcoming/],
  ['/wanted', /Upcoming/],
  ['/queue', /Activity/],
  ['/stats', /Statistics/],
  ['/settings/general', /General/],
  ['/settings/modules', /Media types/],
  ['/settings/media', /Library.*Folders/],
  ['/settings/naming', /File names and importing/],
  ['/settings/quality', /Quality/],
  ['/settings/automation', /Automatic searching/],
  ['/settings/indexers', /Indexers/],
  ['/settings/usenet', /Usenet/],
  ['/settings/torrents', /Torrents/],
  ['/settings/vpn', /VPN/],
  ['/settings/download-rules', /Download rules/],
  ['/settings/subtitles', /Subtitles/],
  ['/settings/metadata', /Movie and show info/],
  ['/settings/lists', /Import lists/],
  ['/settings/media-servers', /Media servers/],
  ['/settings/notifications', /Notifications/],
  ['/settings/other-apps', /Other apps/],
  ['/settings/profile', /Your profile/],
  ['/settings/security', /Security/],
  ['/settings/services', /Linked services/],
  ['/settings/accounts', /All accounts/],
  ['/settings/requests', /Requests and reports/],
  ['/settings/sign-in', /Ways to sign in/],
  ['/settings/backup', /Backup and restore/],
  ['/settings/system', /Server/],
  ['/settings/logs', /Logs and errors/],
  ['/settings/migrate', /Move from other apps/],
  ['/settings/about', /About and credits/],
  ['/help', /User guide/],
]

for (const [path, title] of PAGES) {
  test(`opens ${path}`, async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto(path)
    await expect(page).toHaveTitle(title)
    await expect(page.getByText('Something went wrong')).toHaveCount(0)
    await expectNoErrors(errors)
  })
}

// Addresses that moved when the settings were split up still open.
const MOVED: [string, string][] = [
  ['/settings/downloads', '/settings/usenet'],
  ['/settings/language', '/settings/general'],
]
for (const [from, to] of MOVED) {
  test(`${from} opens ${to}`, async ({ page }) => {
    await page.goto(from)
    await expect(page).toHaveURL(new RegExp(`${to}$`))
  })
}

test('/settings is the overview of every group', async ({ page }) => {
  await page.goto('/settings')
  await expect(page).toHaveURL(/\/settings$/)
  await expect(page.locator('.settings-overview')).toBeVisible()
})

test('the Library and Settings menus open only on a click, and only one at a time', async ({ page }) => {
  await page.goto('/')
  await openNav(page)
  const library = page.locator('#sn-head-library')
  const settings = page.locator('#sn-head-settings')
  await expect(library).toBeVisible()
  // on the home page both are closed
  await expect(library).toHaveAttribute('aria-expanded', 'false')
  await expect(settings).toHaveAttribute('aria-expanded', 'false')
  // a tap on a group also opens its first page, which closes the drawer on a phone
  await library.click()
  await expect(library).toHaveAttribute('aria-expanded', 'true')
  await expect(settings).toHaveAttribute('aria-expanded', 'false')
  await openNav(page)
  await settings.click()
  await expect(settings).toHaveAttribute('aria-expanded', 'true')
  await expect(library).toHaveAttribute('aria-expanded', 'false')
  // a reload starts with both closed, even on a page inside a group
  await page.goto('/settings/general')
  await page.reload()
  await openNav(page)
  await expect(library).toHaveAttribute('aria-expanded', 'false')
  await expect(settings).toHaveAttribute('aria-expanded', 'false')
  // while closed, the row names the page you are on
  await expect(settings.locator('.nav-here')).not.toBeEmpty()
})

test('the Library entry lists its kinds, and each one opens the Library on that tab', async ({ page }) => {
  await page.goto('/')
  await openNav(page)
  const nav = page.getByRole('navigation', { name: 'Main' })
  const movies = nav.getByRole('link', { name: 'Movies', exact: true })
  const shows = nav.getByRole('link', { name: 'TV shows', exact: true })
  await expect(movies).toBeHidden()
  await page.locator('#sn-head-library').click()
  // the tap opens the Library page, which closes the drawer on a tablet or phone;
  // wait for that so the menu is not reopened just before it closes
  await expect(page).toHaveURL(/\/library/)
  if (page.viewportSize()!.width <= 900) await expect(menuToggle(page)).toHaveAttribute('aria-expanded', 'false')
  await openNav(page)
  await expect(movies).toBeVisible()
  await shows.click()
  await expect(page).toHaveURL(/\/library\?kind=tv$/)
  await expect(shows).toHaveAttribute('aria-current', 'page')
  await expect(movies).not.toHaveAttribute('aria-current', 'page')
  // inside the Library the menu stays open
  await openNav(page)
  await movies.click()
  await expect(page).toHaveURL(/\/library\?kind=movie$/)
  await expect(movies).toHaveAttribute('aria-current', 'page')
})

test('an unknown address shows the not-found page', async ({ page }) => {
  await page.goto('/no-such-page')
  await expect(page.getByRole('heading', { level: 1 }).filter({ hasText: /exist|found/i })).toBeVisible()
})
