import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GwModal from '../../../src/components/GwModal.vue'
import ConfirmDialog from '../../../src/components/ConfirmDialog.vue'
import CalendarDialog from '../../../src/calendar/CalendarDialog.vue'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

describe('GwModal', () => {
  it('has a title, a body and an accessible role', () => {
    const wrapper = mount(GwModal, {
      props: { title: 'Delete calendar' },
      slots: { default: '<p>Sure?</p>' }
    })
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    const labelled = dialog.attributes('aria-labelledby')!
    expect(wrapper.get(`#${labelled}`).text()).toBe('Delete calendar')
    expect(dialog.text()).toContain('Sure?')
  })

  it('shows the start and actions slots in its action row', () => {
    const wrapper = mount(GwModal, {
      props: { title: 'T' },
      slots: { start: '<button data-testid="start">Delete</button>', actions: '<button data-testid="act">Save</button>' }
    })
    const row = wrapper.get('.gw-modal-actions')
    expect(row.find('[data-testid="start"]').exists()).toBe(true)
    expect(row.find('[data-testid="act"]').exists()).toBe(true)
  })

  it('emits close on Escape', async () => {
    const wrapper = mount(GwModal, { props: { title: 'T' } })
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('ignores Escape when it is persistent', async () => {
    const wrapper = mount(GwModal, { props: { title: 'T', persistent: true } })
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('submits through a form that holds the actions, when asked to', async () => {
    const wrapper = mount(GwModal, {
      props: { title: 'T', form: true },
      slots: { actions: '<button type="submit" data-testid="go">Go</button>' },
      attachTo: document.body
    })
    await wrapper.get('[data-testid="go"]').trigger('click')
    expect(wrapper.emitted('submit')).toHaveLength(1)
    expect(wrapper.get('form').find('[data-testid="go"]').exists()).toBe(true)
  })

  it('has no form unless it is asked to', () => {
    const wrapper = mount(GwModal, { props: { title: 'T' } })
    expect(wrapper.find('form').exists()).toBe(false)
  })

})

describe('ConfirmDialog', () => {
  it('asks, cancels and confirms through its two buttons', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: { title: 'Delete', message: 'Sure?', confirmLabel: 'Delete' }
    })
    expect(wrapper.text()).toContain('Sure?')
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })

  it('cancels on Escape', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: { title: 'Delete', message: 'Sure?', confirmLabel: 'Delete' }
    })
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('cancel')).toHaveLength(1)
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
