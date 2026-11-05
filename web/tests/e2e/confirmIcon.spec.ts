import { expect, test, type Page } from '@playwright/test'
import { openApp, openNav, signIn, textField } from './helpers'

// The revoke question of the Devices page gives the confirm dialog an icon
// of its own.
async function openRevokeDialog(page: Page) {
  await signIn(page)
  await openApp(page, 'Calendar')
  await openNav(page, 'Devices')
  await page.getByTestId('app-password-add').click()
  await page.locator('input[data-testid="app-password-name"]').fill(`Icon test ${Date.now()}`)
  await page.getByTestId('dialog-confirm').click()
  await page.getByTestId('app-password-done').click()
  await page.getByTestId('app-password-revoke').first().click()
}

// The question about a calendar uses the icon the dialog has by default.
async function openDeleteCalendarDialog(page: Page) {
  await signIn(page)
  await openApp(page, 'Calendar')
  await page.getByTestId('calendar-add').click()
  const name = `Icon probe ${Date.now()}`
  await textField(page, 'calendar-name').fill(name)
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.getByTestId('calendar-item').filter({ hasText: name }).getByTestId('calendar-delete').first().click()
}

const dialogs = { revoke: openRevokeDialog, 'delete calendar': openDeleteCalendarDialog }

for (const [name, open] of Object.entries(dialogs)) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`the icon of the ${name} dialog is drawn (${scheme})`, async ({ browser }, info) => {
      const context = await browser.newContext({
        colorScheme: scheme,
        ignoreHTTPSErrors: true,
        baseURL: info.project.use.baseURL
      })
      const page = await context.newPage()
      await open(page)

      // The design system loads the icon by name as an SVG. A name it cannot
      // draw leaves an empty wrapper.
      const svg = page.locator('.oc-modal-title .oc-icon svg')
      await expect(svg).toHaveCount(1)
      await expect(svg.locator('path').first()).toHaveAttribute('d', /.+/)
      const box = await svg.boundingBox()
      expect(box?.width).toBeGreaterThan(0)
      expect(box?.height).toBeGreaterThan(0)
      if (process.env.SHOT_DIR) {
        await page.screenshot({ path: `${process.env.SHOT_DIR}/${name.replace(' ', '-')}-${scheme}.png` })
      }
      // Both questions remove what the test made.
      await page.getByTestId('dialog-confirm').click()
      await expect(page.getByRole('dialog')).toBeHidden()
      await context.close()
    })
  }
}
