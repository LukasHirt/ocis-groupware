import { expect, test, type Page } from '@playwright/test'

// The demo users that oCIS creates with IDM_CREATE_DEMO_USERS.
const user = { name: 'einstein', password: 'relativity' }

async function signIn(page: Page) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(user.name)
  await page.locator('#oc-login-password').fill(user.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
  await page.locator('#_appSwitcherButton').click()
  await page.getByRole('link', { name: 'Calendar and Contacts' }).click()
  await page.locator('#web-nav-sidebar').getByRole('link', { name: 'Account' }).click()
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
  await signIn(page)
  await expect(page.locator('h1')).toHaveText('Account')
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
  await page.getByTestId('app-password-name').fill(name)
  await page.getByTestId('app-password-expiry').selectOption('30')
  await page.getByTestId('dialog-confirm').click()
  const secret = (await page.getByTestId('app-password-secret').innerText()).trim()
  expect(secret).toMatch(/^[a-z2-7]{32}$/)
  await page.getByTestId('app-password-done').click()
  await expect(page.getByTestId('app-password-secret')).toHaveCount(0)

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
