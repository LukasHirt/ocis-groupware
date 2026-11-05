import { expect, test } from '@playwright/test'
import { openNav, signIn } from './helpers'

test('oCIS Web loads the application', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)

  // The application menu lists the app that was loaded from /web/.
  await page.locator('#_appSwitcherButton').click()
  await expect(page.getByRole('link', { name: 'Calendar and Contacts', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'Calendar and Contacts', exact: true }).click()

  // The root route redirects to the calendar page.
  await expect(page).toHaveURL(/\/groupware\/calendar$/)
  await expect(page.locator('h1')).toHaveText('Calendar')

  // The page reads the calendars through the Graph paths. The default
  // calendar exists after the first sign-in.
  await expect(page.getByTestId('calendar-item').first()).toContainText('Personal')

  // The sidebar carries both entries, and the second one routes within the app.
  const sidebar = page.locator('#web-nav-sidebar')
  await expect(sidebar.getByRole('link', { name: 'Calendar' })).toBeVisible()
  await openNav(page, 'Contacts')
  await expect(page).toHaveURL(/\/groupware\/contacts$/)
  await expect(page.locator('h1')).toHaveText('Contacts')
  await expect(page.getByTestId('folder-item').first()).toContainText('Contacts')

  expect(pageErrors).toEqual([])
})

test('opening oCIS Web renews the sign-in lease without opening the app', async ({ page }) => {
  // The page records what the service answers to the call that oCIS Web
  // makes on its own after sign-in. The Files app is the landing page, so
  // neither Calendar nor Contacts is ever opened.
  const confirmed = page.waitForResponse(
    (r) => r.url().endsWith('/groupware/v1/me') && r.request().method() === 'GET'
  )
  await signIn(page)
  const res = await confirmed
  expect(res.status()).toBe(200)
  const body = await res.json()
  // With the lease check on, the answer carries the end of the lease, which
  // lies in the future after the renewal. It is null when the check is off.
  if (body.app_passwords_valid_until !== null) {
    expect(new Date(body.app_passwords_valid_until).getTime()).toBeGreaterThan(Date.now())
  }
})
