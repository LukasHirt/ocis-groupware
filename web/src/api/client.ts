import { toApiError } from './errors'
import type {
  Account,
  AppPassword,
  AppPasswordInput,
  IssuedAppPassword,
  CalendarInput,
  ContactFolderInput,
  ContactInput,
  EventInput,
  GraphCalendar,
  GraphContact,
  GraphContactFolder,
  GraphEvent,
  GraphPermission,
  Grantee,
  ImportReport,
  DirectoryEntry,
  Page,
  Role,
  ShareKind
} from './types'

export interface Request {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  url: string
  params?: Record<string, string>
  data?: unknown
  headers?: Record<string, string>
  // 'blob' for a binary answer such as a photo.
  responseType?: 'blob'
}

export interface Response {
  status: number
  data: unknown
}

// A transport sends one request and throws what axios throws for a failed
// status. In the browser it is the authenticated client of oCIS Web; tests
// pass a fake.
export type Transport = (request: Request) => Promise<Response>

const graph = '/graph/v1.0/me'
const groupware = '/groupware/v1'

const enc = encodeURIComponent

const appPasswords = '/groupware/v1/app-passwords'

const permissionsOf = (kind: ShareKind, id: string) =>
  kind === 'calendar'
    ? `${graph}/calendars/${enc(id)}/calendarPermissions`
    : `${graph}/contactFolders/${enc(id)}/permissions`

// The directory is the oCIS Graph API, which the signed-in user's own token
// may search. It answers only from three characters on.
const directory = '/graph/v1.0'
const searchLimit = '10'

// inZone asks the server to write times in a zone, the way Graph does. Without
// it the times are in UTC.
function inZone(zone?: string): Pick<Request, 'headers'> {
  return zone ? { headers: { Prefer: `outlook.timezone="${zone}"` } } : {}
}

// ifMatch gives the headers for a write that must not overwrite a newer
// version. Without an etag the write is unconditional.
function ifMatch(etag?: string): Pick<Request, 'headers'> {
  return etag ? { headers: { 'If-Match': etag } } : {}
}

export function createClient(transport: Transport) {
  async function send<T>(request: Request): Promise<T> {
    try {
      return (await transport(request)).data as T
    } catch (err) {
      throw toApiError(err)
    }
  }

  // list reads every page. The server's next link names the host it saw, so
  // only the path and query are followed.
  async function list<T>(
    url: string,
    params?: Record<string, string>,
    extra: Pick<Request, 'headers'> = {}
  ): Promise<T[]> {
    const items: T[] = []
    let next: Request | undefined = { method: 'GET', url, params, ...extra }
    while (next) {
      const page: Page<T> = await send<Page<T>>(next)
      items.push(...page.value)
      const link: string | undefined = page['@odata.nextLink']
      if (link) {
        const target = new URL(link, 'http://placeholder.invalid')
        next = { method: 'GET', url: target.pathname + target.search, ...extra }
      } else {
        next = undefined
      }
    }
    return items
  }

  async function search(what: 'users' | 'groups', term: string): Promise<DirectoryEntry[]> {
    const page = await send<Page<DirectoryEntry>>({
      method: 'GET',
      url: `${directory}/${what}`,
      params: { $search: term, $top: searchLimit }
    })
    return page.value.map((e) => ({ id: e.id, displayName: e.displayName }))
  }

  return {
    listCalendars: () => list<GraphCalendar>(`${graph}/calendars`),
    createCalendar: (input: CalendarInput) =>
      send<GraphCalendar>({ method: 'POST', url: `${graph}/calendars`, data: input }),
    updateCalendar: (id: string, input: CalendarInput) =>
      send<GraphCalendar>({ method: 'PATCH', url: `${graph}/calendars/${enc(id)}`, data: input }),
    deleteCalendar: (id: string) =>
      send<void>({ method: 'DELETE', url: `${graph}/calendars/${enc(id)}` }),

    // The file is the request body as it is, up to 20 MB.
    importCalendar: (id: string, file: Blob) =>
      send<ImportReport>({
        method: 'POST',
        url: `${groupware}/calendars/${enc(id)}/import`,
        data: file,
        headers: { 'Content-Type': file.type || 'text/calendar' }
      }),
    exportCalendar: (id: string) =>
      send<Blob>({
        method: 'GET',
        url: `${groupware}/calendars/${enc(id)}/export`,
        responseType: 'blob'
      }),

    // calendarView returns the single events and the occurrences of series
    // between two instants, expanded by the server.
    // The zone is the one the times are written in.
    calendarView: (q: { start: string; end: string; calendarId?: string; zone?: string }) =>
      list<GraphEvent>(
        q.calendarId ? `${graph}/calendars/${enc(q.calendarId)}/calendarView` : `${graph}/calendarView`,
        { startDateTime: q.start, endDateTime: q.end },
        inZone(q.zone)
      ),
    // createEvent writes into the given calendar, or the default one.
    createEvent: (input: EventInput, calendarId?: string) =>
      send<GraphEvent>({
        method: 'POST',
        url: calendarId ? `${graph}/calendars/${enc(calendarId)}/events` : `${graph}/events`,
        data: input
      }),
    getEvent: (id: string, zone?: string) =>
      send<GraphEvent>({ method: 'GET', url: `${graph}/events/${enc(id)}`, ...inZone(zone) }),
    // updateEvent and deleteEvent take an occurrence id to change or remove
    // one occurrence of a series.
    updateEvent: (id: string, input: EventInput, etag?: string) =>
      send<GraphEvent>({
        method: 'PATCH',
        url: `${graph}/events/${enc(id)}`,
        data: input,
        ...ifMatch(etag)
      }),
    deleteEvent: (id: string, etag?: string) =>
      send<void>({ method: 'DELETE', url: `${graph}/events/${enc(id)}`, ...ifMatch(etag) }),

    listContactFolders: () => list<GraphContactFolder>(`${graph}/contactFolders`),
    createContactFolder: (input: ContactFolderInput) =>
      send<GraphContactFolder>({ method: 'POST', url: `${graph}/contactFolders`, data: input }),
    updateContactFolder: (id: string, input: ContactFolderInput) =>
      send<GraphContactFolder>({
        method: 'PATCH',
        url: `${graph}/contactFolders/${enc(id)}`,
        data: input
      }),
    deleteContactFolder: (id: string) =>
      send<void>({ method: 'DELETE', url: `${graph}/contactFolders/${enc(id)}` }),

    // listContacts reads one folder, or the default folder when no folder
    // is given.
    listContacts: (folderId?: string) =>
      list<GraphContact>(
        folderId ? `${graph}/contactFolders/${enc(folderId)}/contacts` : `${graph}/contacts`
      ),
    getContact: (folderId: string, id: string) =>
      send<GraphContact>({
        method: 'GET',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts/${enc(id)}`
      }),
    createContact: (folderId: string, input: ContactInput) =>
      send<GraphContact>({
        method: 'POST',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts`,
        data: input
      }),
    updateContact: (folderId: string, id: string, input: ContactInput, etag?: string) =>
      send<GraphContact>({
        method: 'PATCH',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts/${enc(id)}`,
        data: input,
        ...ifMatch(etag)
      }),
    deleteContact: (folderId: string, id: string, etag?: string) =>
      send<void>({
        method: 'DELETE',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts/${enc(id)}`,
        ...ifMatch(etag)
      }),

    // exportAddressBook downloads the address book as one vCard file.
    // importAddressBook sends a vCard file as the body, 20 MB at most, and
    // answers with what happened to each card.
    exportAddressBook: (id: string) =>
      send<Blob>({
        method: 'GET',
        url: `/groupware/v1/addressbooks/${enc(id)}/export`,
        responseType: 'blob'
      }),
    importAddressBook: (id: string, file: Blob) =>
      send<ImportReport>({
        method: 'POST',
        url: `/groupware/v1/addressbooks/${enc(id)}/import`,
        data: file,
        headers: { 'Content-Type': 'text/vcard' }
      }),

    listPermissions: (kind: ShareKind, id: string) =>
      list<GraphPermission>(permissionsOf(kind, id)),
    createPermission: (kind: ShareKind, id: string, grantee: Grantee, role: Role) =>
      send<GraphPermission>({
        method: 'POST',
        url: permissionsOf(kind, id),
        data: { role, grantee }
      }),
    updatePermission: (kind: ShareKind, id: string, permissionId: string, role: Role) =>
      send<GraphPermission>({
        method: 'PATCH',
        url: `${permissionsOf(kind, id)}/${enc(permissionId)}`,
        data: { role }
      }),
    deletePermission: (kind: ShareKind, id: string, permissionId: string) =>
      send<void>({ method: 'DELETE', url: `${permissionsOf(kind, id)}/${enc(permissionId)}` }),

    getAccount: () => send<Account>({ method: 'GET', url: '/groupware/v1/me' }),

    // The list never carries a secret. createAppPassword answers with the
    // secret once; revokeAppPassword refuses the clients that use it.
    listAppPasswords: async () =>
      (await send<{ app_passwords: AppPassword[] }>({ method: 'GET', url: appPasswords })).app_passwords,
    createAppPassword: (input: AppPasswordInput) =>
      send<IssuedAppPassword>({ method: 'POST', url: appPasswords, data: input }),
    revokeAppPassword: (id: string) =>
      send<void>({ method: 'DELETE', url: `${appPasswords}/${enc(id)}` }),

    searchUsers: (term: string) => search('users', term),
    searchGroups: (term: string) => search('groups', term),

    // A photo is read as a blob and written as the raw image, JPEG or PNG
    // of at most 1 MB, with the image's own content type.
    getContactPhoto: (folderId: string, id: string) =>
      send<Blob>({
        method: 'GET',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts/${enc(id)}/photo/$value`,
        responseType: 'blob'
      }),
    setContactPhoto: (folderId: string, id: string, image: Blob) =>
      send<unknown>({
        method: 'PUT',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts/${enc(id)}/photo/$value`,
        data: image,
        headers: { 'Content-Type': image.type }
      }),
    deleteContactPhoto: (folderId: string, id: string) =>
      send<void>({
        method: 'DELETE',
        url: `${graph}/contactFolders/${enc(folderId)}/contacts/${enc(id)}/photo/$value`
      })
  }
}

export type Client = ReturnType<typeof createClient>
