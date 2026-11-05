import { expect, test, type Page } from '@playwright/test'
import { openApp, openNav, signIn, user } from './helpers'

async function openAccount(page: Page) {
  await signIn(page)
  await openApp(page, 'Calendar')
  await openNav(page, 'Devices')
}

// A DAV client's first request: the calendars of the user, with the app
// password over HTTP Basic.
async function davStatus(page: Page, password: string): Promise<number> {
  const response = await page.request.fetch(`/dav/calendars/${user.name}/`, {
    method: 'PROPFIND',
    headers: {
      Authorization: `Basic ${Buffer.from(`${user.name}:${password}`).toString('base64')}`,
      Depth: '0'
    }
  })
  return response.status()
}

test('an app password is created, works for a DAV client and is revoked', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))
  const me = page.waitForResponse((r) => r.url().endsWith('/groupware/v1/me') && r.request().method() === 'GET')
  await openAccount(page)
  await expect(page.locator('h1')).toHaveText('Devices')
  const { app_passwords_valid_until: validUntil } = await (await me).json()

  // The DAV address and the username are shown.
  const dav = page.getByTestId('dav-account')
  await expect(dav.getByTestId('dav-username-value')).toHaveText(user.name)
  await expect(dav.getByTestId('dav-url-value')).toHaveText(/\/dav\/$/)
  await expect(dav.getByTestId('dav-calendar-value')).toHaveText(new RegExp(`/dav/calendars/${user.name}/$`))
  await expect(page.getByTestId('dav-setup')).toContainText('DAVx5')
  // The end of the lease is shown when the service limits it, and only then.
  await expect(dav.getByTestId('dav-valid-until')).toHaveCount(validUntil === null ? 0 : 1)

  // Create one with a shorter lifetime. The password is shown once.
  const name = `Browser test ${Date.now()}`
  await page.getByTestId('app-password-add').click()
  await page.locator('input[data-testid="app-password-name"]').fill(name)
  await page.locator('.v-select[data-testid="app-password-expiry"]').click()
  await page.getByRole('option', { name: '30 days' }).click()
  await expect(page.locator('.v-select[data-testid="app-password-expiry"]')).toContainText('30 days')
  await page.getByTestId('dialog-confirm').click()
  const secret = (await page.locator('input[data-testid="app-password-secret"]').inputValue()).trim()
  expect(secret).toMatch(/^[a-z2-7]{32}$/)
  await page.getByTestId('app-password-done').click()
  await expect(page.locator('[data-testid="app-password-secret"]')).toHaveCount(0)

  const item = page.getByTestId('app-password-item').filter({ hasText: name })
  await expect(item).toHaveCount(1)
  await expect(item).toContainText('Never used')
  await expect(page.locator('body')).not.toContainText(secret)

  // A DAV client signs in with it.
  expect(await davStatus(page, secret)).toBe(207)

  // Revoking it refuses the client from then on.
  await item.getByTestId('app-password-revoke').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByTestId('app-password-item').filter({ hasText: name })).toHaveCount(0)
  expect(await davStatus(page, secret)).toBe(401)

  expect(pageErrors).toEqual([])
})

test('the devices page lines up its heading and content and fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await openAccount(page)
  await expect(page.getByTestId('dav-account')).toBeVisible()

  // On a desktop the content starts where the heading does.
  const left = (selector: string) =>
    page.locator(selector).first().evaluate((el) => el.getBoundingClientRect().left)
  expect(Math.abs((await left('h1')) - (await left('.gw-devices')))).toBeLessThanOrEqual(2)

  // On a phone nothing touches the edge of the window and an address is only
  // broken after a slash.
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByTestId('app-password-add')).toBeVisible()
  const edge = await page
    .getByTestId('app-password-add')
    .evaluate((el) => window.innerWidth - el.getBoundingClientRect().right)
  expect(edge).toBeGreaterThanOrEqual(8)
  for (const id of ['dav-url-value', 'dav-calendar-value', 'dav-addressbook-value']) {
    // The layout of the narrow window settles a moment after the resize.
    const breaks = () => page.getByTestId(id).evaluate((el) => {
      const range = document.createRange()
      const bad: string[] = []
      let top: number | undefined
      let before = ''
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
      for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
        for (let i = 0; i < node.length; i++) {
          range.setStart(node, i)
          range.setEnd(node, i + 1)
          const rect = range.getBoundingClientRect()
          if (top !== undefined && rect.top > top + 2 && before !== '/') {
            bad.push(`${before}|${node.data[i]}`)
          }
          top = rect.top
          before = node.data[i]
        }
      }
      return bad
    })
    await expect.poll(breaks, { message: id }).toEqual([])
  }
})
