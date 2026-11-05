import { expect, test, type Page } from '@playwright/test'
import { openApp, openNav, signIn } from './helpers'

// A short window makes every page and dialog taller than the window, so the
// checks below hold whatever data the account has. The wide window is a
// desktop, the narrow one a phone.
const windows = [
  { name: 'desktop', width: 1280, height: 260 },
  { name: 'phone', width: 390, height: 260 }
]

// openPage signs in with a desktop-sized window, because the sidebar and the
// menu fold away in a narrow one, opens the page and then resizes the window.
async function openPage(
  page: Page,
  entry: 'Calendar' | 'Contacts' | 'Devices',
  size: { width: number; height: number }
) {
  await page.setViewportSize({ width: 1280, height: 800 })
  await signIn(page)
  await openApp(page, entry === 'Contacts' ? 'Contacts' : 'Calendar')
  if (entry === 'Devices') await openNav(page, 'Devices')
  await expect(page.locator('.gw-body')).toBeVisible()
  await page.setViewportSize(size)
}

// scrollTops lists the scroll position of the document and of every ancestor
// of the element.
function scrollTops(page: Page, selector: string): Promise<number[]> {
  return page.evaluate((sel) => {
    const tops = [document.scrollingElement?.scrollTop ?? 0]
    for (let el = document.querySelector(sel); el; el = el.parentElement) {
      tops.push(el.scrollTop)
    }
    return tops
  }, selector)
}

// expectScrollsToEnd turns the mouse wheel over the element and checks that
// something scrolled and that the last child of the element can be reached.
async function expectScrollsToEnd(page: Page, selector: string, height: number) {
  const target = page.locator(selector).first()
  await expect(target).toBeVisible()
  const box = await target.boundingBox()
  expect(box).not.toBeNull()
  const before = await scrollTops(page, selector)

  await page.mouse.move(box!.x + 20, Math.min(box!.y + 20, height - 20))
  for (let i = 0; i < 5; i++) {
    await page.mouse.wheel(0, 2000)
    await page.waitForTimeout(100)
  }

  const after = await scrollTops(page, selector)
  expect(after.some((top, i) => top > before[i])).toBe(true)
  const lastBottom = await page.evaluate((sel) => {
    const el = document.querySelector(sel)!
    // The last child that takes room, skipping hidden helper elements.
    const children = [...el.children].filter((c) => c.getBoundingClientRect().height > 0)
    return children[children.length - 1].getBoundingClientRect().bottom
  }, selector)
  expect(lastBottom).toBeLessThanOrEqual(height)
}

for (const size of windows) {
  test.describe(size.name, () => {
    test('the calendar page scrolls', async ({ page }) => {
      await openPage(page, 'Calendar', size)
      await expect(page.getByTestId('calendar-title')).not.toBeEmpty()
      await expectScrollsToEnd(page, '.gw-body', size.height)
    })

    test('the contacts page scrolls', async ({ page }) => {
      await openPage(page, 'Contacts', size)
      await expect(page.locator('h1')).toHaveText('All contacts')
      // Wait for the contacts: a page that is still loading is too short to scroll.
      await expect(
        page.getByTestId('no-contacts').or(page.getByTestId('contact-item').first())
      ).toBeVisible()
      await expectScrollsToEnd(page, '.gw-body', size.height)
    })

    test('the devices page scrolls', async ({ page }) => {
      await openPage(page, 'Devices', size)
      await expect(page.locator('h1')).toHaveText('Devices')
      await expectScrollsToEnd(page, '.gw-body', size.height)
    })

    test('a long dialog scrolls', async ({ page }) => {
      await openPage(page, 'Calendar', size)
      await page.getByTestId('event-add').click()
      await expect(page.getByRole('dialog')).toBeVisible()
      await expectScrollsToEnd(page, '.gw-modal', size.height)
    })
  })
}
