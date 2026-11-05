import { describe, expect, it } from 'vitest'
import { calendarIdOf, toCalendarEvent } from '../../../src/calendar/mapping'
import { calendarColor, palette, readableTextColor } from '../../../src/calendar/colors'
import type { GraphCalendar, GraphEvent } from '../../../src/api/types'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import calendarview from '../../../../internal/graphapi/testdata/contract/calendarview.json'
import eventZone from '../../../../internal/graphapi/testdata/contract/event-zone.json'

const byId = new Map((calendars.value as GraphCalendar[]).map((c) => [c.id, c]))
const view = calendarview.value as GraphEvent[]

describe('calendarIdOf', () => {
  it('takes the calendar part of an event id and of an occurrence id', () => {
    expect(calendarIdOf('cGVyc29uYWwtY2Fs.bWVldGluZy5pY3M')).toBe('cGVyc29uYWwtY2Fs')
    expect(calendarIdOf('cGVyc29uYWwtY2Fs.c2VyaWVzLmljcw.20261106T090000Z')).toBe('cGVyc29uYWwtY2Fs')
  })
})

describe('toCalendarEvent', () => {
  it('turns a timed event in UTC into instants', () => {
    const fc = toCalendarEvent(view[0], byId, 'Untitled')
    expect(fc.id).toBe(view[0].id)
    expect(fc.title).toBe('Planning')
    expect(fc.start).toBe('2026-11-05T09:00:00.000Z')
    expect(fc.end).toBe('2026-11-05T10:30:00.000Z')
    expect(fc.allDay).toBe(false)
  })

  it('converts a local time in a named zone to the instant', () => {
    const e: GraphEvent = {
      ...(eventZone as GraphEvent),
      start: { dateTime: '2026-07-01T09:00:00.0000000', timeZone: 'Europe/Prague' },
      end: { dateTime: '2026-07-01T10:00:00.0000000', timeZone: 'Europe/Prague' }
    }
    const fc = toCalendarEvent(e, byId, 'Untitled')
    expect(fc.start).toBe('2026-07-01T07:00:00.000Z')
    expect(fc.end).toBe('2026-07-01T08:00:00.000Z')
  })

  it('keeps all-day events as dates', () => {
    const e: GraphEvent = {
      ...view[0],
      isAllDay: true,
      start: { dateTime: '2026-11-05T00:00:00.0000000', timeZone: 'UTC' },
      end: { dateTime: '2026-11-06T00:00:00.0000000', timeZone: 'UTC' }
    }
    const fc = toCalendarEvent(e, byId, 'Untitled')
    expect(fc.allDay).toBe(true)
    expect(fc.start).toBe('2026-11-05')
    expect(fc.end).toBe('2026-11-06')
  })

  it('falls back to UTC for a zone it does not know', () => {
    const e: GraphEvent = {
      ...view[0],
      start: { dateTime: '2026-11-05T09:00:00.0000000', timeZone: 'No/Such_Zone' },
      end: { dateTime: '2026-11-05T10:00:00.0000000', timeZone: 'No/Such_Zone' }
    }
    expect(toCalendarEvent(e, byId, 'Untitled').start).toBe('2026-11-05T09:00:00.000Z')
  })

  it('names an event without a subject', () => {
    expect(toCalendarEvent({ ...view[0], subject: '' }, byId, 'Untitled').title).toBe('Untitled')
  })

  it('colours the event like its calendar and records the calendar', () => {
    const calendar = byId.get(calendarIdOf(view[0].id))!
    const fc = toCalendarEvent(view[0], byId, 'Untitled')
    expect(fc.backgroundColor).toBe(calendarColor(calendar))
    expect(fc.extendedProps?.calendarId).toBe(calendar.id)
    expect(fc.extendedProps?.calendarName).toBe(calendar.name)
    expect(fc.extendedProps?.graph).toEqual(view[0])
  })
})

describe('calendarColor', () => {
  const base = (calendars.value as GraphCalendar[])[0]

  it('uses the colour of the calendar when it has one', () => {
    expect(calendarColor({ ...base, hexColor: '#aa3300' })).toBe('#aa3300')
  })

  it('gives a calendar without a colour the same one every time', () => {
    const a = calendarColor({ ...base, hexColor: '' })
    expect(a).toMatch(/^#[0-9a-f]{6}$/)
    expect(calendarColor({ ...base, hexColor: '' })).toBe(a)
  })

  it('spreads different calendars over the palette', () => {
    const seen = new Set(
      ['a', 'b', 'c', 'd', 'e', 'f'].map((id) => calendarColor({ ...base, id, hexColor: '' }))
    )
    expect(seen.size).toBeGreaterThan(2)
  })
})

describe('readableTextColor', () => {
  it('picks white on dark and black on light', () => {
    expect(readableTextColor('#112233')).toBe('#ffffff')
    expect(readableTextColor('#f5f5a0')).toBe('#000000')
  })

  it('keeps a contrast of 4.5 or more on every palette colour', () => {
    const luminance = (hex: string) => {
      const n = parseInt(hex.slice(1), 16)
      const c = (v: number) => {
        const s = v / 255
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
      }
      return 0.2126 * c((n >> 16) & 255) + 0.7152 * c((n >> 8) & 255) + 0.0722 * c(n & 255)
    }
    for (const colour of palette) {
      const a = luminance(colour)
      const b = luminance(readableTextColor(colour))
      expect((Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05), colour).toBeGreaterThanOrEqual(4.5)
    }
  })
})
