import { expect, test, type Page } from '@playwright/test'
import { openApp, openNav, calendarAction, pickOption, selectBox, signIn, textField } from './helpers'

// The page shows times in the browser's zone, so the zone is fixed for the
// checks of times below.
test.use({ timezoneId: 'Europe/Prague' })

// settled waits until the page is done reading events. The grid draws its
// events again when the answer arrives, and a click that lands in between
// would hit an element that is replaced.
async function settled(page: Page) {
  await expect(page.locator('.gw-events-progress')).toHaveCount(0)
}

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
  await expect(page.getByTestId('calendar-title')).not.toBeEmpty()

  await page.getByRole('button', { name: 'Week', exact: true }).click()
  await expect(page.locator('.fc-timeGridWeek-view')).toBeVisible()
  await page.getByRole('button', { name: 'Day', exact: true }).click()
  await expect(page.locator('.fc-timeGridDay-view')).toBeVisible()
  await page.getByRole('button', { name: 'Month', exact: true }).click()
  await expect(page.locator('.fc-dayGridMonth-view')).toBeVisible()

  // The default calendar is listed with its colour, and can be switched off.
  const item = page.getByTestId('calendar-item').first()
  await expect(item).toContainText('Personal')
  const toggle = item.getByTestId('calendar-toggle').getByRole('checkbox')
  await expect(toggle).toBeChecked()
  await toggle.uncheck()

  // The choice stays when the page is opened again. A reload would go to the
  // service itself, since the application's paths are not served by Web.
  await openNav(page, 'Devices')
  await expect(page).toHaveURL(/\/organizer\/calendar\/devices$/)
  await openNav(page, 'Calendar')
  const again = page.getByTestId('calendar-item').first().getByTestId('calendar-toggle').getByRole('checkbox')
  await expect(again).not.toBeChecked()
  await again.check()

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

  await calendarAction(page, mine, 'calendar-edit')
  await expect(textField(page, 'calendar-name')).toHaveValue('Field notes')
  await textField(page, 'calendar-name').fill('Lab notes')
  await page.getByTestId('calendar-color-#2f9e6e').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const renamed = items.filter({ hasText: 'Lab notes' })
  await expect(renamed).toHaveCount(1)
  await expect(renamed.locator('.gw-swatch')).toHaveCSS('background-color', 'rgb(47, 158, 110)')

  // A name is required.
  await calendarAction(page, renamed, 'calendar-edit')
  await textField(page, 'calendar-name').fill('')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.locator('#gw-calendar-name-message')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()

  // The server has it: it is still there when the page is opened again.
  await openNav(page, 'Devices')
  await openNav(page, 'Calendar')
  await expect(items.filter({ hasText: 'Lab notes' })).toHaveCount(1)

  await calendarAction(page, renamed, 'calendar-delete')
  await page.getByTestId('dialog-cancel').click()
  await expect(renamed).toHaveCount(1)
  await calendarAction(page, renamed, 'calendar-delete')
  await page.getByTestId('dialog-confirm').click()
  await expect(items.filter({ hasText: 'Lab notes' })).toHaveCount(0)
})

function today(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

test('an event can be created, shown in the sidebar, changed and deleted', async ({ page }) => {
  await openCalendar(page)
  const event = (title: string) => page.locator('.fc-event', { hasText: title })
  const details = page.getByTestId('event-details')

  await page.getByTestId('event-add').click()
  await textField(page, 'event-subject').fill('Beam time')
  await textField(page, 'event-start-date').fill(today())
  await textField(page, 'event-start-time').fill('09:00')
  await textField(page, 'event-end-date').fill(today())
  await textField(page, 'event-end-time').fill('10:30')
  await pickOption(page, 'event-zone', 'Europe/Prague')
  await textField(page, 'event-location').fill('Hall 2')
  await pickOption(page, 'event-status', 'Tentative')
  await textField(page, 'event-categories').fill('Lab, Beam')
  await pickOption(page, 'event-reminder', '30 minutes before')
  await textField(page, 'attendee-input').fill('marie@example.com')
  await page.getByTestId('attendee-add').click()
  await textField(page, 'event-notes').fill('Bring the detector.')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(event('Beam time')).toHaveCount(1)

  // A click shows the event in the sidebar, in the zone it was written in.
  await settled(page)
  await event('Beam time').click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(details.getByTestId('event-title')).toHaveText('Beam time')
  await expect(details.getByTestId('event-when')).toContainText('9:00')
  await expect(details.getByTestId('event-when')).toContainText('10:30')
  await expect(details).toContainText('Europe/Prague')
  await expect(details.getByTestId('event-place')).toHaveText('Hall 2')
  await expect(details.getByTestId('event-status-text')).toHaveText('Tentative')
  await expect(details.getByTestId('event-reminder-text')).toHaveText('30 minutes before')
  await expect(details.getByTestId('event-tags')).toContainText('Lab')
  await expect(details.getByTestId('event-tags')).toContainText('Beam')
  await expect(details.getByTestId('event-attendee')).toContainText('marie@example.com')
  await expect(details.getByTestId('event-notes')).toHaveText('Bring the detector.')

  // Edit opens the editor with the same values.
  await details.getByTestId('event-edit').click()
  await expect(textField(page, 'event-subject')).toHaveValue('Beam time')
  await expect(textField(page, 'event-start-time')).toHaveValue('09:00')
  await expect(textField(page, 'event-end-time')).toHaveValue('10:30')
  await expect(selectBox(page, 'event-zone')).toContainText('Europe/Prague')
  await expect(textField(page, 'event-location')).toHaveValue('Hall 2')
  await expect(selectBox(page, 'event-status')).toContainText('Tentative')
  await expect(textField(page, 'event-categories')).toHaveValue('Lab, Beam')
  await expect(selectBox(page, 'event-reminder')).toContainText('30 minutes before')
  await expect(page.getByRole('dialog').getByTestId('attendee')).toContainText('marie@example.com')
  await expect(textField(page, 'event-notes')).toHaveValue('Bring the detector.')

  // An end before the start is refused, and the message belongs to the field.
  await textField(page, 'event-end-time').fill('08:00')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.locator('#gw-event-end-time-message')).toBeVisible()

  await textField(page, 'event-end-time').fill('11:00')
  await textField(page, 'event-subject').fill('Beam time 2')
  await page.getByTestId('attendee-remove').click()
  await pickOption(page, 'event-reminder', 'No reminder')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(event('Beam time 2')).toHaveCount(1)

  // The sidebar follows the change.
  await expect(details.getByTestId('event-title')).toHaveText('Beam time 2')
  await expect(details.getByTestId('event-when')).toContainText('11:00')
  await expect(details.getByTestId('event-attendee')).toHaveCount(0)
  await expect(details.getByTestId('event-reminder-text')).toHaveCount(0)
  await expect(details.getByTestId('event-place')).toHaveText('Hall 2')

  await details.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(event('Beam time 2')).toHaveCount(0)
})

test('an event opens in the editor when it is clicked twice', async ({ page }) => {
  await openCalendar(page)
  const event = (title: string) => page.locator('.fc-event', { hasText: title })
  await page.getByTestId('event-add').click()
  await textField(page, 'event-subject').fill('Quick one')
  // The default is the next hour, which late in the evening runs into the next day and shows twice.
  await textField(page, 'event-start-date').fill(today())
  await textField(page, 'event-start-time').fill('09:00')
  await textField(page, 'event-end-date').fill(today())
  await textField(page, 'event-end-time').fill('10:00')
  await page.getByTestId('dialog-confirm').click()
  await expect(event('Quick one')).toHaveCount(1)

  // The sidebar opens beside the grid and moves the events, so the double
  // click is on an event that stays where it is: the first click opens it.
  await settled(page)
  await event('Quick one').click()
  await expect(page.getByTestId('event-title')).toHaveText('Quick one')
  await event('Quick one').dblclick()
  await expect(textField(page, 'event-subject')).toHaveValue('Quick one')
  await page.getByTestId('dialog-cancel').click()

  // A click within half a second of the last one counts as the second of two.
  await page.waitForTimeout(600)
  await settled(page)
  await event('Quick one').click()
  await page.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(event('Quick one')).toHaveCount(0)
})

test('an all-day event is made by clicking a day', async ({ page }) => {
  await openCalendar(page)
  await page.locator(`.fc-daygrid-day[data-date="${today()}"]`).click()
  await expect(page.getByTestId('event-all-day').getByRole('checkbox')).toBeChecked()
  await textField(page, 'event-subject').fill('Conference')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const event = page.locator('.fc-event', { hasText: 'Conference' })
  await expect(event).toHaveCount(1)

  await settled(page)
  await event.click()
  await expect(page.getByTestId('event-title')).toHaveText('Conference')
  await expect(page.getByTestId('event-when')).not.toContainText(':')
  await page.getByTestId('event-edit').click()
  await expect(page.getByTestId('event-all-day').getByRole('checkbox')).toBeChecked()
  await expect(textField(page, 'event-start-date')).toHaveValue(today())
  await expect(textField(page, 'event-end-date')).toHaveValue(today())
  await page.getByTestId('dialog-cancel').click()
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
  await textField(page, 'event-subject').fill('Stand-up')
  await textField(page, 'event-start-date').fill(day)
  await textField(page, 'event-start-time').fill('09:00')
  await textField(page, 'event-end-date').fill(day)
  await textField(page, 'event-end-time').fill('09:30')
  await pickOption(page, 'repeat-frequency', 'Daily')
  await pickOption(page, 'repeat-end', 'After a number of times')
  await textField(page, 'repeat-count').fill('3')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(events).toHaveCount(3)

  // The sidebar says how it repeats, and nothing is asked before an action.
  await settled(page)
  await events.first().click()
  await expect(page.getByTestId('event-repeats')).toContainText('Every day')
  await expect(page.getByRole('dialog')).toHaveCount(0)

  // One day only.
  await page.getByTestId('event-edit').click()
  await page.getByTestId('scope-this').click()
  await expect(page.getByTestId('repeat-frequency')).toHaveCount(0)
  await textField(page, 'event-subject').fill('Stand-up moved')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.locator('.fc-event', { hasText: 'Stand-up moved' })).toHaveCount(1)
  await expect(events).toHaveCount(3)

  // The whole series: the pattern comes back as it was written. The weekday
  // buttons are not shown for a daily pattern.
  await settled(page)
  await events.last().click()
  await page.getByTestId('event-edit').click()
  await page.getByTestId('scope-all').click()
  await expect(selectBox(page, 'repeat-frequency')).toContainText('Daily')
  await expect(selectBox(page, 'repeat-end')).toContainText('After a number of times')
  await expect(textField(page, 'repeat-count')).toHaveValue('3')
  await textField(page, 'repeat-count').fill('2')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(events).toHaveCount(2)

  // Delete one day, then the rest.
  await settled(page)
  await events.last().click()
  await page.getByTestId('event-delete').click()
  await page.getByTestId('scope-this').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(events).toHaveCount(1)

  await settled(page)
  await events.first().click()
  await page.getByTestId('event-delete').click()
  await page.getByTestId('scope-all').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(events).toHaveCount(0)
})

test('a weekly pattern is set with the weekday buttons', async ({ page }) => {
  await openCalendar(page)
  await page.getByTestId('event-add').click()
  await textField(page, 'event-subject').fill('Weekly sync')
  // The weekday of the start is pressed already. The default start is the next
  // hour, which late in the evening is tomorrow, so a day that is neither
  // today nor tomorrow is used.
  await pickOption(page, 'repeat-frequency', 'Weekly')
  const group = page.getByRole('group', { name: 'Repeat on' })
  await expect(group.getByRole('button')).toHaveCount(7)
  const days = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday']
  const friday = page.getByTestId(`repeat-day-${days[(new Date().getDay() + 3) % 7]}`)
  await expect(friday).toHaveAttribute('aria-pressed', 'false')
  await friday.click()
  await expect(friday).toHaveAttribute('aria-pressed', 'true')
  await friday.click()
  await expect(friday).toHaveAttribute('aria-pressed', 'false')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
})

test('Escape closes every dialog of the calendar', async ({ page }) => {
  await openCalendar(page)
  const dialog = page.getByRole('dialog')
  const closesOnEscape = async (open: () => Promise<void>) => {
    await open()
    await expect(dialog).toBeVisible()
    // The dialog takes the focus when it opens; Escape goes to the focused element.
    await expect(dialog.locator(':focus')).toHaveCount(1)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
  }

  await closesOnEscape(() => page.getByTestId('event-add').click())
  await closesOnEscape(() => page.getByTestId('settings-open').click())
  await closesOnEscape(() => page.getByTestId('calendar-add').click())
  const item = page.getByTestId('calendar-item').first()
  await closesOnEscape(() => calendarAction(page, item, 'calendar-edit'))
  await closesOnEscape(() => calendarAction(page, item, 'calendar-share'))

  // A confirmation and a question about a series, from the sidebar.
  await page.getByTestId('event-add').click()
  await textField(page, 'event-subject').fill('Escape test')
  await textField(page, 'event-start-date').fill(today())
  await textField(page, 'event-start-time').fill('09:00')
  await textField(page, 'event-end-date').fill(today())
  await textField(page, 'event-end-time').fill('10:00')
  await page.getByTestId('dialog-confirm').click()
  const event = page.locator('.fc-event', { hasText: 'Escape test' })
  await expect(event).toHaveCount(1)
  await settled(page)
  await event.click()
  await closesOnEscape(() => page.getByTestId('event-delete').click())
  await page.getByTestId('event-delete').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(event).toHaveCount(0)
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
  await expect(selectBox(page, 'event-zone')).toContainText('Asia/Tokyo')
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
  await calendarAction(page, mine, 'calendar-import')
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
  await calendarAction(page, mine, 'calendar-export')
  const file = await download
  expect(file.suggestedFilename()).toBe('Imported.ics')
  const path = await file.path()
  const text = (await import('node:fs')).readFileSync(path, 'utf8')
  expect(text).toContain('SUMMARY:Imported meeting')

  await calendarAction(page, mine, 'calendar-delete')
  await page.getByTestId('dialog-confirm').click()
  await expect(mine).toHaveCount(0)
})
