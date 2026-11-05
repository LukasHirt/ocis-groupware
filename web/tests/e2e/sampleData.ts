import { expect, type Page } from '@playwright/test'
import { calendarAction, openApp, pickOption, textField } from './helpers'

// Sample data for the screen tests: a calendar with events of several kinds,
// an address book with contacts and a group, both shared with one person.
// It is made through the pages themselves, so it works on any stack, and is
// removed again by deleting the calendar and the address book.

export const calendarName = 'Field notes'
export const bookName = 'Clients'

// The day in the zone the tests run the browser in, which is not the zone of
// the machine that runs them.
export function isoDay(): string {
  return new Date().toLocaleDateString('sv-SE', { timeZone: 'Europe/Prague' })
}

interface Sample {
  subject: string
  start?: string
  end?: string
  allDay?: boolean
  place?: string
  weekly?: boolean
}

// The first of these opens the sidebar in the tour; the short one tests the
// one-line layout, the long title the clipping, and four on one day the
// "more" link of the month view.
export const events: Sample[] = [
  { subject: 'Team stand-up', start: '09:00', end: '09:15' },
  { subject: 'Beam time with the detector group, second shift', start: '10:00', end: '11:30', place: 'Hall 2' },
  { subject: 'Lunch', start: '12:00', end: '13:00' },
  { subject: 'Review', start: '16:00', end: '17:00' },
  { subject: 'Conference', allDay: true },
  { subject: 'Weekly sync', start: '14:00', end: '15:00', weekly: true }
]

async function createEvent(page: Page, e: Sample) {
  await page.getByTestId('event-add').click()
  await textField(page, 'event-subject').fill(e.subject)
  await textField(page, 'event-start-date').fill(isoDay())
  await textField(page, 'event-end-date').fill(isoDay())
  if (e.allDay) {
    await page.getByTestId('event-all-day').getByRole('checkbox').check()
  } else {
    await textField(page, 'event-start-time').fill(e.start!)
    await textField(page, 'event-end-time').fill(e.end!)
  }
  await pickOption(page, 'event-calendar', calendarName)
  if (e.place) await textField(page, 'event-location').fill(e.place)
  if (e.weekly) {
    await pickOption(page, 'repeat-frequency', 'Weekly')
    await pickOption(page, 'repeat-end', 'After a number of times')
    await textField(page, 'repeat-count').fill('4')
  }
  await page.getByTestId('dialog-confirm').click()
  await expect(page.getByRole('dialog')).toBeHidden()
}

async function shareWith(page: Page, person: string, label: string) {
  await textField(page, 'share-search').fill(person)
  await page.getByTestId('share-result').filter({ hasText: label }).first().click()
  await pickOption(page, 'share-new-role', 'Can view')
  await page.getByTestId('share-add').click()
  await expect(page.getByTestId('share-chosen')).toHaveCount(0)
}

// seedCalendar needs the Calendar page to be open.
export async function seedCalendar(page: Page) {
  await page.getByTestId('calendar-add').click()
  await textField(page, 'calendar-name').fill(calendarName)
  await page.getByTestId('calendar-color-#c2415d').click()
  await page.getByTestId('dialog-confirm').click()
  const mine = page.getByTestId('calendar-item').filter({ hasText: calendarName })
  await expect(mine).toHaveCount(1)
  for (const e of events) {
    await createEvent(page, e)
    await expect(page.locator('.gw-events-progress')).toHaveCount(0)
  }
  await calendarAction(page, mine, 'calendar-share')
  await shareWith(page, 'marie', 'Marie')
  await page.getByTestId('dialog-cancel').click()
}

export const contacts = [
  { given: 'Ada', surname: 'Lovelace', email: 'ada@example.org', job: 'Mathematician', city: 'London' },
  { given: 'Grace', surname: 'Hopper', email: 'grace@example.org', job: 'Rear admiral', city: 'New York' },
  { given: 'Alan', surname: 'Turing', email: 'alan@example.org', job: 'Cryptanalyst', city: 'Wilmslow' }
]

// seedContacts needs the Contacts page to be open.
export async function seedContacts(page: Page) {
  await page.getByTestId('book-new').click()
  await textField(page, 'book-new-input').fill(bookName)
  await page.getByTestId('book-new-save').click()
  const book = page.getByTestId('folder-item').filter({ hasText: bookName })
  await expect(book).toHaveCount(1)
  for (const c of contacts) {
    await page.getByTestId('contact-new').click()
    await textField(page, 'field-givenName').fill(c.given)
    await textField(page, 'field-surname').fill(c.surname)
    await textField(page, 'emails-0').fill(c.email)
    await textField(page, 'field-jobTitle').fill(c.job)
    await textField(page, 'homeAddress-city').fill(c.city)
    await page.getByTestId('form-save').click()
    await expect(page.getByTestId('contact-detail')).toContainText(`${c.given} ${c.surname}`)
    await page.getByTestId('membership-start').click()
    await textField(page, 'membership-input').fill('Pioneers')
    await page.getByTestId('membership-add').click()
    await expect(page.getByTestId('detail-membership').or(page.getByTestId('membership-name'))).toBeVisible()
  }
  await page.getByTestId('book-share').click()
  await shareWith(page, 'marie', 'Marie')
  await page.getByTestId('dialog-cancel').click()
}

// removeSamples deletes what the seeds made, and what an earlier run that
// stopped halfway left behind.
export async function removeSamples(page: Page) {
  await openApp(page, 'Contacts')
  await expect(page.getByTestId('folder-item').first()).toBeVisible()
  const book = page.getByTestId('folder-item').filter({ hasText: bookName })
  while ((await book.count()) > 0) {
    const before = await book.count()
    await book.first().click()
    await page.getByTestId('book-actions').click()
    await page.getByTestId('book-delete').click()
    await page.getByTestId('dialog-confirm').click()
    await expect(book).toHaveCount(before - 1)
  }
  await openApp(page, 'Calendar')
  await expect(page.getByTestId('calendar-item').first()).toBeVisible()
  const mine = page.getByTestId('calendar-item').filter({ hasText: calendarName })
  while ((await mine.count()) > 0) {
    const before = await mine.count()
    await calendarAction(page, mine.first(), 'calendar-delete')
    await page.getByTestId('dialog-confirm').click()
    await expect(mine).toHaveCount(before - 1)
  }
}
