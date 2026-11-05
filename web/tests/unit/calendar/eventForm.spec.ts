import { describe, expect, it } from 'vitest'
import {
  attendeeError,
  blankForm,
  changeFromForms,
  formFromEvent,
  formProblem,
  inputFromForm,
  parseCategories,
  type EventForm
} from '../../../src/calendar/eventForm'
import type { GraphEvent } from '../../../src/api/types'
import eventSeries from '../../../../internal/graphapi/testdata/contract/event-series.json'
import calendarview from '../../../../internal/graphapi/testdata/contract/calendarview.json'
import eventZone from '../../../../internal/graphapi/testdata/contract/event-zone.json'

const planning = calendarview.value[0] as GraphEvent

describe('formFromEvent', () => {
  it('reads a timed event in its own zone', () => {
    const form = formFromEvent(planning)
    expect(form).toMatchObject({
      calendarId: 'cGVyc29uYWwtY2Fs',
      subject: 'Planning',
      location: 'Room 4',
      notes: 'Agenda',
      allDay: false,
      startDate: '2026-11-05',
      startTime: '09:00',
      endDate: '2026-11-05',
      endTime: '10:30',
      timeZone: 'UTC',
      showAs: 'busy',
      categories: 'Work'
    })
    expect(form.attendees).toEqual([
      { type: 'required', name: 'Marie Curie', address: 'marie@example.com' }
    ])
  })

  it('shows the last day of an all-day event as its end date', () => {
    const form = formFromEvent({
      ...planning,
      isAllDay: true,
      start: { dateTime: '2026-11-05T00:00:00.0000000', timeZone: 'UTC' },
      end: { dateTime: '2026-11-07T00:00:00.0000000', timeZone: 'UTC' }
    })
    expect(form.allDay).toBe(true)
    expect(form.startDate).toBe('2026-11-05')
    expect(form.endDate).toBe('2026-11-06')
  })

  it('keeps the zone name the server used', () => {
    expect(formFromEvent(eventZone as GraphEvent).timeZone).toBe((eventZone as GraphEvent).start.timeZone)
  })
})

describe('blankForm', () => {
  it('starts an all-day selection as an all-day event with an inclusive end', () => {
    const form = blankForm({
      calendarId: 'c',
      zone: 'Europe/Prague',
      start: '2026-11-04T23:00:00.000Z',
      end: '2026-11-06T23:00:00.000Z',
      allDay: true
    })
    expect(form).toMatchObject({ allDay: true, startDate: '2026-11-05', endDate: '2026-11-06' })
  })

  it('turns a selected time range into the wall time of the zone', () => {
    const form = blankForm({
      calendarId: 'c',
      zone: 'Europe/Prague',
      start: '2026-07-01T07:00:00.000Z',
      end: '2026-07-01T07:30:00.000Z',
      allDay: false
    })
    expect(form).toMatchObject({
      allDay: false,
      startDate: '2026-07-01',
      startTime: '09:00',
      endDate: '2026-07-01',
      endTime: '09:30',
      timeZone: 'Europe/Prague'
    })
  })

  it('has sensible defaults', () => {
    const form = blankForm({ calendarId: 'c', zone: 'UTC', start: '2026-07-01T07:00:00.000Z', allDay: false })
    expect(form).toMatchObject({ subject: '', showAs: 'busy', categories: '', attendees: [], endTime: '08:00' })
  })
})

describe('inputFromForm', () => {
  const form: EventForm = {
    ...formFromEvent(planning),
    subject: 'Review',
    startDate: '2026-11-06',
    endDate: '2026-11-06',
    timeZone: 'Europe/Prague'
  }

  it('writes local times with the zone', () => {
    const input = inputFromForm(form)
    expect(input.start).toEqual({ dateTime: '2026-11-06T09:00:00', timeZone: 'Europe/Prague' })
    expect(input.end).toEqual({ dateTime: '2026-11-06T10:30:00', timeZone: 'Europe/Prague' })
    expect(input.subject).toBe('Review')
    expect(input.body).toEqual({ contentType: 'text', content: 'Agenda' })
    expect(input.location).toEqual({ displayName: 'Room 4' })
    expect(input.isAllDay).toBe(false)
    expect(input.categories).toEqual(['Work'])
    expect(input.attendees).toEqual([
      { type: 'required', emailAddress: { name: 'Marie Curie', address: 'marie@example.com' } }
    ])
  })

  it('writes an all-day event as midnight to midnight of the day after the last one', () => {
    const input = inputFromForm({ ...form, allDay: true, startDate: '2026-11-06', endDate: '2026-11-08' })
    expect(input.isAllDay).toBe(true)
    expect(input.start).toEqual({ dateTime: '2026-11-06T00:00:00', timeZone: 'Europe/Prague' })
    expect(input.end).toEqual({ dateTime: '2026-11-09T00:00:00', timeZone: 'Europe/Prague' })
  })
})

describe('changeFromForms', () => {
  const original = formFromEvent(planning)

  it('is empty when nothing changed', () => {
    expect(changeFromForms(original, { ...original })).toEqual({})
  })

  it('carries only the changed fields', () => {
    expect(changeFromForms(original, { ...original, subject: 'Review', location: '' })).toEqual({
      subject: 'Review',
      location: { displayName: '' }
    })
  })

  it('sends start and end together when either moves', () => {
    const change = changeFromForms(original, { ...original, endTime: '11:00' })
    expect(Object.keys(change).sort()).toEqual(['end', 'isAllDay', 'start'])
    expect(change.end).toEqual({ dateTime: '2026-11-05T11:00:00', timeZone: 'UTC' })
  })

  it('sends the attendees when they changed', () => {
    const change = changeFromForms(original, { ...original, attendees: [] })
    expect(change).toEqual({ attendees: [] })
  })

  it('sends the categories as a list', () => {
    expect(changeFromForms(original, { ...original, categories: 'Work, Home' })).toEqual({
      categories: ['Work', 'Home']
    })
  })
})

describe('formProblem', () => {
  const ok = formFromEvent(planning)

  it('accepts a good form, with or without a title', () => {
    expect(formProblem(ok)).toBeUndefined()
    expect(formProblem({ ...ok, subject: '' })).toBeUndefined()
  })

  it('refuses an end before the start', () => {
    expect(formProblem({ ...ok, endTime: '08:00' })).toBe('end')
  })

  it('allows a single all-day event', () => {
    expect(formProblem({ ...ok, allDay: true, endDate: ok.startDate })).toBeUndefined()
  })

  it('refuses an all-day event that ends before it starts', () => {
    expect(formProblem({ ...ok, allDay: true, endDate: '2026-11-04' })).toBe('end')
  })

  it('refuses missing dates', () => {
    expect(formProblem({ ...ok, startDate: '' })).toBe('start')
  })
})

describe('attendeeError', () => {
  it('accepts an address and refuses other text and duplicates', () => {
    expect(attendeeError('a@b.example', [])).toBeUndefined()
    expect(attendeeError('not an address', [])).toBe('invalid')
    expect(
      attendeeError('A@B.example', [{ address: 'a@b.example', name: '', type: 'required' }])
    ).toBe('duplicate')
  })
})

describe('parseCategories', () => {
  it('splits, trims and drops empty and repeated names', () => {
    expect(parseCategories(' Work, ,Home,work,Home ')).toEqual(['Work', 'Home'])
  })
})

describe('recurrence in the form', () => {
  const series = eventSeries as unknown as GraphEvent
  const base = formFromEvent(planning)

  it('does not repeat for a single event and a blank form', () => {
    expect(base.recurrence.frequency).toBe('none')
    expect(blankForm({ calendarId: 'c', zone: 'UTC', start: '2026-11-05T09:00:00Z', allDay: false }).recurrence.frequency).toBe('none')
  })

  it('reads the pattern of a series master', () => {
    expect(formFromEvent(series).recurrence).toMatchObject({ frequency: 'weekly', daysOfWeek: ['friday'] })
  })

  it('writes the recurrence of a new event, in the zone of the event', () => {
    const form: EventForm = {
      ...base,
      timeZone: 'Europe/Prague',
      recurrence: { ...base.recurrence, frequency: 'daily', interval: 2 }
    }
    expect(inputFromForm(form).recurrence).toMatchObject({
      pattern: { type: 'daily', interval: 2 },
      range: { startDate: '2026-11-05', recurrenceTimeZone: 'Europe/Prague' }
    })
    expect(inputFromForm(base).recurrence).toBeUndefined()
  })

  it('sends a recurrence change, null to stop repeating, and nothing when it is unchanged', () => {
    const master = formFromEvent(series)
    expect(changeFromForms(master, { ...master })).toEqual({})
    expect(
      changeFromForms(master, { ...master, recurrence: { ...master.recurrence, frequency: 'none' } })
    ).toEqual({ recurrence: null })
    const changed = changeFromForms(master, {
      ...master,
      recurrence: { ...master.recurrence, daysOfWeek: ['monday', 'friday'] }
    })
    expect(changed.recurrence?.pattern.daysOfWeek).toEqual(['monday', 'friday'])
    expect(Object.keys(changed)).toEqual(['recurrence'])
    const added = changeFromForms(base, { ...base, recurrence: { ...base.recurrence, frequency: 'weekly' } })
    expect(added.recurrence?.pattern.type).toBe('weekly')
  })

  it('moving the start of a series does not resend the pattern', () => {
    const master = formFromEvent(series)
    const change = changeFromForms(master, { ...master, startDate: '2026-11-07', endDate: '2026-11-07' })
    expect(change.recurrence).toBeUndefined()
    expect(change.start).toBeDefined()
  })

  it('reports a problem in the pattern', () => {
    expect(formProblem({ ...base, recurrence: { ...base.recurrence, frequency: 'daily', interval: 0 } })).toBe('interval')
  })
})
