import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CalendarPage from '../../../src/pages/CalendarPage.vue'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import calendarview from '../../../../internal/graphapi/testdata/contract/calendarview.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

const hoisted = vi.hoisted(() => ({
  calls: [] as { start: string; end: string; zone?: string }[],
  state: { calendars: [] as Record<string, unknown>[] },
  api: {} as Record<string, ReturnType<typeof vi.fn>>
}))
const calendarViewCalls = hoisted.calls

vi.mock('../../../src/composables/useApi', () => {
  const { api, state, calls } = hoisted
  api.listCalendars = vi.fn(() => Promise.resolve(state.calendars))
  api.calendarView = vi.fn((q: { start: string; end: string; zone?: string }) => {
    calls.push(q)
    return Promise.resolve(calendarview.value)
  })
  api.createCalendar = vi.fn((input: { name: string; hexColor?: string }) =>
    Promise.resolve({
      ...calendars.value[0],
      id: 'bmV3',
      name: input.name,
      hexColor: input.hexColor ?? '',
      isDefaultCalendar: false
    })
  )
  api.updateCalendar = vi.fn((id: string, input: object) =>
    Promise.resolve({ ...calendars.value.find((c) => c.id === id), ...input })
  )
  api.deleteCalendar = vi.fn(() => Promise.resolve())
  api.createEvent = vi.fn(() => Promise.resolve({}))
  api.updateEvent = vi.fn(() => Promise.resolve({}))
  api.deleteEvent = vi.fn(() => Promise.resolve())
  return { useApi: () => ({ client: api, run: (_t: string, f: (c: unknown) => unknown) => f(api) }) }
})

const mountOptions = { global: { mocks: { $gettext: (s: string) => s } } }

async function mountPage() {
  const wrapper = mount(CalendarPage, { ...mountOptions, attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('the calendar page', () => {
  beforeEach(() => {
    // The fixture's events lie in November 2026, so the grid must open there.
    vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-11-05T12:00:00Z') })
    localStorage.clear()
    calendarViewCalls.length = 0
    hoisted.state.calendars = structuredClone(calendars.value)
    for (const f of Object.values(hoisted.api)) f.mockClear()
    document.body.innerHTML = ''
  })

  afterEach(() => vi.useRealTimers())

  it('lists the calendars with a switch each, all on', async () => {
    const wrapper = await mountPage()
    const items = wrapper.findAll('[data-testid="calendar-item"]')
    expect(items.map((i) => i.find('.name').text())).toEqual(['Personal', 'Work', 'Team'])
    for (const toggle of wrapper.findAll<HTMLInputElement>('[data-testid="calendar-toggle"]')) {
      expect(toggle.element.checked).toBe(true)
    }
  })

  it('reads the events for the range the grid shows', async () => {
    await mountPage()
    expect(calendarViewCalls).toHaveLength(1)
    const [{ start, end }] = calendarViewCalls
    expect(new Date(end).getTime()).toBeGreaterThan(new Date(start).getTime())
    // The times are asked for in the zone the page shows them in.
    expect(calendarViewCalls[0].zone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
  })

  it('draws the events of the view', async () => {
    const wrapper = await mountPage()
    const titles = wrapper.findAll('.fc-event-title').map((t) => t.text())
    expect(titles).toContain('Planning')
    expect(titles).toContain('Standup')
  })

  it('hides the events of a calendar that is switched off, and remembers it', async () => {
    const wrapper = await mountPage()
    const personal = calendars.value[0]
    const first = wrapper.find<HTMLInputElement>('[data-testid="calendar-toggle"]')
    await first.setValue(false)
    expect(wrapper.findAll('.fc-event')).toHaveLength(0)
    expect(JSON.parse(localStorage.getItem('groupware.hiddenCalendars') ?? '[]')).toEqual([
      personal.id
    ])

    wrapper.unmount()
    const again = await mountPage()
    expect(again.find<HTMLInputElement>('[data-testid="calendar-toggle"]').element.checked).toBe(false)
  })

  it('offers edit and delete for calendars the user may change, and nothing for others', async () => {
    const wrapper = await mountPage()
    const [personal, work, team] = wrapper.findAll('[data-testid="calendar-item"]')
    expect(personal.find('[data-testid="calendar-edit"]').exists()).toBe(true)
    expect(work.find('[data-testid="calendar-delete"]').exists()).toBe(true)
    expect(team.find('[data-testid="calendar-edit"]').exists()).toBe(false)
    expect(team.find('[data-testid="calendar-delete"]').exists()).toBe(false)
  })

  it('creates a calendar and lists it', async () => {
    const wrapper = await mountPage()
    await wrapper.get('[data-testid="calendar-add"]').trigger('click')
    await wrapper.get('[data-testid="calendar-name"]').setValue('Holidays')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(hoisted.api.createCalendar).toHaveBeenCalledWith({ name: 'Holidays', hexColor: expect.stringMatching(/^#[0-9a-f]{6}$/) })
    const names = wrapper.findAll('[data-testid="calendar-item"] .name').map((n) => n.text())
    expect(names).toContain('Holidays')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('renames and recolours a calendar', async () => {
    const wrapper = await mountPage()
    await wrapper.findAll('[data-testid="calendar-edit"]')[1].trigger('click')
    await wrapper.get('[data-testid="calendar-name"]').setValue('Office')
    await wrapper.get('[data-testid="calendar-color-#c2415d"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(hoisted.api.updateCalendar).toHaveBeenCalledWith(calendars.value[1].id, {
      name: 'Office',
      hexColor: '#c2415d'
    })
    expect(wrapper.findAll('[data-testid="calendar-item"] .name')[1].text()).toBe('Office')
  })

  it('deletes a calendar only after a confirmation, and drops its events', async () => {
    const wrapper = await mountPage()
    expect(wrapper.findAll('.fc-event').length).toBeGreaterThan(0)
    await wrapper.findAll('[data-testid="calendar-delete"]')[0].trigger('click')
    expect(hoisted.api.deleteCalendar).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(hoisted.api.deleteCalendar).not.toHaveBeenCalled()

    await wrapper.findAll('[data-testid="calendar-delete"]')[0].trigger('click')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    expect(hoisted.api.deleteCalendar).toHaveBeenCalledWith(calendars.value[0].id)
    const names = wrapper.findAll('[data-testid="calendar-item"] .name').map((n) => n.text())
    expect(names).not.toContain('Personal')
    expect(wrapper.findAll('.fc-event')).toHaveLength(0)
  })

  describe('events', () => {
    const planning = calendarview.value[0]
    const eventByTitle = (wrapper: ReturnType<typeof mount>, title: string) =>
      wrapper.findAll('.fc-event').find((e) => e.text().includes(title))!

    it('opens an event for editing and saves only what changed', async () => {
      const wrapper = await mountPage()
      await eventByTitle(wrapper, 'Planning').trigger('click')
      const subject = wrapper.get<HTMLInputElement>('[data-testid="event-subject"]')
      expect(subject.element.value).toBe('Planning')
      await subject.setValue('Planning v2')
      calendarViewCalls.length = 0
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(hoisted.api.updateEvent).toHaveBeenCalledWith(
        planning.id,
        { subject: 'Planning v2' },
        planning['@odata.etag']
      )
      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
      // The view is read again so the change shows.
      expect(calendarViewCalls).toHaveLength(1)
    })

    it('deletes an event after a confirmation', async () => {
      const wrapper = await mountPage()
      await eventByTitle(wrapper, 'Planning').trigger('click')
      await wrapper.get('[data-testid="event-delete"]').trigger('click')
      expect(hoisted.api.deleteEvent).not.toHaveBeenCalled()
      await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
      await flushPromises()
      expect(hoisted.api.deleteEvent).toHaveBeenCalledWith(planning.id, planning['@odata.etag'])
      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    })

    it('creates an event from the new event button, in the default calendar', async () => {
      const wrapper = await mountPage()
      await wrapper.get('[data-testid="event-add"]').trigger('click')
      expect(wrapper.get<HTMLSelectElement>('[data-testid="event-calendar"]').element.value).toBe(
        calendars.value[0].id
      )
      await wrapper.get('[data-testid="event-subject"]').setValue('Lunch')
      calendarViewCalls.length = 0
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(hoisted.api.createEvent).toHaveBeenCalledWith(
        expect.objectContaining({ subject: 'Lunch', isAllDay: false }),
        calendars.value[0].id
      )
      expect(calendarViewCalls).toHaveLength(1)
    })

    it('opens a repeating event for reading only', async () => {
      const wrapper = await mountPage()
      await eventByTitle(wrapper, 'Standup').trigger('click')
      expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-testid="dialog-confirm"]').exists()).toBe(false)
    })

    it('keeps the dialog open when the server refuses the change', async () => {
      hoisted.api.updateEvent.mockResolvedValueOnce(undefined as never)
      const wrapper = await mountPage()
      await eventByTitle(wrapper, 'Planning').trigger('click')
      await wrapper.get('[data-testid="event-subject"]').setValue('X')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    })
  })
})
