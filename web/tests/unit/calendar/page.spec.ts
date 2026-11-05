import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CalendarPage from '../../../src/pages/CalendarPage.vue'
import { choose } from '../helpers/select'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import importReport from '../../../../internal/graphapi/testdata/contract/import-report.json'
import eventSeries from '../../../../internal/graphapi/testdata/contract/event-series.json'
import calendarview from '../../../../internal/graphapi/testdata/contract/calendarview.json'

// The design system and the host's services are not here; see mocks/webPkg.
vi.mock('@ownclouders/web-pkg', async () => (await import('../mocks/webPkg')).webPkgMock())

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

// The signed-in user comes from the host's store, which is not here.
const saved = vi.hoisted(() => ({ calls: [] as { name: string; blob: Blob }[] }))
vi.mock('../../../src/components/download', async (orig) => ({
  ...(await orig<typeof import('../../../src/components/download')>()),
  saveBlob: (blob: Blob, name: string) => saved.calls.push({ blob, name })
}))
vi.mock('../../../src/composables/useUserKey', () => ({ useUserKey: () => '' }))

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
  api.importCalendar = vi.fn(() => Promise.resolve(importReport))
  api.exportCalendar = vi.fn(() => Promise.resolve(new Blob(['BEGIN:VCALENDAR'])))
  api.getEvent = vi.fn(() => Promise.resolve(eventSeries))
  api.createEvent = vi.fn(() => Promise.resolve({}))
  api.updateEvent = vi.fn(() => Promise.resolve({}))
  api.deleteEvent = vi.fn(() => Promise.resolve())
  return { useApi: () => ({ client: api, run: (_t: string, f: (c: unknown) => unknown) => f(api) }) }
})

const interpolate = (s: string, vars?: Record<string, string>) =>
  s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
const mountOptions = { global: { mocks: { $gettext: interpolate } } }

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
    saved.calls.length = 0
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

    describe('a repeating event', () => {
      const occurrence = calendarview.value[1]

      it('asks whether to change this event or all of them, after reading the series', async () => {
        const wrapper = await mountPage()
        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        expect(hoisted.api.getEvent).toHaveBeenCalledWith(occurrence.seriesMasterId, expect.any(String))
        expect(wrapper.find('[data-testid="scope-this"]').exists()).toBe(true)
        expect(wrapper.find('[data-testid="scope-all"]').exists()).toBe(true)
        await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
        expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
      })

      it('changes one event by the id of the occurrence, without a pattern', async () => {
        const wrapper = await mountPage()
        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        await wrapper.get('[data-testid="scope-this"]').trigger('click')
        expect(wrapper.find('[data-testid="repeat-frequency"]').exists()).toBe(false)
        await wrapper.get('[data-testid="event-subject"]').setValue('Standup, moved')
        await wrapper.get('form').trigger('submit')
        await flushPromises()
        expect(hoisted.api.updateEvent).toHaveBeenCalledWith(
          occurrence.id,
          { subject: 'Standup, moved' },
          occurrence['@odata.etag']
        )
      })

      it('changes all events by the id of the series, with its pattern', async () => {
        const wrapper = await mountPage()
        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        await wrapper.get('[data-testid="scope-all"]').trigger('click')
        expect(wrapper.get<HTMLSelectElement>('[data-testid="repeat-frequency"]').element.value).toBe('weekly')
        await wrapper.get('[data-testid="repeat-day-monday"]').setValue(true)
        await wrapper.get('form').trigger('submit')
        await flushPromises()
        const [id, input, etag] = hoisted.api.updateEvent.mock.calls[0]
        expect(id).toBe(eventSeries.id)
        expect(etag).toBe(eventSeries['@odata.etag'])
        expect(input.recurrence.pattern.daysOfWeek).toEqual(['monday', 'friday'])
      })

      it('deletes one occurrence, or the whole series, after a confirmation', async () => {
        const wrapper = await mountPage()
        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        await wrapper.get('[data-testid="scope-this"]').trigger('click')
        await wrapper.get('[data-testid="event-delete"]').trigger('click')
        expect(wrapper.text()).toContain('Delete this event')
        await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
        await flushPromises()
        expect(hoisted.api.deleteEvent).toHaveBeenLastCalledWith(occurrence.id, occurrence['@odata.etag'])

        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        await wrapper.get('[data-testid="scope-all"]').trigger('click')
        await wrapper.get('[data-testid="event-delete"]').trigger('click')
        expect(wrapper.text()).toContain('Delete all events')
        await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
        await flushPromises()
        expect(hoisted.api.deleteEvent).toHaveBeenLastCalledWith(eventSeries.id, eventSeries['@odata.etag'])
      })

      it('is read-only when the series has a rule the editor cannot show', async () => {
        hoisted.api.getEvent.mockResolvedValueOnce({
          ...eventSeries,
          recurrence: null,
          'groupware.recurrenceEditable': false
        })
        const wrapper = await mountPage()
        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        expect(wrapper.find('[data-testid="scope-this"]').exists()).toBe(false)
        expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
        expect(wrapper.get('[data-testid="event-notice"]').text()).toContain('cannot')
        expect(wrapper.find('[data-testid="dialog-confirm"]').exists()).toBe(false)
      })

      it('says nothing more when the series cannot be read', async () => {
        hoisted.api.getEvent.mockResolvedValueOnce(undefined as never)
        const wrapper = await mountPage()
        await eventByTitle(wrapper, 'Standup').trigger('click')
        await flushPromises()
        expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
      })
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

  describe('settings', () => {
    it('shows weeks from the chosen day, with week numbers', async () => {
      localStorage.setItem(
        'groupware.calendarSettings.',
        JSON.stringify({ timeZone: '', firstDay: 0, weekNumbers: true })
      )
      const wrapper = await mountPage()
      expect(wrapper.findAll('.fc-col-header-cell')[0].text()).toContain('Sun')
      expect(wrapper.find('.fc-daygrid-week-number').exists()).toBe(true)
    })

    it('asks for the times in the chosen zone', async () => {
      localStorage.setItem(
        'groupware.calendarSettings.',
        JSON.stringify({ timeZone: 'Asia/Tokyo', firstDay: 1, weekNumbers: false })
      )
      await mountPage()
      expect(calendarViewCalls[0].zone).toBe('Asia/Tokyo')
    })

    it('opens the settings, applies a change at once and keeps it', async () => {
      const wrapper = await mountPage()
      expect(wrapper.findAll('.fc-col-header-cell')[0].text()).toContain('Mon')
      await wrapper.get('[data-testid="settings-open"]').trigger('click')
      await choose(wrapper, 'settings-first-day', 'Sunday')
      await choose(wrapper, 'settings-zone', 'Asia/Tokyo')
      calendarViewCalls.length = 0
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
      expect(wrapper.findAll('.fc-col-header-cell')[0].text()).toContain('Sun')
      // The events are read again in the new zone.
      expect(calendarViewCalls.at(-1)?.zone).toBe('Asia/Tokyo')
      expect(JSON.parse(localStorage.getItem('groupware.calendarSettings.') ?? '{}')).toMatchObject({
        timeZone: 'Asia/Tokyo',
        firstDay: 0
      })
    })

    it('uses the chosen zone for a new event', async () => {
      localStorage.setItem(
        'groupware.calendarSettings.',
        JSON.stringify({ timeZone: 'Asia/Tokyo', firstDay: 1, weekNumbers: false })
      )
      const wrapper = await mountPage()
      await wrapper.get('[data-testid="event-add"]').trigger('click')
      expect(wrapper.get<HTMLSelectElement>('[data-testid="event-zone"]').element.value).toBe('Asia/Tokyo')
    })
  })

  it('offers edit and delete only to the owner, not to a recipient who can edit events', async () => {
    hoisted.state.calendars[2] = { ...hoisted.state.calendars[2], canEdit: true, canShare: false, isRemovable: true }
    const wrapper = await mountPage()
    const team = wrapper.findAll('[data-testid="calendar-item"]')[2]
    expect(team.find('[data-testid="calendar-edit"]').exists()).toBe(false)
    expect(team.find('[data-testid="calendar-delete"]').exists()).toBe(false)
    expect(team.find('[data-testid="calendar-import"]').exists()).toBe(true)
  })

  describe('import and export', () => {
    function choose(wrapper: ReturnType<typeof mount>, file: File) {
      const input = wrapper.get<HTMLInputElement>('[data-testid="calendar-import-file"]')
      Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
      return input.trigger('change')
    }

    it('offers export for every calendar and import only where the user can write', async () => {
      const wrapper = await mountPage()
      const [personal, work, team] = wrapper.findAll('[data-testid="calendar-item"]')
      expect(personal.find('[data-testid="calendar-export"]').exists()).toBe(true)
      expect(personal.find('[data-testid="calendar-import"]').exists()).toBe(true)
      expect(work.find('[data-testid="calendar-import"]').exists()).toBe(true)
      expect(team.find('[data-testid="calendar-export"]').exists()).toBe(true)
      expect(team.find('[data-testid="calendar-import"]').exists()).toBe(false)
    })

    it('imports a file into the chosen calendar, shows the report and reads the events again', async () => {
      const wrapper = await mountPage()
      await wrapper.findAll('[data-testid="calendar-import"]')[1].trigger('click')
      const file = new File(['BEGIN:VCALENDAR'], 'holidays.ics', { type: 'text/calendar' })
      calendarViewCalls.length = 0
      await choose(wrapper, file)
      await flushPromises()
      expect(hoisted.api.importCalendar).toHaveBeenCalledWith(calendars.value[1].id, file)
      expect(wrapper.get('[data-testid="import-summary"]').text()).toContain('2 added')
      expect(wrapper.findAll('[data-testid="import-failure"]')).toHaveLength(1)
      expect(calendarViewCalls).toHaveLength(1)
      await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    })

    it('shows no report when the import was refused', async () => {
      hoisted.api.importCalendar.mockResolvedValueOnce(undefined as never)
      const wrapper = await mountPage()
      await wrapper.findAll('[data-testid="calendar-import"]')[0].trigger('click')
      await choose(wrapper, new File(['x'], 'x.ics'))
      await flushPromises()
      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    })

    it('downloads a calendar under its name', async () => {
      const wrapper = await mountPage()
      await wrapper.findAll('[data-testid="calendar-export"]')[1].trigger('click')
      await flushPromises()
      expect(hoisted.api.exportCalendar).toHaveBeenCalledWith(calendars.value[1].id)
      expect(saved.calls.map((c) => c.name)).toEqual(['Work.ics'])
    })
  })
})
