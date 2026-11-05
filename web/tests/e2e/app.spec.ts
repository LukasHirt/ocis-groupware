import { expect, test } from '@playwright/test'
import { menuEntry, openApp, openNav, signIn } from './helpers'

test('oCIS Web lists Calendar and Contacts as two applications with their own navigation', async ({
  page
}) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)

  // One application adds two entries to the application menu.
  const switcher = page.locator('#_appSwitcherButton')
  await switcher.click()
  await expect(menuEntry(page, 'Calendar')).toBeVisible()
  await expect(menuEntry(page, 'Contacts')).toBeVisible()
  await page.keyboard.press('Escape')

  // Calendar opens on its main page, with its own two navigation items.
  await openApp(page, 'Calendar')
  await expect(page).toHaveURL(/\/organizer\/calendar\/events$/)
  await expect(page.locator('h1')).toHaveText('Calendar')
  const nav = page.locator('#web-nav-sidebar')
  await expect(nav.getByRole('link')).toHaveText(['Calendar', 'Devices'])

  // The page reads the calendars through the Graph paths. The default
  // calendar exists after the first sign-in.
  await expect(page.getByTestId('calendar-item').first()).toContainText('Personal')

  // The entry of the open area is the active one.
  await switcher.click()
  await expect(menuEntry(page, 'Calendar')).toHaveClass(/router-link-active/)
  await expect(menuEntry(page, 'Contacts')).not.toHaveClass(/router-link-active/)
  await page.keyboard.press('Escape')

  // Devices is part of the area.
  await openNav(page, 'Devices')
  await expect(page).toHaveURL(/\/organizer\/calendar\/devices$/)
  await expect(page.locator('h1')).toHaveText('Devices')

  // Contacts has its own navigation, with the same Devices page.
  await openApp(page, 'Contacts')
  await expect(page).toHaveURL(/\/organizer\/contacts\/list$/)
  await expect(page.locator('h1')).toHaveText('Contacts')
  await expect(nav.getByRole('link')).toHaveText(['Contacts', 'Devices'])
  await expect(page.getByTestId('folder-item').first()).toContainText('Contacts')
  await openNav(page, 'Devices')
  await expect(page).toHaveURL(/\/organizer\/contacts\/devices$/)
  await expect(page.locator('h1')).toHaveText('Devices')

  expect(pageErrors).toEqual([])
})

test('reloading an application page shows the application again', async ({ page }) => {
  await signIn(page)
  await openApp(page, 'Contacts')
  await expect(page).toHaveURL(/\/organizer\/contacts\/list$/)

  // The server answers a reload or a bookmark; its path must belong to
  // oCIS Web, not to the service's own API.
  await page.reload()
  await expect(page.locator('#web-content')).toBeVisible()
  await expect(page).toHaveURL(/\/organizer\/contacts\/list$/)
  await expect(page.locator('h1')).toHaveText('Contacts')
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
