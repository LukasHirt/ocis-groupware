import { describe, expect, it } from 'vitest'
import { createClient, type Request, type Response, type Transport } from '../../src/api/client'
import { ApiError } from '../../src/api/errors'
import type {
  GraphCalendar,
  GraphContact,
  GraphContactFolder,
  GraphEvent,
  GraphPermission,
  ImportReport,
  Page
} from '../../src/api/types'
import calendars from '../../../internal/graphapi/testdata/contract/calendars.json'
import calendarsPage from '../../../internal/graphapi/testdata/contract/calendars-page.json'
import calendar from '../../../internal/graphapi/testdata/contract/calendar.json'
import calendarShared from '../../../internal/graphapi/testdata/contract/calendar-shared.json'
import calendarview from '../../../internal/graphapi/testdata/contract/calendarview.json'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import contactFolder from '../../../internal/graphapi/testdata/contract/contact-folder.json'
import calendarPermission from '../../../internal/graphapi/testdata/contract/calendar-permission.json'
import calendarPermissions from '../../../internal/graphapi/testdata/contract/calendar-permissions.json'
import contactFolderPermission from '../../../internal/graphapi/testdata/contract/contactfolder-permission.json'
import contactFolderShared from '../../../internal/graphapi/testdata/contract/contact-folder-shared.json'
import contactFolders from '../../../internal/graphapi/testdata/contract/contact-folders.json'
import importReport from '../../../internal/graphapi/testdata/contract/import-report-contacts.json'
import contacts from '../../../internal/graphapi/testdata/contract/contacts.json'
import event from '../../../internal/graphapi/testdata/contract/event.json'
import eventOccurrence from '../../../internal/graphapi/testdata/contract/event-occurrence.json'
import eventSeries from '../../../internal/graphapi/testdata/contract/event-series.json'
import eventZone from '../../../internal/graphapi/testdata/contract/event-zone.json'
import events from '../../../internal/graphapi/testdata/contract/events.json'

// The golden files are written by the Go tests of the facade. Typing them
// here makes a change of shape on the server fail the type check, and the key
// lists below make a new or removed field fail the test.
const calendarKeys = {
  id: 1, name: 1, color: 1, hexColor: 1, isDefaultCalendar: 1, changeKey: 1, canShare: 1,
  canViewPrivateItems: 1, canEdit: 1, isRemovable: 1, owner: 1
} satisfies Record<keyof GraphCalendar, 1>
const eventKeys = {
  id: 1, '@odata.etag': 1, changeKey: 1, iCalUId: 1, type: 1, seriesMasterId: 1, subject: 1,
  body: 1, start: 1, end: 1, isAllDay: 1, isCancelled: 1, showAs: 1, sensitivity: 1,
  categories: 1, attendees: 1, organizer: 1, recurrence: 1, 'groupware.recurrenceEditable': 1,
  location: 1, isReminderOn: 1, reminderMinutesBeforeStart: 1, createdDateTime: 1,
  lastModifiedDateTime: 1
} satisfies Record<keyof GraphEvent, 1>
const folderKeys = {
  id: 1, displayName: 1, 'groupware.isDefault': 1, 'groupware.canEdit': 1,
  'groupware.canShare': 1, 'groupware.owner': 1
} satisfies Record<keyof GraphContactFolder, 1>
const contactKeys = {
  id: 1, '@odata.etag': 1, changeKey: 1, parentFolderId: 1, lastModifiedDateTime: 1,
  categories: 1, birthday: 1, displayName: 1, givenName: 1, middleName: 1, surname: 1,
  nickName: 1, title: 1, generation: 1, yomiGivenName: 1, yomiSurname: 1, yomiCompany: 1,
  jobTitle: 1, companyName: 1, department: 1, profession: 1, assistantName: 1, manager: 1,
  spouseName: 1, children: 1, businessPhones: 1, homePhones: 1, mobilePhone: 1,
  businessHomePage: 1, personalNotes: 1, emailAddresses: 1, imAddresses: 1, homeAddress: 1,
  businessAddress: 1, otherAddress: 1, 'groupware.groups': 1, 'groupware.hasPhoto': 1
} satisfies Record<keyof GraphContact, 1>

const permissionKeys = {
  id: 1, emailAddress: 1, isInsideOrganization: 1, isRemovable: 1, role: 1, allowedRoles: 1,
  grantee: 1
} satisfies Record<keyof GraphPermission, 1>

function onlyKnown(items: object[], known: object) {
  for (const item of items) {
    for (const key of Object.keys(item)) {
      expect(Object.keys(known), `unknown field ${key}`).toContain(key)
    }
  }
}

describe('the golden files match the client types', () => {
  it('calendars', () => {
    const all: GraphCalendar[] = [
      ...calendars.value,
      ...calendarsPage.value,
      calendar,
      calendarShared
    ]
    onlyKnown(all, calendarKeys)
  })

  it('events', () => {
    const all: GraphEvent[] = [
      ...events.value,
      ...calendarview.value,
      event,
      eventOccurrence,
      eventSeries,
      eventZone
    ]
    onlyKnown(all, eventKeys)
    // Fields that are always present must be there in every file.
    for (const e of all) {
      expect(e.start.timeZone).toBeTruthy()
      expect(Array.isArray(e.attendees)).toBe(true)
    }
  })

  it('contact folders', () => {
    const all: GraphContactFolder[] = [...contactFolders.value, contactFolder, contactFolderShared]
    onlyKnown(all, folderKeys)
  })

  it('permissions', () => {
    const all: GraphPermission[] = [
      ...calendarPermissions.value,
      calendarPermission,
      contactFolderPermission
    ]
    onlyKnown(all, permissionKeys)
    for (const p of all) {
      expect(['user', 'group']).toContain(p.grantee.type)
      expect(p.allowedRoles).toContain(p.role)
    }
  })

  it('contacts', () => {
    const all: GraphContact[] = [...contacts.value, contact]
    onlyKnown(all, contactKeys)
  })
})

// fake answers each request from a table of "METHOD path" and records them.
function fake(answers: Record<string, Response | ((r: Request) => Response)>) {
  const calls: Request[] = []
  const transport: Transport = (req) => {
    calls.push(req)
    const answer = answers[`${req.method} ${req.url}`]
    if (!answer) {
      return Promise.reject({
        response: { status: 404, data: { error: { code: 'itemNotFound', message: 'no' } } }
      })
    }
    try {
      return Promise.resolve(typeof answer === 'function' ? answer(req) : answer)
    } catch (err) {
      return Promise.reject(err)
    }
  }
  return { client: createClient(transport), calls }
}

const graph = '/graph/v1.0'

describe('calendars', () => {
  it('lists them and follows the next link of a page', async () => {
    const { client, calls } = fake({
      [`GET ${graph}/me/calendars`]: { status: 200, data: calendarsPage as Page<GraphCalendar> },
      [`GET ${graph}/me/calendars?%24skip=1&%24top=1`]: {
        status: 200,
        data: { value: [calendars.value[1]] }
      }
    })
    const list = await client.listCalendars()
    expect(list.map((c) => c.name)).toEqual(['Personal', 'Work'])
    // The link names the host the server saw; the client keeps path and query.
    expect(calls[1].url).toBe(`${graph}/me/calendars?%24skip=1&%24top=1`)
  })

  it('creates, renames and deletes', async () => {
    const { client, calls } = fake({
      [`POST ${graph}/me/calendars`]: { status: 201, data: calendar },
      [`PATCH ${graph}/me/calendars/abc`]: { status: 200, data: calendar },
      [`DELETE ${graph}/me/calendars/abc`]: { status: 204, data: undefined }
    })
    await client.createCalendar({ name: 'Trips', hexColor: '#336699' })
    await client.updateCalendar('abc', { name: 'Holidays' })
    await client.deleteCalendar('abc')
    expect(calls.map((c) => c.method)).toEqual(['POST', 'PATCH', 'DELETE'])
    expect(calls[0].data).toEqual({ name: 'Trips', hexColor: '#336699' })
  })
})

describe('events', () => {
  it('reads the calendar view between two instants', async () => {
    const { client, calls } = fake({
      [`GET ${graph}/me/calendarView`]: { status: 200, data: calendarview as Page<GraphEvent> }
    })
    const view = await client.calendarView({
      start: '2026-11-01T00:00:00Z',
      end: '2026-12-01T00:00:00Z'
    })
    expect(view.map((e) => e.type)).toEqual([
      'singleInstance',
      'occurrence',
      'exception',
      'occurrence'
    ])
    expect(calls[0].params).toEqual({
      startDateTime: '2026-11-01T00:00:00Z',
      endDateTime: '2026-12-01T00:00:00Z'
    })
  })

  it('limits the view to one calendar', async () => {
    const { client, calls } = fake({
      [`GET ${graph}/me/calendars/c1/calendarView`]: { status: 200, data: { value: [] } }
    })
    await client.calendarView({ start: 'a', end: 'b', calendarId: 'c1' })
    expect(calls[0].url).toBe(`${graph}/me/calendars/c1/calendarView`)
  })

  it('asks for the times in a zone with the Prefer header, on every page', async () => {
    const { client, calls } = fake({
      [`GET ${graph}/me/calendarView`]: {
        status: 200,
        data: { value: [], '@odata.nextLink': 'https://host/graph/v1.0/me/calendarView?%24skip=1' }
      },
      [`GET ${graph}/me/calendarView?%24skip=1`]: { status: 200, data: { value: [] } },
      [`GET ${graph}/me/events/e1`]: { status: 200, data: event }
    })
    await client.calendarView({ start: 'a', end: 'b', zone: 'Europe/Prague' })
    await client.getEvent('e1', 'Europe/Prague')
    const prefer = { Prefer: 'outlook.timezone="Europe/Prague"' }
    expect(calls.map((c) => c.headers)).toEqual([prefer, prefer, prefer])
  })

  it('leaves the Prefer header out when no zone is given', async () => {
    const { client, calls } = fake({
      [`GET ${graph}/me/calendarView`]: { status: 200, data: { value: [] } }
    })
    await client.calendarView({ start: 'a', end: 'b' })
    expect(calls[0].headers).toBeUndefined()
  })

  it('creates in the default calendar or a given one', async () => {
    const { client, calls } = fake({
      [`POST ${graph}/me/events`]: { status: 201, data: event },
      [`POST ${graph}/me/calendars/c1/events`]: { status: 201, data: event }
    })
    await client.createEvent({ subject: 'A' })
    await client.createEvent({ subject: 'B' }, 'c1')
    expect(calls.map((c) => c.url)).toEqual([
      `${graph}/me/events`,
      `${graph}/me/calendars/c1/events`
    ])
  })

  it('sends If-Match on update and delete when it has an etag', async () => {
    const { client, calls } = fake({
      [`PATCH ${graph}/me/events/e1`]: { status: 200, data: event },
      [`DELETE ${graph}/me/events/e1`]: { status: 204, data: undefined }
    })
    await client.updateEvent('e1', { subject: 'X' }, 'W/"1"')
    await client.deleteEvent('e1', 'W/"1"')
    await client.updateEvent('e1', { subject: 'Y' })
    expect(calls[0].headers).toEqual({ 'If-Match': 'W/"1"' })
    expect(calls[1].headers).toEqual({ 'If-Match': 'W/"1"' })
    expect(calls[2].headers).toBeUndefined()
  })

  it('addresses an occurrence by its own id', async () => {
    const id = 'cGVyc29uYWwtY2Fs.c2VyaWVzLmljcw.20261106T090000Z'
    const { client, calls } = fake({
      [`DELETE ${graph}/me/events/${id}`]: { status: 204, data: undefined }
    })
    await client.deleteEvent(id)
    expect(calls).toHaveLength(1)
  })
})

describe('contacts', () => {
  it('lists folders and the contacts of a folder', async () => {
    const { client } = fake({
      [`GET ${graph}/me/contactFolders`]: { status: 200, data: contactFolders },
      [`GET ${graph}/me/contactFolders/f1/contacts`]: { status: 200, data: contacts },
      [`GET ${graph}/me/contacts`]: { status: 200, data: contacts }
    })
    expect((await client.listContactFolders()).map((f) => f.displayName)).toEqual([
      'Contacts',
      'Team'
    ])
    expect(await client.listContacts('f1')).toHaveLength(contacts.value.length)
    expect(await client.listContacts()).toHaveLength(contacts.value.length)
  })

  it('writes folders and contacts', async () => {
    const { client, calls } = fake({
      [`POST ${graph}/me/contactFolders`]: { status: 201, data: contactFolder },
      [`PATCH ${graph}/me/contactFolders/f1`]: { status: 200, data: contactFolder },
      [`DELETE ${graph}/me/contactFolders/f1`]: { status: 204, data: undefined },
      [`POST ${graph}/me/contactFolders/f1/contacts`]: { status: 201, data: contact },
      [`PATCH ${graph}/me/contactFolders/f1/contacts/k1`]: { status: 200, data: contact },
      [`DELETE ${graph}/me/contactFolders/f1/contacts/k1`]: { status: 204, data: undefined },
      [`GET ${graph}/me/contactFolders/f1/contacts/k1`]: { status: 200, data: contact }
    })
    await client.createContactFolder({ displayName: 'Clients' })
    await client.updateContactFolder('f1', { displayName: 'Customers' })
    await client.deleteContactFolder('f1')
    await client.createContact('f1', { givenName: 'Ada' })
    await client.updateContact('f1', 'k1', { surname: 'Lovelace' }, 'W/"2"')
    await client.deleteContact('f1', 'k1')
    const got = await client.getContact('f1', 'k1')
    expect(got.displayName).toBe('Dr. Marie Salomea Curie')
    expect(calls).toHaveLength(7)
    expect(calls[4].headers).toEqual({ 'If-Match': 'W/"2"' })
  })
})

describe('address book files', () => {
  it('has types that match the import report', () => {
    const r: ImportReport = importReport as ImportReport
    expect(r.results.map((x) => x.status)).toEqual(['created', 'failed', 'created'])
  })

  it('downloads an address book as a blob', async () => {
    const blob = new Blob(['BEGIN:VCARD'], { type: 'text/vcard' })
    const { client, calls } = fake({
      [`GET /groupware/v1/addressbooks/b1/export`]: { status: 200, data: blob }
    })
    expect(await client.exportAddressBook('b1')).toBe(blob)
    expect(calls[0].responseType).toBe('blob')
  })

  it('sends a file as the raw body and returns the report', async () => {
    const file = new Blob(['BEGIN:VCARD'], { type: 'text/vcard' })
    const { client, calls } = fake({
      [`POST /groupware/v1/addressbooks/b1/import`]: { status: 200, data: importReport }
    })
    const report = await client.importAddressBook('b1', file)
    expect(report.failed).toBe(1)
    expect(calls[0].data).toBe(file)
    expect(calls[0].headers).toEqual({ 'Content-Type': 'text/vcard' })
  })
})

describe('contact photos', () => {
  const base = `${graph}/me/contactFolders/f1/contacts/k1/photo/$value`
  it('reads a photo as a blob', async () => {
    const blob = new Blob(['x'], { type: 'image/png' })
    const { client, calls } = fake({ [`GET ${base}`]: { status: 200, data: blob } })
    expect(await client.getContactPhoto('f1', 'k1')).toBe(blob)
    expect(calls[0].responseType).toBe('blob')
  })

  it('uploads a photo with its content type and removes it', async () => {
    const bytes = new Blob(['x'], { type: 'image/jpeg' })
    const { client, calls } = fake({
      [`PUT ${base}`]: { status: 200, data: {} },
      [`DELETE ${base}`]: { status: 204, data: undefined }
    })
    await client.setContactPhoto('f1', 'k1', bytes)
    await client.deleteContactPhoto('f1', 'k1')
    expect(calls[0].data).toBe(bytes)
    expect(calls[0].headers).toEqual({ 'Content-Type': 'image/jpeg' })
    expect(calls[1].method).toBe('DELETE')
  })
})

describe('errors', () => {
  it('carries status, code and message of the Graph error body', async () => {
    const { client } = fake({
      [`PATCH ${graph}/me/events/e1`]: () => {
        throw {
          response: {
            status: 412,
            data: {
              error: {
                code: 'preconditionFailed',
                message: 'The event changed',
                innerError: { 'request-id': 'r-1' }
              }
            }
          }
        }
      }
    })
    const err = await client.updateEvent('e1', {}, 'W/"x"').catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({
      status: 412,
      code: 'preconditionFailed',
      message: 'The event changed',
      requestId: 'r-1',
      isConflict: true
    })
  })

  it('makes an ApiError of a failure without an answer', async () => {
    const client = createClient(() => Promise.reject(new Error('Network Error')))
    const err = await client.listCalendars().catch((e) => e)
    expect(err).toMatchObject({ status: 0, code: 'networkError', message: 'Network Error' })
  })

  it('copes with an error answer that is not a Graph body', async () => {
    const client = createClient(() =>
      Promise.reject({ response: { status: 502, data: '<html>bad gateway</html>' } })
    )
    const err = await client.listCalendars().catch((e) => e)
    expect(err).toMatchObject({ status: 502, code: 'generalException' })
  })
})

describe('sharing', () => {
  const dir = `${graph}/me`
  it('lists the permissions of a calendar and of an address book', async () => {
    const { client, calls } = fake({
      [`GET ${dir}/calendars/c%2F1/calendarPermissions`]: {
        status: 200,
        data: calendarPermissions as Page<GraphPermission>
      },
      [`GET ${dir}/contactFolders/b1/permissions`]: { status: 200, data: { value: [] } }
    })
    const calendarList = await client.listPermissions('calendar', 'c/1')
    expect(calendarList.map((p) => p.grantee)).toEqual([
      { type: 'user', id: 'u-marie' },
      { type: 'group', id: 'g-physics' }
    ])
    expect(await client.listPermissions('addressBook', 'b1')).toEqual([])
    expect(calls).toHaveLength(2)
  })

  it('shares, changes the role and revokes', async () => {
    const { client, calls } = fake({
      [`POST ${dir}/calendars/c1/calendarPermissions`]: { status: 201, data: calendarPermission },
      [`PATCH ${dir}/calendars/c1/calendarPermissions/p%2F1`]: { status: 200, data: calendarPermission },
      [`DELETE ${dir}/contactFolders/b1/permissions/p1`]: { status: 204, data: undefined }
    })
    await client.createPermission('calendar', 'c1', { type: 'user', id: 'u-marie' }, 'read')
    await client.updatePermission('calendar', 'c1', 'p/1', 'write')
    await client.deletePermission('addressBook', 'b1', 'p1')
    expect(calls[0].data).toEqual({ role: 'read', grantee: { type: 'user', id: 'u-marie' } })
    expect(calls[1].data).toEqual({ role: 'write' })
    expect(calls.map((c) => c.method)).toEqual(['POST', 'PATCH', 'DELETE'])
  })

  it('searches users and groups of the directory with $search', async () => {
    const { client, calls } = fake({
      [`GET /graph/v1.0/users`]: {
        status: 200,
        data: { value: [{ id: 'u1', displayName: 'Marie Curie', mail: 'marie@example.org' }] }
      },
      [`GET /graph/v1.0/groups`]: { status: 200, data: { value: [{ id: 'g1', displayName: 'physics-lovers' }] } }
    })
    expect(await client.searchUsers('mar')).toEqual([{ id: 'u1', displayName: 'Marie Curie' }])
    expect(await client.searchGroups('phy')).toEqual([{ id: 'g1', displayName: 'physics-lovers' }])
    expect(calls.map((c) => c.params)).toEqual([{ $search: 'mar', $top: '10' }, { $search: 'phy', $top: '10' }])
  })
})
