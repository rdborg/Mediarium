import { expect, test } from '@playwright/test'
import { creds, expectNoErrors, openNav, watchErrors } from './helpers'

// Starts signed out.
test.use({ storageState: { cookies: [], origins: [] } })

test('a wrong password is refused, the right one signs in', async ({ page }) => {
  const errors = watchErrors(page)
  const { username, password } = creds()
  await page.goto('/')
  await page.locator('input[autocomplete="username"]').fill(username)
  await page.locator('input[autocomplete="current-password"]').fill(password + 'x')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0)
  await expect(page.locator('.error-text, [role="alert"]').first()).toBeVisible()

  await page.locator('input[autocomplete="current-password"]').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.locator('input[autocomplete="current-password"]')).toHaveCount(0)
  await openNav(page)
  await expect(page.getByRole('navigation', { name: 'Main', exact: true })).toBeVisible()
  await expectNoErrors(errors)
})
