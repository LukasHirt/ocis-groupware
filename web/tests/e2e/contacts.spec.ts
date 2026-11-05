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

test('a contact can be created, found, edited, given a photo and a group, and deleted', async ({ page }) => {
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

  // Groups are categories: adding one lists it in the sidebar, where it can
  // be chosen, renamed and removed again.
  await page.getByTestId('membership-input').fill('Pioneers')
  await page.getByTestId('membership-add').click()
  await page.getByTestId('group-item').filter({ hasText: 'Pioneers' }).click()
  await expect(page.getByTestId('contact-item')).toHaveCount(1)
  await page.getByTestId('group-rename').click()
  await page.getByTestId('group-rename-input').fill('Founders')
  await page.getByTestId('group-rename-save').click()
  await expect(page.getByTestId('group-item').filter({ hasText: 'Founders' })).toBeVisible()
  await expect(page.getByTestId('membership-name')).toHaveText('Founders')
  await page.getByTestId('group-delete').click()
  await page.getByTestId('group-delete-confirm').click()
  await expect(page.getByTestId('group-item')).toHaveCount(0)
  await expect(page.getByTestId('membership-name')).toHaveCount(0)

  await page.getByTestId('contact-delete').click()
  await page.getByTestId('contact-delete-confirm').click()
  await expect(page.getByTestId('no-contacts')).toBeVisible()

  expect(pageErrors).toEqual([])
})

test('an address book can be created, renamed, filled from a vCard file, downloaded and deleted', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(page)
  await openContacts(page)

  await page.getByTestId('book-new').click()
  await page.getByTestId('book-new-input').fill('Clients')
  await page.getByTestId('book-new-save').click()
  const book = (name: string) => page.getByTestId('folder-item').filter({ hasText: name })
  await expect(book('Clients')).toBeVisible()

  // The new address book is chosen, and the owner may rename it.
  await page.getByTestId('book-rename').click()
  await page.getByTestId('book-rename-input').fill('Customers')
  await page.getByTestId('book-rename-save').click()
  await expect(book('Customers')).toBeVisible()
  await expect(book('Clients')).toHaveCount(0)

  // A vCard file with two good cards and a broken one is imported, and the
  // report names the broken one. The address book downloads as a .vcf file.
  const vcf = [
    'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:ada\r\nFN:Ada Lovelace\r\nN:Lovelace;Ada;;;\r\nEND:VCARD\r\n',
    'BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Broken\r\n',
    'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:grace\r\nFN:Grace Hopper\r\nN:Hopper;Grace;;;\r\nEND:VCARD\r\n'
  ]
  await page.getByTestId('book-import-input').setInputFiles({
    name: 'cards.vcf',
    mimeType: 'text/vcard',
    buffer: Buffer.from(vcf[0] + vcf[2])
  })
  await expect(page.getByTestId('import-report')).toContainText('2 created')
  await expect(page.getByTestId('contact-item')).toHaveCount(2)
  await page.getByTestId('import-report-close').click()
  await page.getByTestId('book-import-input').setInputFiles({
    name: 'broken.vcf',
    mimeType: 'text/vcard',
    buffer: Buffer.from(vcf[1])
  })
  await expect(page.getByTestId('import-report')).toContainText('1 failed')
  const download = page.waitForEvent('download')
  await page.getByTestId('book-download').click()
  expect((await download).suggestedFilename()).toBe('Customers.vcf')

  // A contact can be put into it.
  await page.getByTestId('contact-new').click()
  await page.getByTestId('field-givenName').fill('Grace')
  await page.getByTestId('form-save').click()
  await expect(page.getByTestId('contact-item')).toHaveCount(3)

  await page.getByTestId('book-delete').click()
  await page.getByTestId('book-delete-confirm').click()
  await expect(book('Customers')).toHaveCount(0)

  expect(pageErrors).toEqual([])
})
