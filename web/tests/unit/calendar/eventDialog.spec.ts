import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { Info } from 'luxon'
import EventDialog from '../../../src/calendar/EventDialog.vue'
import { blankForm, formFromEvent } from '../../../src/calendar/eventForm'
import { zoneNames } from '../../../src/calendar/zones'
import { choose, optionLabels, selectedLabel } from '../helpers/select'
import type { GraphCalendar, GraphEvent } from '../../../src/api/types'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import eventSeries from '../../../../internal/graphapi/testdata/contract/event-series.json'
import calendarview from '../../../../internal/graphapi/testdata/contract/calendarview.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

const editable = (calendars.value as GraphCalendar[]).filter((c) => c.canEdit)
const planning = calendarview.value[0] as GraphEvent

function create() {
  return mount(EventDialog, {
    props: {
      title: 'New event',
      calendars: editable,
      form: blankForm({
        calendarId: editable[0].id,
        zone: 'UTC',
        start: '2026-11-10T09:00:00.000Z',
        end: '2026-11-10T09:30:00.000Z',
        allDay: false
      })
    }
  })
}

function edit(props: Record<string, unknown> = {}) {
  return mount(EventDialog, {
    props: { title: 'Edit event', calendars: editable, form: formFromEvent(planning), existing: true, ...props }
  })
}

describe('EventDialog, creating', () => {
  it('lets the user pick one of the calendars they can write to', () => {
    expect(optionLabels(create(), 'event-calendar')).toEqual(['Personal', 'Work'])
  })

  it('saves what was entered, into the chosen calendar', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="event-subject"]').setValue('Lunch')
    await wrapper.get('[data-testid="event-location"]').setValue('Canteen')
    await choose(wrapper, 'event-calendar', 'Work')
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ calendarId: string; input: Record<string, unknown> }]]
    expect(saved.calendarId).toBe(editable[1].id)
    expect(saved.input).toMatchObject({
      subject: 'Lunch',
      location: { displayName: 'Canteen' },
      start: { dateTime: '2026-11-10T09:00:00', timeZone: 'UTC' },
      end: { dateTime: '2026-11-10T09:30:00', timeZone: 'UTC' },
      isAllDay: false,
      showAs: 'busy'
    })
  })

  it('refuses an end before the start and says which field', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="event-end-time"]').setValue('08:00')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    // The message belongs to the end time, which it names for a screen reader.
    expect(wrapper.get('#gw-event-end-time-message').text()).toContain('after')
    expect(wrapper.get('[data-testid="event-end-time"]').attributes('aria-describedby')).toBe(
      'gw-event-end-time-message'
    )
  })

  it('puts the end problem of an all-day event on its end date', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="event-all-day"] input').setValue(true)
    await wrapper.get('[data-testid="event-end-date"]').setValue('2026-11-01')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('#gw-event-end-date-message').text()).toContain('after')
  })

  it('asks for a start before it saves', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="event-start-date"]').setValue('')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('#gw-event-start-date-message').text()).toContain('start')
  })

  it('hides the times of an all-day event', async () => {
    const wrapper = create()
    expect(wrapper.find('[data-testid="event-start-time"]').exists()).toBe(true)
    await wrapper.get('[data-testid="event-all-day"] input').setValue(true)
    expect(wrapper.find('[data-testid="event-start-time"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ input: { isAllDay: boolean; end: { dateTime: string } } }]]
    expect(saved.input.isAllDay).toBe(true)
    expect(saved.input.end.dateTime).toBe('2026-11-11T00:00:00')
  })

  it('adds and removes attendees and refuses a bad address', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="attendee-input"]').setValue('nonsense')
    await wrapper.get('[data-testid="attendee-add"]').trigger('click')
    expect(wrapper.get('#gw-attendee-input-message').text()).toContain('valid')

    await wrapper.get('[data-testid="attendee-input"]').setValue('marie@example.com')
    await wrapper.get('[data-testid="attendee-add"]').trigger('click')
    expect(wrapper.findAll('[data-testid="attendee"]').map((a) => a.text())).toEqual([
      expect.stringContaining('marie@example.com')
    ])

    await wrapper.get('[data-testid="attendee-input"]').setValue('MARIE@example.com')
    await wrapper.get('[data-testid="attendee-add"]').trigger('click')
    expect(wrapper.get('#gw-attendee-input-message').text()).toContain('already')

    // A half-typed address stops the save; with the field cleared it goes through.
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    await wrapper.get('[data-testid="attendee-input"]').setValue('')
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ input: { attendees: unknown[] } }]]
    expect(saved.input.attendees).toEqual([
      { type: 'required', emailAddress: { name: '', address: 'marie@example.com' } }
    ])

    await wrapper.get('[data-testid="attendee-remove"]').trigger('click')
    expect(wrapper.findAll('[data-testid="attendee"]')).toHaveLength(0)
  })

  it('takes an address typed into the field when the form is saved', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="attendee-input"]').setValue('marie@example.com')
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ input: { attendees: unknown[] } }]]
    expect(saved.input.attendees).toHaveLength(1)
  })

  it('offers UTC and the zone of the event in the zone list', () => {
    const options = optionLabels(create(), 'event-zone')
    expect(options).toContain('UTC')
    expect(options.length).toBeGreaterThan(50)
    expect(zoneNames('Odd/Zone')).toContain('Odd/Zone')
  })
})

describe('EventDialog, editing', () => {
  it('starts from the event', () => {
    const wrapper = edit()
    expect(wrapper.get<HTMLInputElement>('[data-testid="event-subject"]').element.value).toBe('Planning')
    expect(wrapper.get<HTMLInputElement>('[data-testid="event-start-time"]').element.value).toBe('09:00')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="event-notes"]').element.value).toBe('Agenda')
    expect(wrapper.get<HTMLInputElement>('[data-testid="event-categories"]').element.value).toBe('Work')
    expect(wrapper.find('[data-testid="event-calendar"]').exists()).toBe(false)
  })

  it('saves only the changes', async () => {
    const wrapper = edit()
    await wrapper.get('[data-testid="event-subject"]').setValue('Planning v2')
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ input: object }]]
    expect(saved.input).toEqual({ subject: 'Planning v2' })
  })

  it('closes without a request when nothing changed', async () => {
    const wrapper = edit()
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('leaves delete to the details of the event', () => {
    const wrapper = edit()
    expect(wrapper.find('[data-testid="event-delete"]').exists()).toBe(false)
  })

  it('has no disabled form: it is only ever shown for an event that can be changed', () => {
    const wrapper = edit()
    expect(wrapper.find('fieldset').exists()).toBe(false)
    expect(wrapper.find('[disabled]').exists()).toBe(false)
  })

  it('closes on Escape', async () => {
    const wrapper = edit()
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})

describe('EventDialog, repeating', () => {
  const setRepeat = (wrapper: ReturnType<typeof create>, label: string) =>
    choose(wrapper, 'repeat-frequency', label)
  const pressed = (wrapper: ReturnType<typeof create>, day: string) =>
    wrapper.get(`[data-testid="repeat-day-${day}"]`).attributes('aria-pressed')
  const saved = (wrapper: ReturnType<typeof create>) =>
    (wrapper.emitted('save') as [[{ input: Record<string, any> }]])[0][0].input

  it('does not repeat by default and shows no pattern fields', () => {
    const wrapper = create()
    expect(selectedLabel(wrapper, 'repeat-frequency')).toBe('Does not repeat')
    expect(wrapper.find('[data-testid="repeat-interval"]').exists()).toBe(false)
  })

  it('saves a weekly pattern with the days that were ticked', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Weekly')
    // 10 November 2026 is a Tuesday, which starts out on.
    expect(pressed(wrapper, 'tuesday')).toBe('true')
    expect(pressed(wrapper, 'friday')).toBe('false')
    await wrapper.get('[data-testid="repeat-day-friday"]').trigger('click')
    expect(pressed(wrapper, 'friday')).toBe('true')
    await wrapper.get('[data-testid="repeat-interval"]').setValue('2')
    await wrapper.get('form').trigger('submit')
    expect(saved(wrapper).recurrence).toMatchObject({
      pattern: { type: 'weekly', interval: 2, daysOfWeek: ['tuesday', 'friday'] },
      range: { type: 'noEnd', startDate: '2026-11-10', recurrenceTimeZone: 'UTC' }
    })
  })

  it('offers a day of the month or a weekday for monthly patterns', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Monthly')
    expect(wrapper.get<HTMLInputElement>('[data-testid="repeat-day-of-month"]').element.value).toBe('10')
    await choose(wrapper, 'repeat-monthly-by', 'On a weekday')
    await choose(wrapper, 'repeat-index', 'Second')
    await wrapper.get('form').trigger('submit')
    expect(saved(wrapper).recurrence.pattern).toMatchObject({
      type: 'relativeMonthly',
      index: 'second',
      daysOfWeek: ['tuesday']
    })
  })

  it('asks for the month of a yearly pattern', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Yearly')
    expect(selectedLabel(wrapper, 'repeat-month')).toBe('November')
  })

  it('ends after a number of times or on a date', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Daily')
    await choose(wrapper, 'repeat-end', 'After a number of times')
    await wrapper.get('[data-testid="repeat-count"]').setValue('4')
    await wrapper.get('form').trigger('submit')
    expect(saved(wrapper).recurrence.range).toMatchObject({ type: 'numbered', numberOfOccurrences: 4 })

    const other = create()
    await setRepeat(other, 'Daily')
    await choose(other, 'repeat-end', 'On a date')
    await other.get('[data-testid="repeat-end-date"]').setValue('2026-12-24')
    await other.get('form').trigger('submit')
    expect(saved(other).recurrence.range).toMatchObject({ type: 'endDate', endDate: '2026-12-24' })
  })

  it('refuses a bad pattern and names the field', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Weekly')
    await wrapper.get('[data-testid="repeat-day-tuesday"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('#gw-repeat-days-message').text()).toContain('day')

    await wrapper.get('[data-testid="repeat-day-tuesday"]').trigger('click')
    await wrapper.get('[data-testid="repeat-interval"]').setValue('0')
    await wrapper.get('form').trigger('submit')
    // The message sits on the field it is about.
    expect(wrapper.get('#gw-repeat-interval-message').text()).toContain('1 and 99')
    expect(wrapper.find('#gw-repeat-days-message').exists()).toBe(false)
  })

  it('puts each repeat problem on its own field', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Daily')
    await choose(wrapper, 'repeat-end', 'After a number of times')
    await wrapper.get('[data-testid="repeat-count"]').setValue('0')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('#gw-repeat-count-message').text()).toContain('1 and 999')

    await choose(wrapper, 'repeat-end', 'On a date')
    await wrapper.get('[data-testid="repeat-end-date"]').setValue('2026-01-01')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('#gw-repeat-end-date-message').text()).toContain('last date')

    await setRepeat(wrapper, 'Monthly')
    await wrapper.get('[data-testid="repeat-day-of-month"]').setValue('40')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('#gw-repeat-day-of-month-message').text()).toContain('between 1 and 31')
  })

  it('offers the weekdays as a group of toggle buttons with their full names', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'Weekly')
    const group = wrapper.get('[role="group"]')
    const buttons = group.findAll('button')
    expect(buttons).toHaveLength(7)
    // The names come from the locale of the browser.
    expect(buttons[0].attributes('aria-label')).toBe(Info.weekdays('long')[0])
    expect(buttons.every((b) => b.attributes('aria-pressed') !== undefined)).toBe(true)
    expect(group.attributes('aria-labelledby')).toBeTruthy()
  })

  it('starts from the pattern of a series and saves only a change to it', async () => {
    const wrapper = mount(EventDialog, {
      props: {
        title: 'All events',
        calendars: editable,
        existing: true,
        form: formFromEvent(eventSeries as unknown as GraphEvent)
      }
    })
    expect(selectedLabel(wrapper, 'repeat-frequency')).toBe('Weekly')
    expect(wrapper.get('[data-testid="repeat-day-friday"]').attributes('aria-pressed')).toBe('true')
    await choose(wrapper, 'repeat-end', 'Never')
    await wrapper.get('form').trigger('submit')
    const input = (wrapper.emitted('save') as [[{ input: Record<string, any> }]])[0][0].input
    expect(Object.keys(input)).toEqual(['recurrence'])
    expect(input.recurrence.range.type).toBe('noEnd')
  })

  it('can stop a series from repeating', async () => {
    const wrapper = mount(EventDialog, {
      props: {
        title: 'All events',
        calendars: editable,
        existing: true,
        form: formFromEvent(eventSeries as unknown as GraphEvent)
      }
    })
    await setRepeat(wrapper as never, 'Does not repeat')
    await wrapper.get('form').trigger('submit')
    expect((wrapper.emitted('save') as [[{ input: object }]])[0][0].input).toEqual({ recurrence: null })
  })

  it('has no repeat section for one occurrence of a series', () => {
    const wrapper = mount(EventDialog, {
      props: {
        title: 'This event',
        calendars: editable,
        existing: true,
        hideRecurrence: true,
        form: formFromEvent(planning)
      }
    })
    expect(wrapper.find('[data-testid="repeat-frequency"]').exists()).toBe(false)
  })
})

describe('EventDialog, reminders', () => {
  it('has no reminder for a new event, and saves one that was chosen', async () => {
    const wrapper = create()
    expect(selectedLabel(wrapper, 'event-reminder')).toBe('No reminder')
    await choose(wrapper, 'event-reminder', '30 minutes before')
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ input: Record<string, unknown> }]]
    expect(saved.input).toMatchObject({ isReminderOn: true, reminderMinutesBeforeStart: 30 })
  })

  it('shows the reminder an event has and turns it off', async () => {
    const wrapper = edit()
    expect(selectedLabel(wrapper, 'event-reminder')).toBe('15 minutes before')
    expect(optionLabels(wrapper, 'event-reminder')[0]).toBe('No reminder')
    await choose(wrapper, 'event-reminder', 'No reminder')
    await wrapper.get('form').trigger('submit')
    const [[saved]] = wrapper.emitted('save') as [[{ input: object }]]
    expect(saved.input).toEqual({ isReminderOn: false })
  })
})
