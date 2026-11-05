import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import folders from '../../../internal/graphapi/testdata/contract/contact-folders.json'
import contacts from '../../../internal/graphapi/testdata/contract/contacts.json'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const listContacts = vi.fn()
vi.mock('../../src/composables/useApi', () => {
  const client = {
    listContactFolders: () => Promise.resolve(folders.value),
    listContacts: (id: string) => listContacts(id)
  }
  return { useApi: () => ({ client, run: (_t: string, f: (c: unknown) => unknown) => f(client) }) }
})

import ContactsPage from '../../src/pages/ContactsPage.vue'

const mountOptions = { global: { mocks: { $gettext: (s: string) => s } } }
const [personal, team] = folders.value

beforeEach(() => {
  listContacts.mockReset()
  listContacts.mockImplementation((id: string) =>
    Promise.resolve(
      id === personal.id ? contacts.value : [{ ...contact, id: 'team.marie', parentFolderId: id }]
    )
  )
})

async function open() {
  const wrapper = mount(ContactsPage, mountOptions)
  await flushPromises()
  return wrapper
}

const names = (w: Awaited<ReturnType<typeof open>>) =>
  w.findAll('[data-testid="contact-item"] .name').map((n) => n.text())

describe('the contacts page', () => {
  it('lists the contacts of every address book at first', async () => {
    const w = await open()
    expect(listContacts).toHaveBeenCalledWith(personal.id)
    expect(listContacts).toHaveBeenCalledWith(team.id)
    expect(names(w)).toContain('Albert Einstein')
    expect(names(w)).toContain('Dr. Marie Salomea Curie')
  })

  it('shows one address book after it is chosen', async () => {
    const w = await open()
    await w.findAll('[data-testid="folder-item"]')[1].trigger('click')
    await flushPromises()
    expect(names(w)).toEqual(['Dr. Marie Salomea Curie'])
    await w.find('[data-testid="folder-all"]').trigger('click')
    await flushPromises()
    expect(names(w).length).toBeGreaterThan(1)
  })

  it('narrows the list with the search box', async () => {
    const w = await open()
    await w.find('[data-testid="contact-search"]').setValue('curie')
    expect(names(w)).toEqual(['Dr. Marie Salomea Curie', 'Dr. Marie Salomea Curie'])
    await w.find('[data-testid="contact-search"]').setValue('zzz')
    expect(w.find('[data-testid="no-match"]').exists()).toBe(true)
  })

  it('sorts by the chosen key', async () => {
    const w = await open()
    await w.find('[data-testid="contact-sort"]').setValue('company')
    const company = names(w)
    expect(company.indexOf('Albert Einstein')).toBeGreaterThanOrEqual(0)
    await w.find('[data-testid="contact-sort"]').setValue('surname')
    expect(names(w)[0]).toBe('Dr. Marie Salomea Curie')
  })

  it('shows the details of the contact that is clicked', async () => {
    const w = await open()
    const item = w
      .findAll('[data-testid="contact-item"]')
      .find((i) => i.text().includes('Marie'))!
    await item.trigger('click')
    const detail = w.find('[data-testid="contact-detail"]')
    expect(detail.text()).toContain('Dr. Marie Salomea Curie')
    expect(detail.text()).toContain('marie@example.org')
    expect(detail.text()).toContain('+33 1 55 55 01 01')
    expect(detail.text()).toContain('Sorbonne')
    expect(detail.text()).toContain('Sceaux')
    expect(detail.text()).toContain('Physicists')
  })
})
