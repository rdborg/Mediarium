import { expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

export const AUTH_FILE = 'e2e/.auth/admin.json'
export const CREDS_FILE = 'e2e/.auth/creds.json'

// The administrator the setup test created.
export function creds(): { username: string; password: string } {
  return JSON.parse(readFileSync(CREDS_FILE, 'utf8'))
}

// Collects uncaught errors in the page, so a test can fail on them.
export function watchErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  return errors
}

export async function expectNoErrors(errors: string[]) {
  expect(errors, errors.join('\n')).toEqual([])
}

// The layout for tablets and phones starts at 900px: the main menu is a drawer
// that starts closed (the theme button and the links to the guide live in it)
// and the guide's landing page lists its topics as cards instead of a side list.
const narrow = (page: Page) => (page.viewportSize()?.width ?? 1280) <= 900

// The button that opens and closes the menu drawer: the header's menu button on a
// tablet, More in the bottom bar on a phone (the header button is hidden there).
export function menuToggle(page: Page) {
  if ((page.viewportSize()?.width ?? 1280) <= 700) return page.locator('.bottom-nav button[aria-controls="main-menu"]')
  return page.locator('.menu-button')
}

// Opens the menu drawer; on a wide screen the menu is already there.
export async function openNav(page: Page) {
  if (!narrow(page)) return
  const button = menuToggle(page)
  await expect(button).toBeVisible()
  if ((await button.getAttribute('aria-expanded')) === 'false') await button.click()
}
