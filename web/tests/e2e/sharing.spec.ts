import { expect, test, type Browser, type Page } from '@playwright/test'
import { openApp, openNav, signIn, user, type TestUser } from './helpers'

// Demo users that oCIS creates with IDM_CREATE_DEMO_USERS. Both are members
// of the group physics-lovers.
const einstein = user
const marie = { name: 'marie', password: 'radioactivity' }

// A user gets a browser context of their own, so the two stay signed in side
// by side.
async function signInAs(browser: Browser, who: TestUser): Promise<Page> {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    baseURL: process.env.STACK_URL ?? 'https://ocis.localhost'
  })
  const page = await context.newPage()
  await signIn(page, who)
  await openApp(page, 'Calendar and Contacts')
  return page
}

// The application's paths are not served by Web, so a reload would leave
// it. Going to the other page and back reads the lists again.
async function reload(page: Page, name: 'Calendar' | 'Contacts') {
  const other = name === 'Calendar' ? 'Contacts' : 'Calendar'
  await openNav(page, other)
  await openNav(page, name)
}

// share searches the directory and adds the first match with a role.
async function share(page: Page, search: string, label: string, role: 'read' | 'write') {
  await page.getByTestId('share-search').fill(search)
  await page.getByTestId('share-result').filter({ hasText: label }).first().click()
  await page.getByTestId('share-new-role').selectOption(role)
  await page.getByTestId('share-add').click()
  await expect(page.getByTestId('share-chosen')).toHaveCount(0)
}

test('a calendar is shared with a user and with a group, and the role and revoking take effect', async ({
  browser
}) => {
  const owner = await signInAs(browser, einstein)
  const guest = await signInAs(browser, marie)
  const items = (p: Page) => p.getByTestId('calendar-item').filter({ hasText: 'Shared notes' })

  await expect(owner).toHaveURL(/\/groupware\/calendar$/)
  await owner.getByTestId('calendar-add').click()
  await owner.getByTestId('calendar-name').fill('Shared notes')
  await owner.getByTestId('dialog-confirm').click()
  await expect(items(owner)).toHaveCount(1)
  // Nobody else sees it yet.
  await reload(guest, 'Calendar')
  await expect(items(guest)).toHaveCount(0)

  // Share with a user, who may only read.
  await items(owner).getByTestId('calendar-share').click()
  await expect(owner.getByTestId('share-none')).toBeVisible()
  await owner.getByTestId('share-search').fill('ma')
  await expect(owner.getByTestId('share-hint')).toBeVisible()
  await share(owner, 'marie', 'Marie', 'read')
  const row = owner.getByTestId('share-item').filter({ hasText: 'Marie' })
  await expect(row).toHaveCount(1)
  await expect(row.getByTestId('share-role')).toHaveValue('read')
  // The person is no longer offered.
  await owner.getByTestId('share-search').fill('marie')
  await expect(owner.getByTestId('share-no-match')).toBeVisible()
  await owner.getByTestId('dialog-cancel').click()

  await reload(guest, 'Calendar')
  await expect(items(guest)).toHaveCount(1)
  await expect(items(guest)).toContainText('read only')
  await expect(items(guest).getByTestId('calendar-share')).toHaveCount(0)
  await expect(items(guest).getByTestId('calendar-edit')).toHaveCount(0)
  await expect(items(guest).getByTestId('calendar-delete')).toHaveCount(0)

  // The role changes to edit.
  await items(owner).getByTestId('calendar-share').click()
  await owner.getByTestId('share-item').filter({ hasText: 'Marie' }).getByTestId('share-role').selectOption('write')
  await owner.getByTestId('dialog-cancel').click()
  await reload(guest, 'Calendar')
  await expect(items(guest)).toContainText('can edit')

  // Revoking ends the access.
  await items(owner).getByTestId('calendar-share').click()
  await owner.getByTestId('share-item').filter({ hasText: 'Marie' }).getByTestId('share-revoke').click()
  await expect(owner.getByTestId('share-none')).toBeVisible()
  await reload(guest, 'Calendar')
  await expect(items(guest)).toHaveCount(0)

  // Share with a group the guest belongs to.
  await share(owner, 'physics', 'physics-lovers', 'read')
  const group = owner.getByTestId('share-item').filter({ hasText: 'physics-lovers' })
  await expect(group).toContainText('Group')
  await owner.getByTestId('dialog-cancel').click()
  await reload(guest, 'Calendar')
  await expect(items(guest)).toContainText('read only')

  await items(owner).getByTestId('calendar-share').click()
  await group.getByTestId('share-revoke').click()
  await expect(owner.getByTestId('share-none')).toBeVisible()
  await owner.getByTestId('dialog-cancel').click()
  await reload(guest, 'Calendar')
  await expect(items(guest)).toHaveCount(0)

  await items(owner).getByTestId('calendar-delete').click()
  await owner.getByTestId('dialog-confirm').click()
  await expect(items(owner)).toHaveCount(0)
  await owner.context().close()
  await guest.context().close()
})

test('an address book is shared with a user and shows as read only and then editable', async ({
  browser
}) => {
  const owner = await signInAs(browser, einstein)
  const guest = await signInAs(browser, marie)
  const folders = (p: Page) => p.getByTestId('folder-item').filter({ hasText: 'Lab contacts' })
  for (const p of [owner, guest]) {
    await openNav(p, 'Contacts')
    await expect(p.locator('h1')).toHaveText('Contacts')
  }

  await owner.getByTestId('book-new').click()
  await owner.getByTestId('book-new-input').fill('Lab contacts')
  await owner.getByTestId('book-new-save').click()
  await expect(folders(owner)).toHaveCount(1)
  await folders(owner).click()
  await owner.getByTestId('book-share').click()
  await share(owner, 'marie', 'Marie', 'read')
  await expect(owner.getByTestId('share-item')).toHaveCount(1)
  await owner.getByTestId('dialog-cancel').click()

  await reload(guest, 'Contacts')
  await expect(folders(guest)).toContainText('read only')
  await folders(guest).click()
  await expect(guest.getByTestId('book-share')).toHaveCount(0)
  await expect(guest.getByTestId('book-rename')).toHaveCount(0)
  await expect(guest.getByTestId('contact-new')).toBeDisabled()

  await owner.getByTestId('book-share').click()
  await owner.getByTestId('share-role').selectOption('write')
  await owner.getByTestId('dialog-cancel').click()
  await reload(guest, 'Contacts')
  await expect(folders(guest)).toContainText('can edit')

  await owner.getByTestId('book-share').click()
  await owner.getByTestId('share-revoke').click()
  await expect(owner.getByTestId('share-none')).toBeVisible()
  await owner.getByTestId('dialog-cancel').click()
  await reload(guest, 'Contacts')
  await expect(folders(guest)).toHaveCount(0)

  await owner.getByTestId('book-delete').click()
  await owner.getByTestId('book-delete-confirm').click()
  await expect(folders(owner)).toHaveCount(0)
  await owner.context().close()
  await guest.context().close()
})
