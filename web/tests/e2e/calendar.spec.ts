import { expect, test, type Page } from '@playwright/test'
import { openApp, openNav, pickOption, selectBox, signIn, textField } from './helpers'

// The page shows times in the browser's zone, so the zone is fixed for the
// checks of times below.
test.use({ timezoneId: 'Europe/Prague' })

async function openCalendar(page: Page) {
  await signIn(page)
  await openApp(page, 'Calendar')
  await expect(page).toHaveURL(/\/organizer\/calendar\/events$/)
}

test('the calendar page shows month, week and day views and hides a calendar', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(err.message))

  await openCalendar(page)

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
  await openNav(page, 'Devices')
  await expect(page).toHaveURL(/\/organizer\/calendar\/devices$/)
  await openNav(page, 'Calendar')
  await expect(page.getByTestId('calendar-item').first().getByTestId('calendar-toggle')).not.toBeChecked()
  await page.getByTestId('calendar-item').first().getByTestId('calendar-toggle').check()

  expect(pageErrors).toEqual([])
})

test('a calendar can be created, renamed, recoloured and deleted', async ({ page }) => {
  await openCalendar(page)
  const items = page.getByTestId('calendar-item')
  const mine = items.filter({ hasText: 'Field notes' })

  await page.getByTestId('calendar-add').click()
  await textField(page, 'calendar-name').fill('Field notes')
  await page.getByTestId('calendar-color-#c2415d').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(mine).toHaveCount(1)

  await mine.getByTestId('calendar-edit').click()
  await expect(textField(page, 'calendar-name')).toHaveValue('Field notes')
  await textField(page, 'calendar-name').fill('Lab notes')
  await page.getByTestId('calendar-color-#2f9e6e').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const renamed = items.filter({ hasText: 'Lab notes' })
  await expect(renamed).toHaveCount(1)
  await expect(renamed.locator('.groupware-swatch')).toHaveCSS('background-color', 'rgb(47, 158, 110)')

  // A name is required.
  await renamed.getByTestId('calendar-edit').click()
  await textField(page, 'calendar-name').fill('')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.locator('#gw-calendar-name-message')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()

  // The server has it: it is still there when the page is opened again.
  await openNav(page, 'Devices')
  await openNav(page, 'Calendar')
  await expect(items.filter({ hasText: 'Lab notes' })).toHaveCount(1)

  await renamed.getByTestId('calendar-delete').click()
  await page.getByTestId('dialog-cancel').click()
  await expect(renamed).toHaveCount(1)
  await renamed.getByTestId('calendar-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(items.filter({ hasText: 'Lab notes' })).toHaveCount(0)
})

function today(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

test('an event can be created, changed and deleted', async ({ page }) => {
  await openCalendar(page)
  const event = (title: string) => page.locator('.fc-event', { hasText: title })

  await page.getByTestId('event-add').click()
  await page.getByTestId('event-subject').fill('Beam time')
  await page.getByTestId('event-start-date').fill(today())
  await page.getByTestId('event-start-time').fill('09:00')
  await page.getByTestId('event-end-date').fill(today())
  await page.getByTestId('event-end-time').fill('10:30')
  await page.getByTestId('event-zone').selectOption('Europe/Prague')
  await page.getByTestId('event-location').fill('Hall 2')
  await page.getByTestId('event-status').selectOption('tentative')
  await page.getByTestId('event-categories').fill('Lab, Beam')
  await page.getByTestId('event-reminder').selectOption('30')
  await page.getByTestId('attendee-input').fill('marie@example.com')
  await page.getByTestId('attendee-add').click()
  await page.getByTestId('event-notes').fill('Bring the detector.')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(event('Beam time')).toHaveCount(1)

  // What was written comes back in the zone it was written in.
  await event('Beam time').click()
  await expect(page.getByTestId('event-subject')).toHaveValue('Beam time')
  await expect(page.getByTestId('event-start-time')).toHaveValue('09:00')
  await expect(page.getByTestId('event-end-time')).toHaveValue('10:30')
  await expect(page.getByTestId('event-zone')).toHaveValue('Europe/Prague')
  await expect(page.getByTestId('event-location')).toHaveValue('Hall 2')
  await expect(page.getByTestId('event-status')).toHaveValue('tentative')
  await expect(page.getByTestId('event-categories')).toHaveValue('Lab, Beam')
  await expect(page.getByTestId('event-reminder')).toHaveValue('30')
  await expect(page.getByTestId('attendee')).toContainText('marie@example.com')
  await expect(page.getByTestId('event-notes')).toHaveValue('Bring the detector.')

  // An end before the start is refused.
  await page.getByTestId('event-end-time').fill('08:00')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByTestId('event-end-error')).toBeVisible()

  await page.getByTestId('event-end-time').fill('11:00')
  await page.getByTestId('event-subject').fill('Beam time 2')
  await page.getByTestId('attendee-remove').click()
  await page.getByTestId('event-reminder').selectOption('none')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(event('Beam time 2')).toHaveCount(1)
  await event('Beam time 2').click()
  await expect(page.getByTestId('event-end-time')).toHaveValue('11:00')
  await expect(page.getByTestId('attendee')).toHaveCount(0)
  await expect(page.getByTestId('event-reminder')).toHaveValue('none')
  await expect(page.getByTestId('event-location')).toHaveValue('Hall 2')

  await page.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(event('Beam time 2')).toHaveCount(0)
})

test('an all-day event is made by clicking a day', async ({ page }) => {
  await openCalendar(page)
  await page.locator(`.fc-daygrid-day[data-date="${today()}"]`).click()
  await expect(page.getByTestId('event-all-day')).toBeChecked()
  await page.getByTestId('event-subject').fill('Conference')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const event = page.locator('.fc-event', { hasText: 'Conference' })
  await expect(event).toHaveCount(1)

  await event.click()
  await expect(page.getByTestId('event-all-day')).toBeChecked()
  await expect(page.getByTestId('event-start-date')).toHaveValue(today())
  await expect(page.getByTestId('event-end-date')).toHaveValue(today())
  await page.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(event).toHaveCount(0)
})

function firstOfMonth(): string {
  return today().slice(0, 8) + '01'
}

test('a repeating event can be changed for one day or for all days', async ({ page }) => {
  await openCalendar(page)
  const events = page.locator('.fc-event', { hasText: 'Stand-up' })
  const day = firstOfMonth()

  await page.getByTestId('event-add').click()
  await page.getByTestId('event-subject').fill('Stand-up')
  await page.getByTestId('event-start-date').fill(day)
  await page.getByTestId('event-start-time').fill('09:00')
  await page.getByTestId('event-end-date').fill(day)
  await page.getByTestId('event-end-time').fill('09:30')
  await page.getByTestId('repeat-frequency').selectOption('daily')
  await page.getByTestId('repeat-end').selectOption('count')
  await page.getByTestId('repeat-count').fill('3')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(events).toHaveCount(3)

  // One day only.
  await events.first().click()
  await page.getByTestId('scope-this').click()
  await expect(page.getByTestId('repeat-frequency')).toHaveCount(0)
  await page.getByTestId('event-subject').fill('Stand-up moved')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.locator('.fc-event', { hasText: 'Stand-up moved' })).toHaveCount(1)
  await expect(events).toHaveCount(3)

  // The whole series: the pattern comes back as it was written.
  await events.last().click()
  await page.getByTestId('scope-all').click()
  await expect(page.getByTestId('repeat-frequency')).toHaveValue('daily')
  await expect(page.getByTestId('repeat-end')).toHaveValue('count')
  await expect(page.getByTestId('repeat-count')).toHaveValue('3')
  await page.getByTestId('repeat-count').fill('2')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(events).toHaveCount(2)

  // Delete one day, then the rest.
  await events.last().click()
  await page.getByTestId('scope-this').click()
  await page.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(events).toHaveCount(1)

  await events.first().click()
  await page.getByTestId('scope-all').click()
  await page.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(events).toHaveCount(0)
})

test('the settings change the first day, week numbers and time zone, and are kept', async ({ page }) => {
  await openCalendar(page)
  await expect(page.locator('.fc-col-header-cell').first()).toContainText('Mon')
  await expect(page.locator('.fc-daygrid-week-number')).toHaveCount(0)

  await page.getByTestId('settings-open').click()
  await pickOption(page, 'settings-first-day', 'Sunday')
  await page.getByTestId('settings-week-numbers').getByRole('checkbox').check()
  await pickOption(page, 'settings-zone', 'Asia/Tokyo')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.locator('.fc-col-header-cell').first()).toContainText('Sun')
  await expect(page.locator('.fc-daygrid-week-number').first()).toBeVisible()

  // A new event is written in the chosen zone.
  await page.getByTestId('event-add').click()
  await expect(page.getByTestId('event-zone')).toHaveValue('Asia/Tokyo')
  await page.getByTestId('dialog-cancel').click()

  // The settings are still there when the page is opened again.
  await openNav(page, 'Devices')
  await openNav(page, 'Calendar')
  await expect(page.locator('.fc-col-header-cell').first()).toContainText('Sun')
  await page.getByTestId('settings-open').click()
  await expect(selectBox(page, 'settings-zone')).toContainText('Asia/Tokyo')
  await page.getByTestId('dialog-cancel').click()
})

test('a calendar can be imported from a file, with a report, and exported again', async ({ page }) => {
  await openCalendar(page)
  const items = page.getByTestId('calendar-item')
  await page.getByTestId('calendar-add').click()
  await textField(page, 'calendar-name').fill('Imported')
  await page.getByTestId('dialog-confirm').click()
  const mine = items.filter({ hasText: 'Imported' })
  await expect(mine).toHaveCount(1)

  const day = today().replace(/-/g, '')
  const ics = [
    'BEGIN:VCALENDAR',
    'VERSION:2.0',
    'PRODID:-//test//EN',
    'BEGIN:VEVENT',
    'UID:import-one@example.com',
    `DTSTART:${day}T080000Z`,
    `DTEND:${day}T090000Z`,
    'SUMMARY:Imported meeting',
    'END:VEVENT',
    'BEGIN:VTODO',
    'UID:import-todo@example.com',
    'SUMMARY:Not an event',
    'END:VTODO',
    'END:VCALENDAR',
    ''
  ].join('\r\n')

  const chooser = page.waitForEvent('filechooser')
  await mine.getByTestId('calendar-import').click()
  await (await chooser).setFiles({
    name: 'meeting.ics',
    mimeType: 'text/calendar',
    buffer: Buffer.from(ics)
  })
  await expect(page.getByTestId('import-summary')).toContainText('1 added')
  await expect(page.getByTestId('import-summary')).toContainText('1 failed')
  await expect(page.getByTestId('import-failure')).toContainText('import-todo@example.com')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.locator('.fc-event', { hasText: 'Imported meeting' })).toHaveCount(1)

  const download = page.waitForEvent('download')
  await mine.getByTestId('calendar-export').click()
  const file = await download
  expect(file.suggestedFilename()).toBe('Imported.ics')
  const path = await file.path()
  const text = (await import('node:fs')).readFileSync(path, 'utf8')
  expect(text).toContain('SUMMARY:Imported meeting')

  await mine.getByTestId('calendar-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(mine).toHaveCount(0)
})
