import { expect, test, type Page } from '@playwright/test'

const user = { name: 'einstein', password: 'relativity' }

async function signIn(page: Page) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(user.name)
  await page.locator('#oc-login-password').fill(user.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
}

test('the contacts page lists the address books and offers search and sorting', async ({
  page
}) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)
  await page.locator('#_appSwitcherButton').click()
  await page.getByRole('link', { name: 'Calendar and Contacts' }).click()
  await page.locator('#web-nav-sidebar').getByRole('link', { name: 'Contacts' }).click()

  await expect(page.locator('h1')).toHaveText('Contacts')
  await expect(page.getByTestId('folder-item').first()).toContainText('Contacts')
  await expect(page.getByTestId('contact-search')).toBeVisible()
  await expect(page.getByTestId('contact-sort')).toBeVisible()

  // A new account has no contacts.
  await expect(page.getByTestId('no-contacts')).toBeVisible()

  expect(pageErrors).toEqual([])
})
