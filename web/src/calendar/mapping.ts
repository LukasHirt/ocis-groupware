import { DateTime } from 'luxon'
import type { EventInput } from '@fullcalendar/core'
import type { DateTimeTimeZone, GraphCalendar, GraphEvent } from '../api/types'
import { calendarColor, readableTextColor } from './colors'

// Event and occurrence ids start with the id of their calendar.
export function calendarIdOf(eventId: string): string {
  return eventId.split('.')[0]
}

// instant reads a Graph local time in its zone. A zone luxon does not know is
// read as UTC rather than dropping the event.
function instant(value: DateTimeTimeZone): string {
  let parsed = DateTime.fromISO(value.dateTime, { zone: value.timeZone })
  if (!parsed.isValid) {
    parsed = DateTime.fromISO(value.dateTime, { zone: 'utc' })
  }
  return parsed.toUTC().toISO() ?? value.dateTime
}

const day = (value: DateTimeTimeZone) => value.dateTime.slice(0, 10)

// toCalendarEvent turns an event of the calendar view into what FullCalendar
// draws. The Graph event stays in extendedProps for the dialogs.
export function toCalendarEvent(
  event: GraphEvent,
  calendars: Map<string, GraphCalendar>,
  untitled: string
): EventInput {
  const calendarId = calendarIdOf(event.id)
  const calendar = calendars.get(calendarId)
  const color = calendar ? calendarColor(calendar) : calendarColor({ id: calendarId, hexColor: '' })
  return {
    id: event.id,
    title: event.subject || untitled,
    start: event.isAllDay ? day(event.start) : instant(event.start),
    end: event.isAllDay ? day(event.end) : instant(event.end),
    allDay: event.isAllDay,
    backgroundColor: color,
    borderColor: color,
    textColor: readableTextColor(color),
    extendedProps: { calendarId, calendarName: calendar?.name ?? '', graph: event }
  }
}
