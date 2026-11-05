import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'
import ContactGroups from '../../src/contacts/ContactGroups.vue'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const marie = contact as unknown as GraphContact
const mountIt = (canEdit = true, categories = ['Physicists']) =>
  mount(ContactGroups, {
    props: { contact: { ...marie, categories }, canEdit, suggestions: ['Friends', 'Physicists'] },
    global: { mocks: { $gettext: (s: string, p: Record<string, string> = {}) => s.replace(/%\{(\w+)\}/g, (_, k: string) => p[k] ?? '') } }
  })
const find = (w: ReturnType<typeof mountIt>, id: string) => w.find(`[data-testid="${id}"]`)

describe('the groups of a contact', () => {
  it('shows a tag for each group, with a way to leave it', async () => {
    const w = mountIt()
    expect(find(w, 'membership-name').text()).toBe('Physicists')
    await find(w, 'membership-remove').trigger('click')
    expect(w.emitted('change')).toEqual([[[]]])
    expect(find(w, 'membership-remove').attributes('aria-label')).toBe('Remove from group Physicists')
  })

  it('opens a field for a name on the add button, and adds the group on Enter or the button', async () => {
    const w = mountIt()
    expect(find(w, 'membership-input').exists()).toBe(false)
    await find(w, 'membership-start').trigger('click')
    expect(find(w, 'membership-add').attributes('disabled')).toBeDefined()
    await find(w, 'membership-input').setValue('Friends')
    await find(w, 'membership-input').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('change')).toEqual([[['Physicists', 'Friends']]])
    expect(find(w, 'membership-input').exists()).toBe(false)
    await find(w, 'membership-start').trigger('click')
    await find(w, 'membership-input').setValue('Colleagues')
    await find(w, 'membership-add').trigger('click')
    expect(w.emitted('change')![1]).toEqual([['Physicists', 'Colleagues']])
  })

  it('does not add a group the contact is in', async () => {
    const w = mountIt()
    await find(w, 'membership-start').trigger('click')
    await find(w, 'membership-input').setValue('physicists')
    await find(w, 'membership-add').trigger('click')
    expect(w.emitted('change')).toBeUndefined()
  })

  it('closes the field when another contact is shown', async () => {
    const w = mountIt()
    await find(w, 'membership-start').trigger('click')
    expect(find(w, 'membership-input').exists()).toBe(true)
    await w.setProps({ contact: { ...marie, id: 'other', categories: [] } })
    expect(find(w, 'membership-input').exists()).toBe(false)
  })

  it('drops the field again on Escape', async () => {
    const w = mountIt()
    await find(w, 'membership-start').trigger('click')
    await find(w, 'membership-input').trigger('keydown', { key: 'Escape' })
    expect(find(w, 'membership-input').exists()).toBe(false)
  })

  it('only lists the groups of a contact that cannot be changed, or says there are none', () => {
    const w = mountIt(false)
    expect(find(w, 'membership-name').exists()).toBe(true)
    expect(find(w, 'membership-remove').exists()).toBe(false)
    expect(find(w, 'membership-start').exists()).toBe(false)
    expect(mountIt(false, []).text()).toContain('Not in any group.')
  })
})
