import { expect, test, type Page } from '@playwright/test'

// The demo users that oCIS creates with IDM_CREATE_DEMO_USERS.
const user = { name: 'einstein', password: 'relativity' }

async function signIn(page: Page) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(user.name)
  await page.locator('#oc-login-password').fill(user.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
}

test('the calendar page shows month, week and day views and hides a calendar', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)
  await page.locator('#_appSwitcherButton').click()
  await page.getByRole('link', { name: 'Calendar and Contacts' }).click()
  await expect(page).toHaveURL(/\/groupware\/calendar$/)

  // The month view is the first one.
  await expect(page.locator('.fc-dayGridMonth-view')).toBeVisible()
  await expect(page.locator('.fc-toolbar-title')).not.toBeEmpty()

  await page.getByRole('button', { name: 'Week', exact: true }).click()
  await expect(page.locator('.fc-timeGridWeek-view')).toBeVisible()
  await page.getByRole('button', { name: 'Day', exact: true }).click()
  await expect(page.locator('.fc-timeGridDay-view')).toBeVisible()
  await page.getByRole('button', { name: 'Month', exact: true }).click()
  await expect(page.locator('.fc-dayGridMonth-view')).toBeVisible()

  // The default calendar is listed with its colour, and can be switched off.
  const item = page.getByTestId('calendar-item').first()
  await expect(item).toContainText('Personal')
  const toggle = item.getByTestId('calendar-toggle')
  await expect(toggle).toBeChecked()
  await toggle.uncheck()

  // The choice stays when the page is opened again. A reload would go to the
  // service itself, since the application's paths are not served by Web.
  const sidebar = page.locator('#web-nav-sidebar')
  await sidebar.getByRole('link', { name: 'Contacts' }).click()
  await expect(page).toHaveURL(/\/groupware\/contacts$/)
  await sidebar.getByRole('link', { name: 'Calendar' }).click()
  await expect(page.getByTestId('calendar-item').first().getByTestId('calendar-toggle')).not.toBeChecked()
  await page.getByTestId('calendar-item').first().getByTestId('calendar-toggle').check()

  expect(pageErrors).toEqual([])
})
