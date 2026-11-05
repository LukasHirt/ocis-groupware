import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModalDialog from '../../../src/components/ModalDialog.vue'
import CalendarDialog from '../../../src/calendar/CalendarDialog.vue'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

describe('ModalDialog', () => {
  it('has a title, a body and an accessible role', () => {
    const wrapper = mount(ModalDialog, {
      props: { title: 'Delete calendar' },
      slots: { default: '<p>Sure?</p>' }
    })
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    const labelled = dialog.attributes('aria-labelledby')!
    expect(wrapper.get(`#${labelled}`).text()).toBe('Delete calendar')
    expect(dialog.text()).toContain('Sure?')
  })

  it('closes on Escape and on a click outside', async () => {
    const wrapper = mount(ModalDialog, { props: { title: 'T' } })
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await wrapper.get('[data-testid="modal-backdrop"]').trigger('mousedown')
    expect(wrapper.emitted('close')).toHaveLength(2)
  })

  it('does not close when the click starts inside', async () => {
    const wrapper = mount(ModalDialog, { props: { title: 'T' } })
    await wrapper.get('[role="dialog"]').trigger('mousedown')
    expect(wrapper.emitted('close')).toBeUndefined()
  })
})

describe('CalendarDialog', () => {
  it('asks for a name before it saves', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('[data-testid="calendar-name-error"]').text()).toContain('name')
  })

  it('saves the trimmed name and the chosen colour', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    await wrapper.get('[data-testid="calendar-name"]').setValue('  Holidays ')
    await wrapper.get('[data-testid="calendar-color-#2f9e6e"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([[{ name: 'Holidays', hexColor: '#2f9e6e' }]])
  })

  it('starts from the calendar it edits and saves only what changed', async () => {
    const wrapper = mount(CalendarDialog, {
      props: {
        title: 'Edit',
        calendar: { id: 'x', name: 'Work', hexColor: '#3366cc' }
      }
    })
    const name = wrapper.get<HTMLInputElement>('[data-testid="calendar-name"]')
    expect(name.element.value).toBe('Work')
    await name.setValue('Office')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([[{ name: 'Office' }]])
  })

  it('does not save when nothing changed', async () => {
    const wrapper = mount(CalendarDialog, {
      props: { title: 'Edit', calendar: { id: 'x', name: 'Work', hexColor: '#3366cc' } }
    })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('shows the colour of a calendar that has none as chosen', () => {
    const wrapper = mount(CalendarDialog, {
      props: { title: 'Edit', calendar: { id: 'cGVyc29uYWwtY2Fs', name: 'Personal', hexColor: '' } }
    })
    expect(wrapper.findAll('[aria-checked="true"]')).toHaveLength(1)
  })

  it('cancels', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
