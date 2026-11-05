import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CalendarList from '../../../src/calendar/CalendarList.vue'
import calendars from '../../../../internal/graphapi/testdata/contract/calendars.json'
import type { GraphCalendar } from '../../../src/api/types'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

const [personal, work, team] = calendars.value as unknown as GraphCalendar[]

function list(items: GraphCalendar[], hidden: string[] = []) {
  return mount(CalendarList, { props: { calendars: items, hidden } })
}
const actions = (row: ReturnType<typeof list>, id: string) =>
  row.findAll(`[data-testid="${id}"]`).length

describe('the calendar list', () => {
  it('has a heading with a button to add a calendar', async () => {
    const wrapper = list([personal])
    expect(wrapper.get('h2').text()).toBe('Calendars')
    await wrapper.get('[data-testid="calendar-add"]').trigger('click')
    expect(wrapper.emitted('add')).toHaveLength(1)
  })

  it('puts calendars of other people under a heading of their own', () => {
    const wrapper = list([personal, work, team])
    const headings = wrapper.findAll('h2').map((h) => h.text())
    expect(headings).toEqual(['Calendars', 'Shared with you'])
    const lists = wrapper.findAll('ul.oc-list[aria-label]')
    expect(lists[0].findAll('[data-testid="calendar-item"]')).toHaveLength(2)
    expect(lists[1].findAll('[data-testid="calendar-item"]')).toHaveLength(1)
  })

  it('has no shared heading when nothing is shared', () => {
    expect(list([personal, work]).findAll('h2').map((h) => h.text())).toEqual(['Calendars'])
  })

  it('does not print the owner of a calendar of the user', () => {
    const wrapper = list([personal])
    expect(wrapper.get('[data-testid="calendar-item"]').text()).not.toContain('·')
  })

  it('prints the owner and the access of a shared calendar on a line of its own', () => {
    const wrapper = list([team])
    expect(wrapper.get('.gw-calendar-meta').text()).toBe('Marie Curie · Read only')
  })

  it('switches a calendar through a checkbox with a name', async () => {
    const wrapper = list([personal], [])
    const box = wrapper.get('[data-testid="calendar-toggle"] input')
    expect(box.attributes('aria-label')).toBe('Show Personal')
    await box.setValue(false)
    expect(wrapper.emitted('toggle')).toEqual([[personal.id]])
  })

  it('shows a calendar as off when it is hidden', () => {
    const wrapper = list([personal], [personal.id])
    expect((wrapper.get('[data-testid="calendar-toggle"] input').element as HTMLInputElement).checked).toBe(false)
  })

  describe('the menu of a calendar', () => {
    it('has a button with a name for the calendar', () => {
      const button = list([personal]).get('[data-testid="calendar-actions"]')
      expect(button.attributes('aria-label')).toBe('Actions for Personal')
    })

    it('offers every action to the owner, in this order', () => {
      const wrapper = list([personal])
      const ids = wrapper
        .findAll('[data-testid^="calendar-"]')
        .map((e) => e.attributes('data-testid'))
        .filter((id) => ['calendar-edit', 'calendar-share', 'calendar-import', 'calendar-export', 'calendar-delete'].includes(id!))
      expect(ids).toEqual(['calendar-edit', 'calendar-share', 'calendar-import', 'calendar-export', 'calendar-delete'])
    })

    it('leaves out delete for a calendar that cannot be removed', () => {
      const wrapper = list([{ ...personal, isRemovable: false }])
      expect(actions(wrapper, 'calendar-delete')).toBe(0)
      expect(actions(wrapper, 'calendar-edit')).toBe(1)
    })

    it('offers only export to a recipient who may not write', () => {
      const wrapper = list([{ ...team, canEdit: false }])
      expect(actions(wrapper, 'calendar-edit')).toBe(0)
      expect(actions(wrapper, 'calendar-share')).toBe(0)
      expect(actions(wrapper, 'calendar-import')).toBe(0)
      expect(actions(wrapper, 'calendar-delete')).toBe(0)
      expect(actions(wrapper, 'calendar-export')).toBe(1)
    })

    it('offers import to a recipient who may write events', () => {
      const wrapper = list([{ ...team, canEdit: true }])
      expect(actions(wrapper, 'calendar-import')).toBe(1)
      expect(actions(wrapper, 'calendar-edit')).toBe(0)
    })

    it('asks the page to act on the calendar', async () => {
      const wrapper = list([personal])
      for (const [id, event] of [
        ['calendar-edit', 'edit'],
        ['calendar-share', 'share'],
        ['calendar-import', 'import'],
        ['calendar-export', 'export'],
        ['calendar-delete', 'remove']
      ]) {
        await wrapper.get(`[data-testid="${id}"]`).trigger('click')
        expect(wrapper.emitted(event)).toEqual([[personal]])
      }
    })

    it('opens the menu in place when the list is inside a drop', async () => {
      const wrapper = mount(CalendarList, { props: { calendars: [personal], hidden: [], nested: true } })
      // A drop cannot hold another drop, so there is none, and no menu yet.
      expect(wrapper.findComponent({ name: 'OcDrop' }).exists()).toBe(false)
      expect(actions(wrapper, 'calendar-edit')).toBe(0)
      const button = wrapper.get('[data-testid="calendar-actions"]')
      await button.trigger('click')
      expect(button.attributes('aria-expanded')).toBe('true')
      await wrapper.get('[data-testid="calendar-export"]').trigger('click')
      expect(wrapper.emitted('export')).toEqual([[personal]])
      // Choosing closes the menu.
      expect(actions(wrapper, 'calendar-export')).toBe(0)
    })

    it('ties each menu to the button of its own calendar', () => {
      const wrapper = list([personal, work])
      const buttons = wrapper.findAll('[data-testid="calendar-actions"]').map((b) => b.attributes('id'))
      expect(new Set(buttons).size).toBe(2)
    })
  })
})
