import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import permissions from '../../../../internal/graphapi/testdata/contract/calendar-permissions.json'
import permission from '../../../../internal/graphapi/testdata/contract/calendar-permission.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

const api = {
  listPermissions: vi.fn(),
  createPermission: vi.fn(),
  updatePermission: vi.fn(),
  deletePermission: vi.fn(),
  searchUsers: vi.fn(),
  searchGroups: vi.fn()
}
vi.mock('../../../src/composables/useApi', () => ({
  useApi: () => ({
    client: api,
    run: (_t: string, f: (c: unknown) => unknown) => Promise.resolve(f(api)).catch(() => undefined)
  })
}))

import ShareDialog from '../../../src/sharing/ShareDialog.vue'
import { choose, selectedLabel } from '../helpers/select'

const gettext = (s: string, params: Record<string, string> = {}) =>
  s.replace(/%\{(\w+)\}/g, (_, k: string) => params[k] ?? '')

async function open(kind: 'calendar' | 'addressBook' = 'calendar') {
  const wrapper = mount(ShareDialog, {
    props: { kind, collectionId: 'c1', name: 'Team' },
    global: { mocks: { $gettext: gettext } },
    attachTo: document.body
  })
  await flushPromises()
  return wrapper
}

// The role the control of a row is given, as the model of the select.
function roleOf(wrapper: Awaited<ReturnType<typeof open>>, row: number) {
  const selects = wrapper
    .findAllComponents({ name: 'OcSelect' })
    .filter((c) => c.find('[data-testid="share-role"]').exists())
  return selects[row].props('modelValue')
}

// The search waits for the typing to stop.
async function type(wrapper: Awaited<ReturnType<typeof open>>, text: string) {
  await wrapper.get('[data-testid="share-search"]').setValue(text)
  await vi.advanceTimersByTimeAsync(400)
  await flushPromises()
}

beforeEach(() => {
  vi.useFakeTimers()
  for (const f of Object.values(api)) f.mockReset()
  api.listPermissions.mockResolvedValue(structuredClone(permissions.value))
  api.searchUsers.mockResolvedValue([])
  api.searchGroups.mockResolvedValue([])
  document.body.innerHTML = ''
})
afterEach(() => vi.useRealTimers())

describe('the share dialog', () => {
  it('lists who the collection is shared with, with the role of each', async () => {
    const wrapper = await open()
    expect(api.listPermissions).toHaveBeenCalledWith('calendar', 'c1')
    const rows = wrapper.findAll('[data-testid="share-item"]')
    expect(rows.map((r) => r.get('.name').text())).toEqual(['Marie Curie', 'physics-lovers'])
    expect(rows[0].text()).toContain('User')
    expect(rows[1].text()).toContain('Group')
    expect(rows.map((r) => selectedLabel(r, 'share-role'))).toEqual(['Can view', 'Can edit'])
  })

  it('says so when nobody has access yet', async () => {
    api.listPermissions.mockResolvedValue([])
    const wrapper = await open()
    expect(wrapper.find('[data-testid="share-none"]').exists()).toBe(true)
  })

  it('changes a role', async () => {
    api.updatePermission.mockResolvedValue({ ...permission, role: 'write' })
    const wrapper = await open()
    await choose(wrapper.findAll('[data-testid="share-item"]')[0], 'share-role', 'Can edit')
    await flushPromises()
    expect(api.updatePermission).toHaveBeenCalledWith('calendar', 'c1', permission.id, 'write')
    expect(roleOf(wrapper, 0)).toBe('write')
  })

  it('keeps the old role when the change fails', async () => {
    api.updatePermission.mockRejectedValue(new Error('no'))
    const wrapper = await open()
    await choose(wrapper.findAll('[data-testid="share-item"]')[0], 'share-role', 'Can edit')
    await flushPromises()
    // The list still holds the old role, which the control shows.
    expect(roleOf(wrapper, 0)).toBe('read')
  })

  it('revokes a share', async () => {
    api.deletePermission.mockResolvedValue(undefined)
    const wrapper = await open('addressBook')
    await wrapper.findAll('[data-testid="share-revoke"]')[0].trigger('click')
    await flushPromises()
    expect(api.deletePermission).toHaveBeenCalledWith('addressBook', 'c1', permission.id)
    expect(wrapper.findAll('[data-testid="share-item"]')).toHaveLength(1)
  })

  it('does not search before three characters are typed', async () => {
    const wrapper = await open()
    await type(wrapper, 'ma')
    expect(api.searchUsers).not.toHaveBeenCalled()
    expect(wrapper.get('#gw-share-search-message').text()).toContain('at least 3')
  })

  it('searches users and groups once typing stops, and leaves out those who already have access', async () => {
    api.searchUsers.mockResolvedValue([
      { id: 'u-marie', displayName: 'Marie Curie' },
      { id: 'u-max', displayName: 'Max Planck' }
    ])
    api.searchGroups.mockResolvedValue([{ id: 'g-math', displayName: 'math-lovers' }])
    const wrapper = await open()
    await wrapper.get('[data-testid="share-search"]').setValue('m')
    await wrapper.get('[data-testid="share-search"]').setValue('mar')
    await wrapper.get('[data-testid="share-search"]').setValue('mat')
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(api.searchUsers).toHaveBeenCalledTimes(1)
    expect(api.searchUsers).toHaveBeenCalledWith('mat')
    expect(api.searchGroups).toHaveBeenCalledWith('mat')
    const results = wrapper.findAll('[data-testid="share-result"]')
    expect(results.map((r) => r.text())).toEqual([
      expect.stringContaining('Max Planck'),
      expect.stringContaining('math-lovers')
    ])
  })

  it('shares with the chosen user and adds the new row', async () => {
    api.searchUsers.mockResolvedValue([{ id: 'u-max', displayName: 'Max Planck' }])
    api.createPermission.mockResolvedValue({
      ...permission,
      id: 'new',
      emailAddress: { name: 'Max Planck' },
      grantee: { type: 'user', id: 'u-max' }
    })
    const wrapper = await open()
    await type(wrapper, 'max')
    await wrapper.get('[data-testid="share-result"]').trigger('click')
    expect(wrapper.get('[data-testid="share-chosen"]').text()).toContain('Max Planck')
    await choose(wrapper, 'share-new-role', 'Can edit')
    await wrapper.get('[data-testid="share-add"]').trigger('click')
    await flushPromises()
    expect(api.createPermission).toHaveBeenCalledWith(
      'calendar',
      'c1',
      { type: 'user', id: 'u-max' },
      'write'
    )
    expect(wrapper.findAll('[data-testid="share-item"]')).toHaveLength(3)
    // The form is empty again.
    expect(wrapper.find('[data-testid="share-chosen"]').exists()).toBe(false)
  })

  it('picks a result with the arrow keys and Enter', async () => {
    api.searchUsers.mockResolvedValue([
      { id: 'u-max', displayName: 'Max Planck' },
      { id: 'u-mia', displayName: 'Mia Hamm' }
    ])
    const wrapper = await open()
    await type(wrapper, 'm')
    await type(wrapper, 'mia')
    const search = wrapper.get('[data-testid="share-search"]')
    await search.trigger('keydown', { key: 'ArrowDown' })
    await search.trigger('keydown', { key: 'ArrowDown' })
    const options = wrapper.findAll('[role="option"]')
    expect(options[1].attributes('aria-selected')).toBe('true')
    expect(search.attributes('aria-activedescendant')).toBe(options[1].attributes('id'))
    await search.trigger('keydown', { key: 'Enter' })
    expect(wrapper.get('[data-testid="share-chosen"]').text()).toContain('Mia Hamm')
  })

  it('shows the results as a listbox', async () => {
    api.searchUsers.mockResolvedValue([{ id: 'u-max', displayName: 'Max Planck' }])
    const wrapper = await open()
    await type(wrapper, 'max')
    expect(wrapper.get('[role="listbox"]').findAll('[role="option"]')).toHaveLength(1)
  })

  it('removes the chosen person again', async () => {
    api.searchUsers.mockResolvedValue([{ id: 'u-max', displayName: 'Max Planck' }])
    const wrapper = await open()
    await type(wrapper, 'max')
    await wrapper.get('[data-testid="share-result"]').trigger('click')
    await wrapper.get('[data-testid="share-unchoose"]').trigger('click')
    expect(wrapper.find('[data-testid="share-chosen"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="share-search"]').exists()).toBe(true)
  })

  it('offers Share only once someone is chosen', async () => {
    api.searchUsers.mockResolvedValue([{ id: 'u-max', displayName: 'Max Planck' }])
    const wrapper = await open()
    expect(wrapper.find('[data-testid="share-add"]').exists()).toBe(false)
    await type(wrapper, 'max')
    await wrapper.get('[data-testid="share-result"]').trigger('click')
    expect(wrapper.find('[data-testid="share-add"]').exists()).toBe(true)
  })

  it('shows a group search that failed as no groups and still lists the users', async () => {
    api.searchUsers.mockResolvedValue([{ id: 'u-max', displayName: 'Max Planck' }])
    api.searchGroups.mockRejectedValue(new Error('403'))
    const wrapper = await open()
    await type(wrapper, 'max')
    expect(wrapper.findAll('[data-testid="share-result"]')).toHaveLength(1)
  })

  it('says when nobody matches', async () => {
    const wrapper = await open()
    await type(wrapper, 'zzz')
    expect(wrapper.find('[data-testid="share-no-match"]').exists()).toBe(true)
  })

  it('closes with the Close button', async () => {
    const wrapper = await open()
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
