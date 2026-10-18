import { expect, test } from '@playwright/test'
import { openNav } from './helpers'

// Nothing pushes the page wider than the screen, at any of the three sizes
// (run with --project app, app-tablet or app-phone). Russian has the longest
// button labels, so it is the one that shows a button running off the card.
const PAGES = ['/series/101', '/settings/media', '/settings/system', '/settings/services', '/queue', '/library?kind=tv']

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

// The header buttons are big enough for a finger on a tablet or phone.
test('the header buttons are at least 40px on a touch screen', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'phone only')
  await page.goto('/')
  for (const button of [page.locator('.menu-button'), page.locator('button.avatar')]) {
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
