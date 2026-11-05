import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsDialog from '../../../src/calendar/SettingsDialog.vue'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

const current = { timeZone: '', firstDay: 1, weekNumbers: false }

describe('SettingsDialog', () => {
  it('shows the current settings, with the browser zone named', () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'Europe/Prague' } })
    const zone = wrapper.get<HTMLSelectElement>('[data-testid="settings-zone"]')
    expect(zone.element.value).toBe('')
    expect(zone.findAll('option')[0].text()).toContain('Europe/Prague')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="settings-first-day"]').element.value).toBe('1')
    expect(wrapper.get<HTMLInputElement>('[data-testid="settings-week-numbers"]').element.checked).toBe(false)
  })

  it('saves the changes', async () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'UTC' } })
    await wrapper.get('[data-testid="settings-zone"]').setValue('Asia/Tokyo')
    await wrapper.get('[data-testid="settings-first-day"]').setValue('0')
    await wrapper.get('[data-testid="settings-week-numbers"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([
      [{ timeZone: 'Asia/Tokyo', firstDay: 0, weekNumbers: true }]
    ])
  })

  it('offers the days a week can start on', () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'UTC' } })
    const days = wrapper.findAll('[data-testid="settings-first-day"] option').map((o) => o.text())
    expect(days).toEqual(['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'])
  })

  it('cancels', async () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'UTC' } })
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('save')).toBeUndefined()
  })
})
