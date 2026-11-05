import { expect, type Locator, type Page } from '@playwright/test'

export interface TestUser {
  name: string
  password: string
}

// The demo users that oCIS creates with IDM_CREATE_DEMO_USERS.
export const user: TestUser = { name: 'einstein', password: 'relativity' }

// signIn opens oCIS Web, which sends the browser to the identity provider,
// fills the sign-in form and waits for Web to come back signed in.
export async function signIn(page: Page, who: TestUser = user) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(who.name)
  await page.locator('#oc-login-password').fill(who.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
}

// menuEntry is the link of an application in the application menu. The
// left navigation can have an item of the same name, so the menu's own
// attribute picks it out.
export function menuEntry(page: Page, label: string) {
  const exact = new RegExp(`^\\s*${label}\\s*$`)
  return page.locator('[data-test-id$=".app-menu-item"]').filter({ hasText: exact })
}

// openApp opens an application from the application menu. The application's
// paths are not served by Web, so a reload would leave it; tests move
// between pages through the menu and the navigation instead.
export async function openApp(page: Page, label: string) {
  await page.locator('#_appSwitcherButton').click()
  await menuEntry(page, label).click()
}

// openNav follows an item of the navigation inside the open application.
export async function openNav(page: Page, name: string) {
  await page.locator('#web-nav-sidebar').getByRole('link', { name, exact: true }).click()
}

// selectBox is a design-system select. Its wrapper repeats the attributes of
// the select, the test id among them, so the select itself is picked out by
// the class of the library it is made with.
export function selectBox(scope: Page | Locator, testId: string) {
  return scope.locator(`.v-select[data-testid="${testId}"]`)
}

// pickOption chooses an entry of a design-system select by its visible text.
// The scope narrows the search when the page holds several selects with the
// same id, such as one per row.
export async function pickOption(scope: Page | Locator, testId: string, label: string) {
  const select = selectBox(scope, testId)
  await select.locator('.vs__dropdown-toggle').click()
  await select.getByRole('option', { name: label, exact: true }).click()
}

// textField is the input of a design-system text field. Its root element
// carries the test id as well, so the id alone would match two elements.
export function textField(scope: Page | Locator, testId: string) {
  return scope.locator(`input[data-testid="${testId}"], textarea[data-testid="${testId}"]`)
}
