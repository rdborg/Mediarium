import { expect, test } from '@playwright/test'
import { expectNoErrors, watchErrors } from './helpers'

// Demo mode fills the screens with made-up titles; nothing is saved.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
})

test('the library, a title page and the queue show the sample titles', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/library?kind=movie')
  await expect(page.getByText('Saltwind: Part Two').first()).toBeVisible()
  await page.getByText('The Long Equation').first().click()
  await expect(page.getByRole('heading', { name: /The Long Equation/ }).first()).toBeVisible()
  await page.goto('/queue')
  await expect(page.getByText('Northbound').first()).toBeVisible()
  await expectNoErrors(errors)
})

test('discover shows only made-up titles', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/discover?kind=movie')
  await expect(page.getByText('Small Weather').first()).toBeVisible()
  await expectNoErrors(errors)
})

test('a cast member opens the person page with their work', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/library?kind=movie')
  await page.getByText('The Long Equation').first().click()
  await page.getByRole('link', { name: /Ines Marlow/ }).first().click()
  await expect(page).toHaveURL(/\/person\/9001$/)
  await expect(page.getByRole('heading', { name: 'Ines Marlow' }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Movies' })).toBeVisible()
  await page.getByRole('button', { name: 'Directing' }).click()
  await expect(page).toHaveURL(/role=directing/)
  await expectNoErrors(errors)
})
