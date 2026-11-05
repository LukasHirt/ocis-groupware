import { expect, test, type Page } from '@playwright/test'

// The demo users that oCIS creates with IDM_CREATE_DEMO_USERS.
const user = { name: 'einstein', password: 'relativity' }

// signIn opens oCIS Web, which sends the browser to the identity provider,
// fills the sign-in form and waits for Web to come back signed in.
async function signIn(page: Page) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(user.name)
  await page.locator('#oc-login-password').fill(user.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
}

test('oCIS Web loads the application', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)

  // The application menu lists the app that was loaded from /web/.
  await page.locator('#_appSwitcherButton').click()
  const menuItem = page.getByRole('link', { name: 'Calendar and Contacts' })
  await expect(menuItem).toBeVisible()
  await menuItem.click()

  // The root route redirects to the calendar page.
  await expect(page).toHaveURL(/\/groupware\/calendar$/)
  await expect(page.locator('h1')).toHaveText('Calendar')

  // The page reads the calendars through the Graph paths. The default
  // calendar exists after the first sign-in.
  await expect(page.getByTestId('calendar-item').first()).toContainText('Personal')

  // The sidebar carries both entries, and the second one routes within the app.
  const sidebar = page.locator('#web-nav-sidebar')
  await expect(sidebar.getByRole('link', { name: 'Calendar' })).toBeVisible()
  await sidebar.getByRole('link', { name: 'Contacts' }).click()
  await expect(page).toHaveURL(/\/groupware\/contacts$/)
  await expect(page.locator('h1')).toHaveText('Contacts')
  await expect(page.getByTestId('folder-item').first()).toContainText('Contacts')

  expect(pageErrors).toEqual([])
})
