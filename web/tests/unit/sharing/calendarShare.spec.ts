import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CalendarList from '../../../src/calendar/CalendarList.vue'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import permissions from '../../../../internal/graphapi/testdata/contract/calendar-permissions.json'
import CalendarPage from '../../../src/pages/CalendarPage.vue'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const api = {
  listCalendars: vi.fn(),
  calendarView: vi.fn(),
  listPermissions: vi.fn()
}
vi.mock('../../../src/composables/useApi', () => ({
  useApi: () => ({
    client: api,
    run: (_t: string, f: (c: unknown) => unknown) => Promise.resolve(f(api))
  })
}))

const gettext = (s: string, params: Record<string, string> = {}) =>
  s.replace(/%\{(\w+)\}/g, (_, k: string) => params[k] ?? '')
const global = { mocks: { $gettext: gettext } }

beforeEach(() => {
  localStorage.clear()
  document.body.innerHTML = ''
  api.listCalendars.mockResolvedValue(structuredClone(calendars.value))
  api.calendarView.mockResolvedValue([])
  api.listPermissions.mockResolvedValue(structuredClone(permissions.value))
})

describe('calendar list', () => {
  const [personal, , team] = calendars.value

  it('offers sharing for the owner and not for a calendar shared with the user', () => {
    const wrapper = mount(CalendarList, { props: { calendars: [personal, team], hidden: [] }, global })
    const [mine, shared] = wrapper.findAll('[data-testid="calendar-item"]')
    expect(mine.find('[data-testid="calendar-share"]').exists()).toBe(true)
    expect(shared.find('[data-testid="calendar-share"]').exists()).toBe(false)
  })

  it('says whether a shared calendar is read only or can be edited', () => {
    const editable = { ...team, id: 'x', canEdit: true }
    const wrapper = mount(CalendarList, { props: { calendars: [team, editable], hidden: [] }, global })
    const [ro, rw] = wrapper.findAll('[data-testid="calendar-item"]')
    expect(ro.text()).toContain('Marie Curie, read only')
    expect(rw.text()).toContain('Marie Curie, can edit')
  })

  it('asks the page to share the calendar', async () => {
    const wrapper = mount(CalendarList, { props: { calendars: [personal], hidden: [] }, global })
    await wrapper.get('[data-testid="calendar-share"]').trigger('click')
    expect(wrapper.emitted('share')).toEqual([[personal]])
  })
})

describe('calendar page', () => {
  it('opens the share dialog of the calendar', async () => {
    const wrapper = mount(CalendarPage, { global, attachTo: document.body })
    await flushPromises()
    await wrapper.findAll('[data-testid="calendar-share"]')[1].trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('Share Work')
    expect(api.listPermissions).toHaveBeenCalledWith('calendar', calendars.value[1].id)
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
})
