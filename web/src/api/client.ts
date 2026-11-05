import { toApiError } from './errors'
import type {
  CalendarInput,
  ContactFolderInput,
  ContactInput,
  EventInput,
  GraphCalendar,
  GraphContact,
  GraphContactFolder,
  GraphEvent,
  Page
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

const enc = encodeURIComponent

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
  async function list<T>(url: string, params?: Record<string, string>): Promise<T[]> {
    const items: T[] = []
    let next: Request | undefined = { method: 'GET', url, params }
    while (next) {
      const page: Page<T> = await send<Page<T>>(next)
      items.push(...page.value)
      const link: string | undefined = page['@odata.nextLink']
      if (link) {
        const target = new URL(link, 'http://placeholder.invalid')
        next = { method: 'GET', url: target.pathname + target.search }
      } else {
        next = undefined
      }
    }
    return items
  }

  return {
    listCalendars: () => list<GraphCalendar>(`${graph}/calendars`),
    createCalendar: (input: CalendarInput) =>
      send<GraphCalendar>({ method: 'POST', url: `${graph}/calendars`, data: input }),
    updateCalendar: (id: string, input: CalendarInput) =>
      send<GraphCalendar>({ method: 'PATCH', url: `${graph}/calendars/${enc(id)}`, data: input }),
    deleteCalendar: (id: string) =>
      send<void>({ method: 'DELETE', url: `${graph}/calendars/${enc(id)}` }),

    // calendarView returns the single events and the occurrences of series
    // between two instants, expanded by the server.
    calendarView: (q: { start: string; end: string; calendarId?: string }) =>
      list<GraphEvent>(
        q.calendarId ? `${graph}/calendars/${enc(q.calendarId)}/calendarView` : `${graph}/calendarView`,
        { startDateTime: q.start, endDateTime: q.end }
      ),
    // createEvent writes into the given calendar, or the default one.
    createEvent: (input: EventInput, calendarId?: string) =>
      send<GraphEvent>({
        method: 'POST',
        url: calendarId ? `${graph}/calendars/${enc(calendarId)}/events` : `${graph}/events`,
        data: input
      }),
    getEvent: (id: string) => send<GraphEvent>({ method: 'GET', url: `${graph}/events/${enc(id)}` }),
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
