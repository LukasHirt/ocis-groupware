import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import permissions from '../../../../internal/graphapi/testdata/contract/calendar-permissions.json'
import permission from '../../../../internal/graphapi/testdata/contract/calendar-permission.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
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
    expect(rows.map((r) => (r.get('[data-testid="share-role"]').element as HTMLSelectElement).value)).toEqual([
      'read',
      'write'
    ])
  })

  it('says so when nobody has access yet', async () => {
    api.listPermissions.mockResolvedValue([])
    const wrapper = await open()
    expect(wrapper.find('[data-testid="share-none"]').exists()).toBe(true)
  })

  it('changes a role', async () => {
    api.updatePermission.mockResolvedValue({ ...permission, role: 'write' })
    const wrapper = await open()
    await wrapper.findAll('[data-testid="share-role"]')[0].setValue('write')
    await flushPromises()
    expect(api.updatePermission).toHaveBeenCalledWith('calendar', 'c1', permission.id, 'write')
    const select = wrapper.findAll('[data-testid="share-role"]')[0].element as HTMLSelectElement
    expect(select.value).toBe('write')
  })

  it('keeps the old role when the change fails', async () => {
    api.updatePermission.mockRejectedValue(new Error('no'))
    const wrapper = await open()
    await wrapper.findAll('[data-testid="share-role"]')[0].setValue('write')
    await flushPromises()
    const select = wrapper.findAll('[data-testid="share-role"]')[0].element as HTMLSelectElement
    expect(select.value).toBe('read')
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
    expect(wrapper.find('[data-testid="share-hint"]').exists()).toBe(true)
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
    await wrapper.get('[data-testid="share-new-role"]').setValue('write')
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

  it('cannot share before someone is chosen', async () => {
    const wrapper = await open()
    expect(wrapper.get('[data-testid="share-add"]').attributes('disabled')).toBeDefined()
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
