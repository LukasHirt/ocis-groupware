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
