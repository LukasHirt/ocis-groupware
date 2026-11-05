import { DateTime } from 'luxon'
import type { EventInput, GraphEvent } from '../api/types'
import { calendarIdOf } from './mapping'
import {
  defaultRecurrence,
  recurrenceFormFromEvent,
  recurrenceFromForm,
  recurrenceProblem,
  type RecurrenceForm,
  type RecurrenceProblem
} from './recurrence'

export interface FormAttendee {
  type: string
  name: string
  address: string
}

// EventForm is what the event dialog edits. It keeps wall-clock dates and
// times as the inputs write them, in the zone of the event. For an all-day
// event the end date is the last day, as people count it; Graph's end is the
// midnight after it.
export interface EventForm {
  calendarId: string
  subject: string
  location: string
  notes: string
  allDay: boolean
  startDate: string
  startTime: string
  endDate: string
  endTime: string
  timeZone: string
  showAs: string
  // Names separated by commas.
  categories: string
  attendees: FormAttendee[]
  recurrence: RecurrenceForm
}

const dateFormat = 'yyyy-MM-dd'
const timeFormat = 'HH:mm'

function wall(value: { dateTime: string; timeZone: string }): DateTime {
  // The server writes the local time without an offset, so the zone is only
  // a label here and the fields are read as they stand.
  return DateTime.fromISO(value.dateTime, { zone: 'utc' })
}

export function formFromEvent(event: GraphEvent): EventForm {
  const start = wall(event.start)
  let end = wall(event.end)
  if (event.isAllDay) {
    end = end.minus({ days: 1 })
    if (end < start) {
      end = start
    }
  }
  return {
    calendarId: calendarIdOf(event.id),
    subject: event.subject,
    location: event.location?.displayName ?? '',
    notes: event.body?.content ?? '',
    allDay: event.isAllDay,
    startDate: start.toFormat(dateFormat),
    startTime: start.toFormat(timeFormat),
    endDate: end.toFormat(dateFormat),
    endTime: end.toFormat(timeFormat),
    timeZone: event.start.timeZone,
    showAs: event.showAs || 'busy',
    categories: event.categories.join(', '),
    attendees: event.attendees.map((a) => ({
      type: a.type || 'required',
      name: a.emailAddress.name,
      address: a.emailAddress.address
    })),
    recurrence: recurrenceFormFromEvent(event.recurrence, start.toFormat(dateFormat))
  }
}

// A selection in the grid, as instants.
export interface Selection {
  calendarId: string
  zone: string
  start: string
  end?: string
  allDay: boolean
}

export function blankForm(s: Selection): EventForm {
  const start = DateTime.fromISO(s.start, { zone: s.zone })
  let end = s.end ? DateTime.fromISO(s.end, { zone: s.zone }) : start.plus({ hours: 1 })
  if (s.allDay) {
    end = end.minus({ days: 1 })
    if (end < start) {
      end = start
    }
  }
  return {
    calendarId: s.calendarId,
    subject: '',
    location: '',
    notes: '',
    allDay: s.allDay,
    startDate: start.toFormat(dateFormat),
    startTime: s.allDay ? '09:00' : start.toFormat(timeFormat),
    endDate: end.toFormat(dateFormat),
    endTime: s.allDay ? '10:00' : end.toFormat(timeFormat),
    timeZone: s.zone,
    showAs: 'busy',
    categories: '',
    attendees: [],
    recurrence: defaultRecurrence(start.toFormat(dateFormat))
  }
}

export function parseCategories(text: string): string[] {
  const seen = new Set<string>()
  const names: string[] = []
  for (const part of text.split(',')) {
    const name = part.trim()
    if (name !== '' && !seen.has(name.toLowerCase())) {
      seen.add(name.toLowerCase())
      names.push(name)
    }
  }
  return names
}

const stamp = (date: string, time: string) => `${date}T${time}:00`

function times(f: EventForm): Pick<EventInput, 'start' | 'end' | 'isAllDay'> {
  if (f.allDay) {
    const last = DateTime.fromISO(f.endDate, { zone: 'utc' }).plus({ days: 1 })
    return {
      isAllDay: true,
      start: { dateTime: stamp(f.startDate, '00:00'), timeZone: f.timeZone },
      end: { dateTime: stamp(last.toFormat(dateFormat), '00:00'), timeZone: f.timeZone }
    }
  }
  return {
    isAllDay: false,
    start: { dateTime: stamp(f.startDate, f.startTime), timeZone: f.timeZone },
    end: { dateTime: stamp(f.endDate, f.endTime), timeZone: f.timeZone }
  }
}

const attendeeInputs = (list: FormAttendee[]) =>
  list.map((a) => ({
    type: a.type,
    emailAddress: { name: a.name, address: a.address }
  }))

// inputFromForm is the whole event, for a create.
export function inputFromForm(f: EventForm): EventInput {
  const recurrence = recurrenceFromForm(f.recurrence, f.startDate, f.timeZone)
  return {
    ...(recurrence ? { recurrence } : {}),
    subject: f.subject.trim(),
    body: { contentType: 'text', content: f.notes },
    location: { displayName: f.location.trim() },
    showAs: f.showAs,
    categories: parseCategories(f.categories),
    attendees: attendeeInputs(f.attendees),
    ...times(f)
  }
}

const sameAttendees = (a: FormAttendee[], b: FormAttendee[]) =>
  JSON.stringify(a.map((x) => [x.address.toLowerCase(), x.name, x.type])) ===
  JSON.stringify(b.map((x) => [x.address.toLowerCase(), x.name, x.type]))

// changeFromForms is what a PATCH must carry to turn the original into the
// edited form. Start and end always travel together, with isAllDay.
export function changeFromForms(original: EventForm, edited: EventForm): EventInput {
  const change: EventInput = {}
  const full = inputFromForm(edited)
  if (edited.subject.trim() !== original.subject.trim()) {
    change.subject = full.subject
  }
  if (edited.notes !== original.notes) {
    change.body = full.body
  }
  if (edited.location.trim() !== original.location.trim()) {
    change.location = full.location
  }
  if (edited.showAs !== original.showAs) {
    change.showAs = edited.showAs
  }
  if (
    JSON.stringify(parseCategories(edited.categories)) !==
    JSON.stringify(parseCategories(original.categories))
  ) {
    change.categories = full.categories
  }
  if (!sameAttendees(original.attendees, edited.attendees)) {
    change.attendees = full.attendees
  }
  const moved =
    edited.allDay !== original.allDay ||
    edited.startDate !== original.startDate ||
    edited.endDate !== original.endDate ||
    edited.timeZone !== original.timeZone ||
    (!edited.allDay && (edited.startTime !== original.startTime || edited.endTime !== original.endTime))
  if (moved) {
    Object.assign(change, times(edited))
  }
  if (JSON.stringify(original.recurrence) !== JSON.stringify(edited.recurrence)) {
    change.recurrence = recurrenceFromForm(edited.recurrence, edited.startDate, edited.timeZone)
  }
  return change
}

// formProblem names the first field that cannot be saved, or nothing.
export function formProblem(f: EventForm): 'start' | 'end' | RecurrenceProblem | undefined {
  if (f.startDate === '' || (!f.allDay && f.startTime === '')) {
    return 'start'
  }
  if (f.endDate === '' || (!f.allDay && f.endTime === '')) {
    return 'end'
  }
  const start = f.allDay ? f.startDate : `${f.startDate}T${f.startTime}`
  const end = f.allDay ? f.endDate : `${f.endDate}T${f.endTime}`
  if (end < start) {
    return 'end'
  }
  return recurrenceProblem(f.recurrence, f.startDate)
}

const addressPattern = /^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/

export function attendeeError(address: string, existing: FormAttendee[]): 'invalid' | 'duplicate' | undefined {
  if (!addressPattern.test(address)) {
    return 'invalid'
  }
  return existing.some((a) => a.address.toLowerCase() === address.toLowerCase()) ? 'duplicate' : undefined
}
