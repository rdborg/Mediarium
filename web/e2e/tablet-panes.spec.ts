import { expect, test } from '@playwright/test'
import { expectNoErrors, watchErrors } from './helpers'

// On a tablet the Library keeps its list on the left and shows the open title on
// the right. Run with the app project; the sizes are set here, an iPad upright
// and on its side.
for (const size of [
  { name: 'portrait', width: 820, height: 1180 },
  { name: 'landscape', width: 1180, height: 820 },
]) {
  test.describe(`tablet ${size.name} (${size.width}x${size.height})`, () => {
    test.use({ viewport: { width: size.width, height: size.height } })

    test.beforeEach(async ({ page }) => {
      await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
    })

    test('a title opens beside the list, Esc gives the list back', async ({ page }) => {
      const errors = watchErrors(page)
      await page.goto('/library?kind=tv')
      await expect(page.locator('.twopane')).toHaveCount(0)
      await page.locator('a.pcard-link').first().click()
      await expect(page.locator('.twopane-list')).toBeVisible()
      await expect(page.locator('.twopane-detail h1').first()).toBeVisible()
      await expect(page).toHaveURL(/\/series\/\d+\?.*lib=1/)
      await expect(page.locator('.pcard-current')).toHaveCount(1)

      // picking another one only swaps the right side
      const before = await page.locator('.twopane-detail h1').first().innerText()
      await page.locator('a.pcard-link:not([aria-current])').first().click()
      await expect(page.locator('.twopane-detail h1').first()).not.toHaveText(before)

      // nothing in the page runs off the screen
      const content = page.locator('main.content')
      expect(await content.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(0)

      await page.keyboard.press('Escape')
      await expect(page.locator('.twopane')).toHaveCount(0)
      await expect(page).toHaveURL(/\/library\?kind=tv/)
      await expectNoErrors(errors)
    })

    test('refresh keeps the two panes', async ({ page }) => {
      await page.goto('/library?kind=tv')
      await page.locator('a.pcard-link').first().click()
      await expect(page.locator('.twopane-detail h1').first()).toBeVisible()
      await page.reload()
      await expect(page.locator('.twopane-list')).toBeVisible()
      await expect(page.locator('.twopane-detail h1').first()).toBeVisible()
    })
  })
}
