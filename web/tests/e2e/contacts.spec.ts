import { expect, test, type Page } from '@playwright/test'

const user = { name: 'einstein', password: 'relativity' }

async function signIn(page: Page) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(user.name)
  await page.locator('#oc-login-password').fill(user.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
}

// openContacts goes to the Contacts page through the application menu, since
// a reload of the page would end the session.
async function openContacts(page: Page) {
  await page.locator('#_appSwitcherButton').click()
  await page.getByRole('link', { name: 'Calendar and Contacts' }).click()
  await page.locator('#web-nav-sidebar').getByRole('link', { name: 'Contacts' }).click()
  await expect(page.locator('h1')).toHaveText('Contacts')
}

test('the contacts page lists the address books and offers search and sorting', async ({
  page
}) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)
  await openContacts(page)
  await expect(page.getByTestId('folder-item').first()).toContainText('Contacts')
  await expect(page.getByTestId('contact-search')).toBeVisible()
  await expect(page.getByTestId('contact-sort')).toBeVisible()

  // A new account has no contacts.
  await expect(page.getByTestId('no-contacts')).toBeVisible()

  expect(pageErrors).toEqual([])
})

// A valid 1x1 PNG image.
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
  'base64'
)

test('a contact can be created, found, edited, given a photo and deleted', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)
  await openContacts(page)

  await page.getByTestId('contact-new').click()
  await page.getByTestId('field-givenName').fill('Ada')
  await page.getByTestId('field-surname').fill('Lovelace')
  await page.getByTestId('emails-0').fill('ada@example.org')
  await page.getByTestId('homeAddress-city').fill('London')
  await page.getByTestId('form-save').click()

  const detail = page.getByTestId('contact-detail')
  await expect(detail).toContainText('Ada Lovelace')
  await expect(detail).toContainText('ada@example.org')
  await expect(detail).toContainText('London')

  await page.getByTestId('contact-search').fill('lovelace')
  await expect(page.getByTestId('contact-item')).toHaveCount(1)

  await page.getByTestId('contact-edit').click()
  await page.getByTestId('field-jobTitle').fill('Mathematician')
  await page.getByTestId('form-save').click()
  await expect(detail).toContainText('Mathematician')

  await expect(page.getByTestId('photo-initials')).toHaveText('AL')
  await page
    .getByTestId('photo-input')
    .setInputFiles({ name: 'ada.png', mimeType: 'image/png', buffer: png })
  await expect(page.getByTestId('photo-image')).toBeVisible()
  await page.getByTestId('photo-remove').click()
  await expect(page.getByTestId('photo-initials')).toBeVisible()

  await page.getByTestId('contact-delete').click()
  await page.getByTestId('contact-delete-confirm').click()
  await expect(page.getByTestId('no-contacts')).toBeVisible()

  expect(pageErrors).toEqual([])
})
