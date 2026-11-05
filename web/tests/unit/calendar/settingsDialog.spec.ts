import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsDialog from '../../../src/calendar/SettingsDialog.vue'
import { choose, optionLabels, selectedLabel } from '../helpers/select'

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
    expect(selectedLabel(wrapper, 'settings-zone')).toContain('Europe/Prague')
    expect(optionLabels(wrapper, 'settings-zone')[0]).toContain('Europe/Prague')
    expect(selectedLabel(wrapper, 'settings-first-day')).toBe('Monday')
    expect(wrapper.get<HTMLInputElement>('[data-testid="settings-week-numbers"] input').element.checked).toBe(false)
  })

  it('saves the changes', async () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'UTC' } })
    await choose(wrapper, 'settings-zone', 'Asia/Tokyo')
    await choose(wrapper, 'settings-first-day', 'Sunday')
    await wrapper.get('[data-testid="settings-week-numbers"] input').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([
      [{ timeZone: 'Asia/Tokyo', firstDay: 0, weekNumbers: true }]
    ])
  })

  it('offers the days a week can start on', () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'UTC' } })
    expect(optionLabels(wrapper, 'settings-first-day')).toEqual(['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'])
  })

  it('cancels', async () => {
    const wrapper = mount(SettingsDialog, { props: { settings: current, browserZone: 'UTC' } })
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('save')).toBeUndefined()
  })
})
