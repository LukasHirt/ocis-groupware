// Types of the Graph facade's JSON. They follow what the server writes (the
// golden files under internal/graphapi/testdata/contract), not the whole of
// Microsoft Graph. Enumerations are plain strings because Graph allows new
// values to appear.

export interface Page<T> {
  value: T[]
  '@odata.nextLink'?: string
}

export interface OwnerName {
  name: string
}

export interface GraphCalendar {
  id: string
  name: string
  // "auto" or a preset name; hexColor holds the colour when one is set.
  color: string
  hexColor: string
  isDefaultCalendar: boolean
  changeKey: string
  canShare: boolean
  canViewPrivateItems: boolean
  canEdit: boolean
  isRemovable: boolean
  owner: OwnerName
}

export interface CalendarInput {
  name?: string
  hexColor?: string
}

export interface ItemBody {
  contentType: string
  content: string
}

export interface DateTimeTimeZone {
  // Local time without an offset, e.g. 2026-11-05T09:00:00.0000000.
  dateTime: string
  timeZone: string
}

export interface EmailAddress {
  name: string
  address: string
}

export interface Attendee {
  type: string
  status: { response: string; time?: string }
  emailAddress: EmailAddress
}

export interface PatternedRecurrence {
  pattern: {
    type: string
    interval: number
    month: number
    dayOfMonth: number
    daysOfWeek: string[]
    firstDayOfWeek: string
    index: string
  }
  range: {
    type: string
    startDate: string
    endDate?: string
    recurrenceTimeZone?: string
    numberOfOccurrences: number
  }
}

export interface GraphEvent {
  id: string
  '@odata.etag'?: string
  changeKey?: string
  iCalUId: string
  // singleInstance, seriesMaster, occurrence or exception.
  type: string
  seriesMasterId?: string
  subject: string
  body: ItemBody
  start: DateTimeTimeZone
  end: DateTimeTimeZone
  isAllDay: boolean
  isCancelled: boolean
  showAs: string
  sensitivity: string
  categories: string[]
  attendees: Attendee[]
  organizer?: { emailAddress: EmailAddress }
  recurrence: PatternedRecurrence | null
  // False when the series has a rule the editor cannot represent.
  'groupware.recurrenceEditable'?: boolean
  location: { displayName: string }
  isReminderOn: boolean
  reminderMinutesBeforeStart: number
  createdDateTime?: string
  lastModifiedDateTime?: string
}

// The writable fields of an event. A field left out is not changed; null
// clears the ones the server allows to clear.
export type EventInput = Partial<
  Pick<
    GraphEvent,
    | 'subject'
    | 'body'
    | 'start'
    | 'end'
    | 'isAllDay'
    | 'showAs'
    | 'sensitivity'
    | 'categories'
    | 'location'
    | 'isReminderOn'
    | 'reminderMinutesBeforeStart'
  >
> & {
  recurrence?: PatternedRecurrence | null
  // An attendee needs no response status; the server keeps the stored one.
  attendees?: (Pick<Attendee, 'type' | 'emailAddress'> & Partial<Pick<Attendee, 'status'>>)[]
}

export interface PhysicalAddress {
  street: string
  city: string
  state: string
  countryOrRegion: string
  postalCode: string
}

export interface ContactGroup {
  // The UID of the group card. Groups are shown and never written.
  id: string
  displayName: string
}

export interface GraphContactFolder {
  id: string
  displayName: string
  'groupware.isDefault': boolean
  'groupware.canEdit': boolean
  'groupware.canShare': boolean
  'groupware.owner': OwnerName
}

export interface ContactFolderInput {
  displayName?: string
}

export interface GraphContact {
  id: string
  '@odata.etag'?: string
  changeKey?: string
  parentFolderId?: string
  lastModifiedDateTime?: string
  categories: string[]
  birthday: string | null
  displayName: string
  givenName: string
  middleName: string
  surname: string
  nickName: string
  title: string
  generation: string
  yomiGivenName: string
  yomiSurname: string
  yomiCompany: string
  jobTitle: string
  companyName: string
  department: string
  profession: string
  assistantName: string
  manager: string
  spouseName: string
  children: string[]
  businessPhones: string[]
  homePhones: string[]
  mobilePhone: string
  businessHomePage: string
  personalNotes: string
  emailAddresses: EmailAddress[]
  imAddresses: string[]
  homeAddress: PhysicalAddress
  businessAddress: PhysicalAddress
  otherAddress: PhysicalAddress
  'groupware.groups': ContactGroup[]
  'groupware.hasPhoto': boolean
}

// The writable fields of a contact. A field left out is not changed; null or
// an empty value clears it.
export type ContactInput = Partial<
  Omit<
    GraphContact,
    | 'id'
    | '@odata.etag'
    | 'changeKey'
    | 'parentFolderId'
    | 'lastModifiedDateTime'
    | 'groupware.groups'
    | 'groupware.hasPhoto'
  >
>

export interface GraphErrorBody {
  error: {
    code: string
    message: string
    innerError?: { 'request-id'?: string; date?: string }
  }
}

// What an import did with each object of the file. Index counts from 0.
export interface ImportResult {
  index: number
  uid: string
  name?: string
  status: 'created' | 'updated' | 'failed'
  error?: string
}

export interface ImportReport {
  total: number
  created: number
  updated: number
  failed: number
  results: ImportResult[]
}

// What a collection can be shared as. A calendar's permissions are below it
// as calendarPermissions, an address book's as permissions.
export type ShareKind = 'calendar' | 'addressBook'

export type Role = 'read' | 'write'

export interface Grantee {
  type: 'user' | 'group'
  // The id of the user or group in the oCIS directory.
  id: string
}

// What the server writes for a permission. Role and grantee type are plain
// strings, as for the other enumerations, and are one of Role and Grantee's
// types today.
export interface GraphPermission {
  id: string
  emailAddress: OwnerName
  isInsideOrganization: boolean
  isRemovable: boolean
  role: string
  allowedRoles: string[]
  grantee: { type: string; id: string }
}

// A user or group as the directory's search returns it.
export interface DirectoryEntry {
  id: string
  displayName: string
}

// An app password as the list shows it. The secret is never part of it.
// last_used_at is null until a client signs in with it.
export interface AppPassword {
  id: string
  name: string
  created_at: string
  expires_at: string
  last_used_at: string | null
}

// The answer to a creation: the only time the secret is returned.
export interface IssuedAppPassword extends AppPassword {
  password: string
}

export interface AppPasswordInput {
  name: string
  // Shortens the lifetime, 1 to 365. The server's default is 365.
  expires_in_days?: number
}

// The signed-in user's DAV address, as GET /groupware/v1/me answers it.
// username and the two home addresses are empty when the user's name is not
// available to DAV clients.
export interface Account {
  username: string
  displayName: string
  // True when the user's oCIS username is held by another account that can
  // still use it. username is then empty.
  username_conflict?: boolean
  dav: {
    url: string
    calendarHomeUrl: string
    addressBookHomeUrl: string
  }
}
