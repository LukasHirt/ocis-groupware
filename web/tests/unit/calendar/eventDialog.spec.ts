import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import EventDialog from '../../../src/calendar/EventDialog.vue'
import { blankForm, formFromEvent } from '../../../src/calendar/eventForm'
import { zoneNames } from '../../../src/calendar/zones'
import type { GraphCalendar, GraphEvent } from '../../../src/api/types'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
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
