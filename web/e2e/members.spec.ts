import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { expectNoErrors, menuToggle, watchErrors } from './helpers'

// A member (made by the administrator through the API) must not be shown the
// administrator's pages and tabs, and the phone menu must be out of reach
// while it is closed.
const MEMBER = { username: 'e2e-member', password: 'e2e-member-pass-1', name: 'E2E Member', role: 'member' }

// The administrator's session comes from the setup project. A second run
// finds the account already there, which is fine.
async function ensureMember(request: APIRequestContext) {
  const res = await request.post('/api/users', { data: MEMBER })
  expect([200, 201, 400, 409]).toContain(res.status())
}

async function signInAsMember(page: Page) {
  await page.context().clearCookies()
  await page.goto('/')
  await page.locator('input[autocomplete="username"]').fill(MEMBER.username)
  await page.locator('input[autocomplete="current-password"]').fill(MEMBER.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  // On a tablet the menu is closed (hidden) until its button is pressed; on a phone the bottom bar is there.
  await expect(page.locator(':is(nav#main-menu, .menu-button, .bottom-nav):visible').first()).toBeVisible()
}

test('a member sees no administrator tabs or pages', async ({ page, request }) => {
  const errors = watchErrors(page)
  await ensureMember(request)
  await signInAsMember(page)

  // Settings pages for administrators send the member to their profile.
  await page.goto('/settings/accounts')
  await expect(page).toHaveURL(/\/settings\/profile/)
  await page.goto('/settings/system')
  await expect(page).toHaveURL(/\/settings\/profile/)

  // A link to the blocklist tab opens the queue instead of an empty page.
  await page.goto('/queue?tab=blocklist')
  await expect(page.getByRole('button', { name: /^Blocklist/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Download clients/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Queue/ })).toHaveAttribute('aria-pressed', 'true')
  await expectNoErrors(errors)
})

test('on a phone the closed menu is out of reach and Escape closes the open one', async ({ page, request }) => {
  const errors = watchErrors(page)
  await ensureMember(request)
  await page.setViewportSize({ width: 375, height: 800 })
  await signInAsMember(page)
  await page.goto('/library')

  const nav = page.locator('nav#main-menu')
  await expect(nav).toBeHidden()
  const button = menuToggle(page)
  await expect(button).toHaveAttribute('aria-expanded', 'false')

  await button.click()
  await expect(nav).toBeVisible()
  await expect(nav.getByRole('link').first()).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(nav).toBeHidden()
  await expect(button).toBeFocused()
  await expectNoErrors(errors)
})
