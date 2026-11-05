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

const calendarViewCalls: { start: string; end: string }[] = []
vi.mock('../../../src/composables/useApi', () => {
  const client = {
    listCalendars: () => Promise.resolve(calendars.value),
    calendarView: (q: { start: string; end: string }) => {
      calendarViewCalls.push(q)
      return Promise.resolve(calendarview.value)
    }
  }
  return { useApi: () => ({ client, run: (_t: string, f: (c: unknown) => unknown) => f(client) }) }
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
})
