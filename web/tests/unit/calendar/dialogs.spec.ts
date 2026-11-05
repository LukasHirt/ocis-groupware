import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GwModal from '../../../src/components/GwModal.vue'
import ConfirmDialog from '../../../src/components/ConfirmDialog.vue'
import CalendarDialog from '../../../src/calendar/CalendarDialog.vue'
import ScopeDialog from '../../../src/calendar/ScopeDialog.vue'

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

  it('shows a trash icon in the title by default', () => {
    const wrapper = mount(ConfirmDialog, {
      props: { title: 'Delete', message: 'Sure?', confirmLabel: 'Delete' }
    })
    expect(wrapper.get('.oc-modal-title .oc-icon').attributes('data-name')).toBe('delete-bin')
  })

  it('shows the icon it is given in the title', () => {
    const wrapper = mount(ConfirmDialog, {
      props: { title: 'Revoke', message: 'Sure?', confirmLabel: 'Revoke', icon: 'error-warning' }
    })
    expect(wrapper.get('.oc-modal-title .oc-icon').attributes('data-name')).toBe('error-warning')
  })
})

describe('CalendarDialog', () => {
  it('asks for a name before it saves', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    const message = wrapper.get('#gw-calendar-name-message')
    expect(message.text()).toContain('name')
    // The message belongs to the field.
    const field = wrapper.get('[data-testid="calendar-name"]')
    expect(field.attributes('aria-describedby')).toBe('gw-calendar-name-message')
    expect(field.attributes('aria-invalid')).toBe('true')
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

  it('offers the colours as radios with names', () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    const group = wrapper.get('[role="radiogroup"]')
    const names = group.findAll('[role="radio"]').map((r) => r.attributes('aria-label'))
    expect(names.slice(0, 3)).toEqual(['Blue', 'Green', 'Orange'])
    expect(names.at(-1)).toBe('Custom colour')
    expect(wrapper.findAll('[aria-checked="true"]')).toHaveLength(1)
  })

  it('moves the choice with the arrow keys and keeps one tab stop', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' }, attachTo: document.body })
    const radios = () => wrapper.findAll('[role="radio"]')
    // The eight palette swatches are one tab stop; the custom one is the next.
    expect(radios().slice(0, 8).filter((r) => r.attributes('tabindex') === '0')).toHaveLength(1)
    await radios()[0].trigger('keydown', { key: 'ArrowRight' })
    expect(radios()[1].attributes('aria-checked')).toBe('true')
    expect(radios()[1].attributes('tabindex')).toBe('0')
    expect(radios()[0].attributes('tabindex')).toBe('-1')
    await radios()[1].trigger('keydown', { key: 'ArrowLeft' })
    await radios()[0].trigger('keydown', { key: 'ArrowLeft' })
    // It wraps around to the last colour of the palette.
    expect(radios()[7].attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })

  it('takes a custom colour', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    await wrapper.get('[data-testid="calendar-name"]').setValue('Trips')
    await wrapper.get('[data-testid="calendar-color-custom"]').setValue('#123456')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([[{ name: 'Trips', hexColor: '#123456' }]])
  })

  it('cancels', async () => {
    const wrapper = mount(CalendarDialog, { props: { title: 'New calendar' } })
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})

describe('ScopeDialog', () => {
  it('asks about changing, with the safe choice first and the main one filled', async () => {
    const wrapper = mount(ScopeDialog)
    expect(wrapper.get('[role="dialog"]').text()).toContain('Edit repeating event')
    const buttons = wrapper.findAll('button').map((b) => b.attributes('data-testid'))
    expect(buttons).toEqual(['dialog-cancel', 'scope-this', 'scope-all'])
    await wrapper.get('[data-testid="scope-this"]').trigger('click')
    await wrapper.get('[data-testid="scope-all"]').trigger('click')
    expect(wrapper.emitted('choose')).toEqual([['this'], ['all']])
  })

  it('asks about deleting in its own words', () => {
    const wrapper = mount(ScopeDialog, { props: { remove: true } })
    expect(wrapper.get('[role="dialog"]').text()).toContain('Delete repeating event')
    expect(wrapper.text()).toContain('delete only this event')
  })

  it('closes on Escape', async () => {
    const wrapper = mount(ScopeDialog)
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
