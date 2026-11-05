import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import folders from '../../../../internal/graphapi/testdata/contract/contact-folders.json'
import permissions from '../../../../internal/graphapi/testdata/contract/calendar-permissions.json'
import ContactsPage from '../../../src/pages/ContactsPage.vue'

// The design system and the host's services are not here; see mocks/webPkg.
vi.mock('@ownclouders/web-pkg', async () => (await import('../mocks/webPkg')).webPkgMock())

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const api = {
  listContactFolders: vi.fn(),
  listContacts: vi.fn(),
  listPermissions: vi.fn()
}
vi.mock('../../../src/composables/useApi', () => ({
  useApi: () => ({
    client: api,
    run: (_t: string, f: (c: unknown) => unknown) => Promise.resolve(f(api))
  })
}))

const gettext = (s: string, params: Record<string, string> = {}) =>
  s.replace(/%\{(\w+)\}/g, (_, k: string) => params[k] ?? '')
const global = { mocks: { $gettext: gettext } }
const [personal, team] = folders.value

beforeEach(() => {
  document.body.innerHTML = ''
  api.listContactFolders.mockResolvedValue(structuredClone(folders.value))
  api.listContacts.mockResolvedValue([])
  api.listPermissions.mockResolvedValue(structuredClone(permissions.value))
})

async function open() {
  const wrapper = mount(ContactsPage, { global, attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('contacts page sharing', () => {
  it('offers sharing only for an address book the user owns', async () => {
    const wrapper = await open()
    const items = wrapper.findAll('[data-testid="folder-item"]')
    await items[0].trigger('click')
    expect(wrapper.find('[data-testid="book-share"]').exists()).toBe(true)
    await items[1].trigger('click')
    expect(wrapper.find('[data-testid="book-share"]').exists()).toBe(false)
  })

  it('says whether a shared address book is read only or can be edited', async () => {
    api.listContactFolders.mockResolvedValue([
      personal,
      team,
      { ...team, id: 'x', 'groupware.canEdit': true }
    ])
    const wrapper = await open()
    const items = wrapper.findAll('[data-testid="folder-item"]')
    expect(items[1].text().replace(/\s+/g, ' ')).toContain('Marie Curie, read only')
    expect(items[2].text().replace(/\s+/g, ' ')).toContain('Marie Curie, can edit')
  })

  it('opens the share dialog of the chosen address book', async () => {
    const wrapper = await open()
    await wrapper.findAll('[data-testid="folder-item"]')[0].trigger('click')
    await wrapper.get('[data-testid="book-share"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('Share Contacts')
    expect(api.listPermissions).toHaveBeenCalledWith('addressBook', personal.id)
  })
})
