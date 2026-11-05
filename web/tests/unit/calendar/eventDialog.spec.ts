import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import EventDialog from '../../../src/calendar/EventDialog.vue'
import { blankForm, formFromEvent } from '../../../src/calendar/eventForm'
import { zoneNames } from '../../../src/calendar/zones'
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
    const options = create().findAll('[data-testid="event-calendar"] option')
    expect(options.map((o) => o.text())).toEqual(['Personal', 'Work'])
  })

  it('saves what was entered, into the chosen calendar', async () => {
    const wrapper = create()
    await wrapper.get('[data-testid="event-subject"]').setValue('Lunch')
    await wrapper.get('[data-testid="event-location"]').setValue('Canteen')
    await wrapper.get('[data-testid="event-calendar"]').setValue(editable[1].id)
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
    expect(wrapper.get('[data-testid="event-end-error"]').text()).toContain('after')
  })

  it('hides the times of an all-day event', async () => {
    const wrapper = create()
    expect(wrapper.find('[data-testid="event-start-time"]').exists()).toBe(true)
    await wrapper.get('[data-testid="event-all-day"]').setValue(true)
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
    expect(wrapper.get('[data-testid="attendee-error"]').text()).toContain('valid')

    await wrapper.get('[data-testid="attendee-input"]').setValue('marie@example.com')
    await wrapper.get('[data-testid="attendee-add"]').trigger('click')
    expect(wrapper.findAll('[data-testid="attendee"]').map((a) => a.text())).toEqual([
      expect.stringContaining('marie@example.com')
    ])

    await wrapper.get('[data-testid="attendee-input"]').setValue('MARIE@example.com')
    await wrapper.get('[data-testid="attendee-add"]').trigger('click')
    expect(wrapper.get('[data-testid="attendee-error"]').text()).toContain('already')

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
    const options = create().findAll('[data-testid="event-zone"] option').map((o) => o.element.getAttribute('value'))
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

  it('offers delete', async () => {
    const wrapper = edit()
    await wrapper.get('[data-testid="event-delete"]').trigger('click')
    expect(wrapper.emitted('remove')).toHaveLength(1)
  })

  it('is read-only when the event cannot be changed', () => {
    const wrapper = edit({ readOnly: true, notice: 'Shared read-only.' })
    // A disabled fieldset disables every control inside it.
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="dialog-confirm"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="event-delete"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Shared read-only.')
  })
})

describe('EventDialog, repeating', () => {
  const setRepeat = (wrapper: ReturnType<typeof create>, value: string) =>
    wrapper.get('[data-testid="repeat-frequency"]').setValue(value)
  const saved = (wrapper: ReturnType<typeof create>) =>
    (wrapper.emitted('save') as [[{ input: Record<string, any> }]])[0][0].input

  it('does not repeat by default and shows no pattern fields', () => {
    const wrapper = create()
    expect(wrapper.get<HTMLSelectElement>('[data-testid="repeat-frequency"]').element.value).toBe('none')
    expect(wrapper.find('[data-testid="repeat-interval"]').exists()).toBe(false)
  })

  it('saves a weekly pattern with the days that were ticked', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'weekly')
    // 10 November 2026 is a Tuesday, which starts out ticked.
    expect(wrapper.get<HTMLInputElement>('[data-testid="repeat-day-tuesday"]').element.checked).toBe(true)
    await wrapper.get('[data-testid="repeat-day-friday"]').setValue(true)
    await wrapper.get('[data-testid="repeat-interval"]').setValue('2')
    await wrapper.get('form').trigger('submit')
    expect(saved(wrapper).recurrence).toMatchObject({
      pattern: { type: 'weekly', interval: 2, daysOfWeek: ['tuesday', 'friday'] },
      range: { type: 'noEnd', startDate: '2026-11-10', recurrenceTimeZone: 'UTC' }
    })
  })

  it('offers a day of the month or a weekday for monthly patterns', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'monthly')
    expect(wrapper.get<HTMLInputElement>('[data-testid="repeat-day-of-month"]').element.value).toBe('10')
    await wrapper.get('[data-testid="repeat-monthly-by"]').setValue('weekday')
    await wrapper.get('[data-testid="repeat-index"]').setValue('second')
    await wrapper.get('form').trigger('submit')
    expect(saved(wrapper).recurrence.pattern).toMatchObject({
      type: 'relativeMonthly',
      index: 'second',
      daysOfWeek: ['tuesday']
    })
  })

  it('asks for the month of a yearly pattern', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'yearly')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="repeat-month"]').element.value).toBe('11')
  })

  it('ends after a number of times or on a date', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'daily')
    await wrapper.get('[data-testid="repeat-end"]').setValue('count')
    await wrapper.get('[data-testid="repeat-count"]').setValue('4')
    await wrapper.get('form').trigger('submit')
    expect(saved(wrapper).recurrence.range).toMatchObject({ type: 'numbered', numberOfOccurrences: 4 })

    const other = create()
    await setRepeat(other, 'daily')
    await other.get('[data-testid="repeat-end"]').setValue('date')
    await other.get('[data-testid="repeat-end-date"]').setValue('2026-12-24')
    await other.get('form').trigger('submit')
    expect(saved(other).recurrence.range).toMatchObject({ type: 'endDate', endDate: '2026-12-24' })
  })

  it('refuses a bad pattern and names the field', async () => {
    const wrapper = create()
    await setRepeat(wrapper, 'weekly')
    await wrapper.get('[data-testid="repeat-day-tuesday"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('[data-testid="repeat-error"]').text()).toContain('day')

    await wrapper.get('[data-testid="repeat-day-tuesday"]').setValue(true)
    await wrapper.get('[data-testid="repeat-interval"]').setValue('0')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[data-testid="repeat-error"]').text()).toContain('1 and 99')
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
    expect(wrapper.get<HTMLSelectElement>('[data-testid="repeat-frequency"]').element.value).toBe('weekly')
    expect(wrapper.get<HTMLInputElement>('[data-testid="repeat-day-friday"]').element.checked).toBe(true)
    await wrapper.get('[data-testid="repeat-end"]').setValue('never')
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
    await setRepeat(wrapper as never, 'none')
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
