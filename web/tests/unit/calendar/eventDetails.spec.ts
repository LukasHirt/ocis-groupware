import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import EventDetails from '../../../src/calendar/EventDetails.vue'
import type { GraphCalendar, GraphEvent } from '../../../src/api/types'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import calendarview from '../../../../internal/graphapi/testdata/contract/calendarview.json'
import eventSeries from '../../../../internal/graphapi/testdata/contract/event-series.json'

vi.mock('@ownclouders/web-pkg', async () => (await import('../mocks/webPkg')).webPkgMock())

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

const personal = calendars.value[0] as unknown as GraphCalendar
const planning = calendarview.value[0] as unknown as GraphEvent
const standup = calendarview.value[1] as unknown as GraphEvent

// A bare event: only what every event has.
const bare: GraphEvent = {
  ...planning,
  body: { contentType: 'text', content: '' },
  categories: [],
  attendees: [],
  location: { displayName: '' },
  isReminderOn: false,
  showAs: '',
  recurrence: null
}

function show(props: Record<string, unknown>) {
  return mount(EventDetails, { props })
}
const has = (wrapper: ReturnType<typeof show>, id: string) => wrapper.find(`[data-testid="${id}"]`).exists()

describe('the details of an event', () => {
  it('says to select an event when there is none', () => {
    const wrapper = show({})
    expect(wrapper.text()).toContain('Select an event to see its details')
    expect(wrapper.find('[data-icon="calendar-event"]').exists()).toBe(true)
  })

  it('shows the title, the day with the times, the zone and the calendar', () => {
    const wrapper = show({ event: planning, calendar: personal })
    expect(wrapper.get('[data-testid="event-title"]').text()).toBe('Planning')
    const when = wrapper.get('[data-testid="event-when"]').text()
    expect(when).toContain('2026')
    expect(when).toContain(' to ')
    expect(wrapper.text()).toContain('UTC')
    expect(wrapper.get('[data-testid="event-calendar-name"]').text()).toBe('Personal')
  })

  it('names an event without a title', () => {
    expect(show({ event: { ...planning, subject: '' }, calendar: personal }).get('[data-testid="event-title"]').text()).toBe(
      '(No title)'
    )
  })

  it('shows the place, reminder, status, categories and notes it has', () => {
    const wrapper = show({ event: planning, calendar: personal })
    expect(wrapper.get('[data-testid="event-place"]').text()).toBe('Room 4')
    expect(wrapper.get('[data-testid="event-reminder-text"]').text()).toBe('15 minutes before')
    expect(wrapper.get('[data-testid="event-status-text"]').text()).toBe('Busy')
    expect(wrapper.get('[data-testid="event-tags"]').text()).toBe('Work')
    expect(wrapper.get('[data-testid="event-notes"]').text()).toBe('Agenda')
  })

  it('leaves out every row that has nothing to say', () => {
    const wrapper = show({ event: bare, calendar: personal })
    for (const id of [
      'event-place',
      'event-reminder-text',
      'event-status-text',
      'event-tags',
      'event-attendees',
      'event-notes',
      'event-repeats'
    ]) {
      expect(has(wrapper, id)).toBe(false)
    }
    // Not even the headings of the empty sections.
    expect(wrapper.text()).not.toContain('Attendees')
    expect(wrapper.text()).not.toContain('Notes')
  })

  it('lists the attendees with their answer', () => {
    const wrapper = show({ event: planning, calendar: personal })
    const rows = wrapper.findAll('[data-testid="event-attendee"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('Marie Curie')
    expect(rows[0].text()).toContain('marie@example.com')
    expect(rows[0].text()).toContain('Accepted')
  })

  it('says "No answer" for an attendee who has not answered', () => {
    const event = {
      ...planning,
      attendees: [{ ...planning.attendees[0], status: { response: 'none' } }]
    }
    expect(show({ event, calendar: personal }).get('[data-testid="event-attendee"]').text()).toContain('No answer')
  })

  it('writes an all-day event as days, and the last day as the day before the end', () => {
    const event = {
      ...bare,
      isAllDay: true,
      start: { dateTime: '2026-11-11T00:00:00.0000000', timeZone: 'UTC' },
      end: { dateTime: '2026-11-14T00:00:00.0000000', timeZone: 'UTC' }
    }
    const when = show({ event, calendar: personal }).get('[data-testid="event-when"]').text()
    expect(when).toContain(' - ')
    expect(when).toContain('11')
    expect(when).toContain('13')
    expect(when).not.toContain('14')
  })

  it('writes the pattern of a series in words, from the series of an occurrence', () => {
    const wrapper = show({ event: standup, calendar: personal, series: eventSeries })
    expect(wrapper.get('[data-testid="event-repeats"]').text()).toMatch(/^Every week on /)
  })

  it('shows no pattern for an occurrence whose series is not known yet', () => {
    expect(has(show({ event: standup, calendar: personal }), 'event-repeats')).toBe(false)
  })

  it('offers Edit and Delete only when the event can be changed', async () => {
    const wrapper = show({ event: planning, calendar: personal, canEdit: true })
    await wrapper.get('[data-testid="event-edit"]').trigger('click')
    await wrapper.get('[data-testid="event-delete"]').trigger('click')
    expect(wrapper.emitted('edit')).toHaveLength(1)
    expect(wrapper.emitted('remove')).toHaveLength(1)
    const readOnly = show({ event: planning, calendar: personal, canEdit: false })
    expect(has(readOnly, 'event-edit')).toBe(false)
    expect(has(readOnly, 'event-delete')).toBe(false)
  })

  it('shows a note with its icon', () => {
    const wrapper = show({ event: planning, calendar: personal, notice: 'Read only.', noticeIcon: 'lock' })
    expect(wrapper.get('[data-testid="event-notice"]').text()).toBe('Read only.')
    expect(wrapper.find('[data-testid="event-notice"] [data-name="lock"]').exists()).toBe(true)
  })

  it('uses a heading below the title of the panel, not a second h1 or h2', () => {
    const wrapper = show({ event: planning, calendar: personal })
    expect(wrapper.find('h1, h2').exists()).toBe(false)
    expect(wrapper.get('h3').text()).toBe('Planning')
  })
})
