import { expect, test, type Page } from '@playwright/test'

// The demo users that oCIS creates with IDM_CREATE_DEMO_USERS.
const user = { name: 'einstein', password: 'relativity' }

// The page shows times in the browser's zone, so the zone is fixed for the
// checks of times below.
test.use({ timezoneId: 'Europe/Prague' })

async function signIn(page: Page) {
  await page.goto('/')
  await page.locator('#oc-login-username').fill(user.name)
  await page.locator('#oc-login-password').fill(user.password)
  await page.locator('button[type="submit"]').click()
  await expect(page.locator('#web-content')).toBeVisible()
}

async function openCalendar(page: Page) {
  await signIn(page)
  await page.locator('#_appSwitcherButton').click()
  await page.getByRole('link', { name: 'Calendar and Contacts' }).click()
  await expect(page).toHaveURL(/\/groupware\/calendar$/)
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
  const sidebar = page.locator('#web-nav-sidebar')
  await sidebar.getByRole('link', { name: 'Contacts' }).click()
  await expect(page).toHaveURL(/\/groupware\/contacts$/)
  await sidebar.getByRole('link', { name: 'Calendar' }).click()
  await expect(page.getByTestId('calendar-item').first().getByTestId('calendar-toggle')).not.toBeChecked()
  await page.getByTestId('calendar-item').first().getByTestId('calendar-toggle').check()

  expect(pageErrors).toEqual([])
})

test('a calendar can be created, renamed, recoloured and deleted', async ({ page }) => {
  await openCalendar(page)
  const items = page.getByTestId('calendar-item')
  const mine = items.filter({ hasText: 'Field notes' })

  await page.getByTestId('calendar-add').click()
  await page.getByTestId('calendar-name').fill('Field notes')
  await page.getByTestId('calendar-color-#c2415d').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(mine).toHaveCount(1)

  await mine.getByTestId('calendar-edit').click()
  await expect(page.getByTestId('calendar-name')).toHaveValue('Field notes')
  await page.getByTestId('calendar-name').fill('Lab notes')
  await page.getByTestId('calendar-color-#2f9e6e').click()
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const renamed = items.filter({ hasText: 'Lab notes' })
  await expect(renamed).toHaveCount(1)
  await expect(renamed.locator('.groupware-swatch')).toHaveCSS('background-color', 'rgb(47, 158, 110)')

  // A name is required.
  await renamed.getByTestId('calendar-edit').click()
  await page.getByTestId('calendar-name').fill('')
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByTestId('calendar-name-error')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()

  // The server has it: it is still there when the page is opened again.
  const sidebar = page.locator('#web-nav-sidebar')
  await sidebar.getByRole('link', { name: 'Contacts' }).click()
  await sidebar.getByRole('link', { name: 'Calendar' }).click()
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
