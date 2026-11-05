import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test'
import { calendarAction, openApp, openNav, pickOption, signIn, textField } from './helpers'
import { capture, escapeCloses, modes, watchBrowser, type Finding, type Mode } from './screenChecks'
import { bookName, calendarName, removeSamples, seedCalendar, seedContacts } from './sampleData'

// A tour of every screen of the application in three modes: light and dark on
// a desktop window, and a 390 pixel phone. Each screen is saved as a picture
// under test-results/screens/<mode>/ and checked for what a machine can
// judge: nothing wider than the window, nothing cut off, a visible focus on
// the first control, Escape closing each dialog, text that is readable
// against its background, controls made by the design system, and a browser
// that reports no error. The pictures are for a person to look at; they are
// not compared pixel by pixel, since fonts differ between machines.

test.use({ timezoneId: 'Europe/Prague', viewport: { width: 1440, height: 900 }, actionTimeout: 15_000 })

const desktop = { width: 1440, height: 900 }

async function withSamples(browser: Browser, fn: (page: Page) => Promise<void>) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, baseURL: process.env.STACK_URL ?? 'https://ocis.localhost', viewport: desktop })
  const page = await context.newPage()
  try {
    await signIn(page)
    await fn(page)
  } finally {
    await context.close()
  }
}

test.beforeAll(async ({ browser }) => {
  test.setTimeout(240_000)
  await withSamples(browser, async (page) => {
    await removeSamples(page)
    await openApp(page, 'Calendar')
    await expect(page.getByTestId('calendar-title')).not.toBeEmpty()
    await seedCalendar(page)
    await openApp(page, 'Contacts')
    await expect(page.locator('h1')).toBeVisible()
    await seedContacts(page)
  })
})

test.afterAll(async ({ browser }) => {
  await withSamples(browser, removeSamples)
})

// go opens an area with a desktop-sized window, because the application menu
// folds away in a narrow one, and then sets the size of the mode.
async function go(page: Page, mode: Mode, area: 'Calendar' | 'Contacts' | 'Devices') {
  await page.setViewportSize(desktop)
  if (area === 'Devices') await openNav(page, 'Devices')
  else await openApp(page, area)
  await expect(page.locator('.gw-body')).toBeVisible()
  await page.setViewportSize({ width: mode.width, height: mode.height })
  await page.waitForTimeout(300)
}

// The list pane is a drop behind a button when the window is narrow.
async function openList(page: Page) {
  if (await page.getByTestId('list-pane').isVisible()) return
  await page.getByTestId('list-toggle').click()
  await expect(page.locator('.gw-list-drop')).toBeVisible()
}

async function closeList(page: Page) {
  if (await page.locator('.gw-list-drop').isVisible()) {
    // A click on the page's title is outside the drop and does nothing else.
    await page.locator('h1').click()
    await expect(page.locator('.gw-list-drop')).toBeHidden()
  }
}

async function setView(page: Page, label: 'Month' | 'Week' | 'Day') {
  const button = page.getByRole('button', { name: label, exact: true })
  if (!(await button.isVisible())) await page.locator('#gw-view-toggle').click()
  await button.click()
  await expect(page.locator(`.fc-${{ Month: 'dayGridMonth', Week: 'timeGridWeek', Day: 'timeGridDay' }[label]}-view`)).toBeVisible()
  await expect(page.locator('.gw-events-progress')).toHaveCount(0)
}

async function closeSideBar(page: Page) {
  await page.locator('#app-sidebar, .sidebar-panel').first().locator('button').first().click()
  await expect(page.locator('.gw-sidebar')).toHaveCount(0)
}

// One test walks all three modes, each in a browser context of its own, so
// that a problem in one mode does not hide the screens of the others.
test('every screen in light, dark and phone sizes', async ({ browser }, info: TestInfo) => {
  test.setTimeout(420_000)
  const failed: string[] = []
  for (const mode of modes) {
    const context = await browser.newContext({
      ignoreHTTPSErrors: true,
      baseURL: process.env.STACK_URL ?? 'https://ocis.localhost',
      timezoneId: 'Europe/Prague',
      colorScheme: mode.colorScheme,
      viewport: desktop
    })
    const page = await context.newPage()
    try {
      await test.step(mode.name, () => tour(page, info, mode))
    } catch (err) {
      failed.push(`${mode.name}: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      await context.close()
    }
  }
  expect(failed).toEqual([])
})

async function tour(page: Page, info: TestInfo, mode: Mode) {
  {
    {
      const problems = watchBrowser(page)
      const findings: Finding[] = []
      const shoot = (name: string, options?: { skipFocus?: boolean }) => capture(page, info, mode, name, options)
      // A dialog is captured, then Escape has to close it.
      const dialog = async (name: string) => {
        await expect(page.getByRole('dialog')).toBeVisible()
        await shoot(name)
        const closed = await escapeCloses(page, `${mode.name}/${name}`)
        findings.push(...closed)
        expect.soft(closed, `${mode.name}/${name}: Escape closes the dialog`).toEqual([])
        if (closed.length) await page.keyboard.press('Escape')
      }

      await signIn(page)

      // Calendar
      await go(page, mode, 'Calendar')
      await expect(page.getByTestId('calendar-title')).not.toBeEmpty()
      await expect(page.locator('.fc-event').first()).toBeVisible()
      await expect(page.locator('.gw-events-progress')).toHaveCount(0)
      await setView(page, 'Month')
      await shoot('calendar-month')
      await setView(page, 'Week')
      await shoot('calendar-week')
      await setView(page, 'Day')
      await shoot('calendar-day')

      await page.locator('.fc-event', { hasText: 'Team stand-up' }).first().click()
      await expect(page.getByTestId('event-details')).toBeVisible()
      await shoot('calendar-event-details')
      await closeSideBar(page)

      await page.getByTestId('event-add').click()
      await pickOption(page, 'repeat-frequency', 'Weekly')
      await page.getByTestId('repeat-day-monday').click()
      await page.getByTestId('repeat-day-wednesday').click()
      await pickOption(page, 'repeat-end', 'After a number of times')
      await textField(page, 'repeat-count').fill('6')
      await dialog('calendar-event-dialog-recurrence')

      await page.getByTestId('settings-open').click()
      await dialog('calendar-settings')

      await openList(page)
      await page.getByTestId('calendar-add').click()
      await dialog('calendar-add')

      const mine = page.getByTestId('calendar-item').filter({ hasText: calendarName })
      await openList(page)
      await calendarAction(page, mine, 'calendar-share')
      await dialog('calendar-share')
      await openList(page)
      await calendarAction(page, mine, 'calendar-delete')
      await dialog('calendar-delete-confirm')
      await closeList(page)

      // Contacts
      await go(page, mode, 'Contacts')
      await expect(page.locator('h1')).toBeVisible()
      await openList(page)
      await page.getByTestId('folder-item').filter({ hasText: bookName }).click()
      await closeList(page)
      await expect(page.getByTestId('contact-item').first()).toBeVisible()
      await shoot('contacts-list')

      await page.getByTestId('contact-item').filter({ hasText: 'Lovelace' }).click()
      await expect(page.getByTestId('contact-detail')).toContainText('Ada Lovelace')
      await shoot('contacts-details')
      await page.getByTestId('contact-edit').click()
      await expect(page.getByTestId('contact-form')).toBeVisible()
      await shoot('contacts-editor')
      await page.getByTestId('form-cancel').click()
      await closeSideBar(page)

      await openList(page)
      await page.getByTestId('book-new').click()
      await dialog('contacts-book-dialog')

      await openList(page)
      await page.getByTestId('group-item').first().click()
      await closeList(page)
      await page.getByTestId('group-rename').click()
      await dialog('contacts-group-dialog')
      await page.getByTestId('group-delete').click()
      await dialog('contacts-group-delete-confirm')

      await openList(page)
      await page.getByTestId('group-all').click()
      await page.getByTestId('folder-item').filter({ hasText: bookName }).click()
      await closeList(page)
      await page.getByTestId('book-import-input').setInputFiles({
        name: 'cards.vcf',
        mimeType: 'text/vcard',
        buffer: Buffer.from(
          'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:screens-edsger\r\nFN:Edsger Dijkstra\r\nN:Dijkstra;Edsger;;;\r\nEND:VCARD\r\n' +
            'BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Broken\r\n'
        )
      })
      await expect(page.getByTestId('import-report')).toBeVisible()
      await dialog('contacts-import-report')

      await page.getByTestId('book-share').click()
      await dialog('contacts-share')

      // Devices
      await go(page, mode, 'Devices')
      await expect(page.locator('h1')).toHaveText('Devices')
      await page.getByTestId('app-password-add').click()
      await dialog('devices-new-password-dialog')
      await page.getByTestId('app-password-add').click()
      await textField(page, 'app-password-name').fill(`Screens ${mode.name}`)
      await page.getByTestId('dialog-confirm').click()
      await expect(page.getByTestId('app-password-done')).toBeVisible()
      await shoot('devices-new-password-secret')
      await page.getByTestId('app-password-done').click()
      const row = page.getByTestId('app-password-item').filter({ hasText: `Screens ${mode.name}` })
      await expect(row).toHaveCount(1)
      await shoot('devices')
      await row.getByTestId('app-password-revoke').click()
      await page.getByTestId('dialog-confirm').click()
      await expect(row).toHaveCount(0)

      // The browser reported nothing wrong along the way.
      expect.soft(problems(), `${mode.name}: errors and failed requests`).toEqual([])
      expect(findings).toEqual([])
    }
  }
}
