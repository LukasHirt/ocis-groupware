import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import folders from '../../../internal/graphapi/testdata/contract/contact-folders.json'
import contacts from '../../../internal/graphapi/testdata/contract/contacts.json'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const listContacts = vi.fn()
const createContact = vi.fn()
const updateContact = vi.fn()
const deleteContact = vi.fn()
vi.mock('../../src/composables/useApi', () => {
  const client = {
    listContactFolders: () => Promise.resolve(folders.value),
    listContacts: (id: string) => listContacts(id),
    createContact: (...a: unknown[]) => createContact(...a),
    updateContact: (...a: unknown[]) => updateContact(...a),
    deleteContact: (...a: unknown[]) => deleteContact(...a)
  }
  return { useApi: () => ({ client, run: (_t: string, f: (c: unknown) => unknown) => f(client) }) }
})

import ContactsPage from '../../src/pages/ContactsPage.vue'

const mountOptions = { global: { mocks: { $gettext: (s: string) => s } } }
const [personal, team] = folders.value

beforeEach(() => {
  listContacts.mockReset()
  createContact.mockReset()
  updateContact.mockReset()
  deleteContact.mockReset()
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

  describe('editing', () => {
    const marie = (w: Awaited<ReturnType<typeof open>>) =>
      w.findAll('[data-testid="contact-item"]').find((i) => i.text().includes('Marie'))!

    it('creates a contact in the chosen address book and selects it', async () => {
      createContact.mockImplementation((folder: string, input: Record<string, unknown>) =>
        Promise.resolve({
          ...contact,
          id: 'new.id',
          parentFolderId: folder,
          displayName: `${input.givenName} ${input.surname}`,
          givenName: input.givenName,
          surname: input.surname
        })
      )
      const w = await open()
      await w.find('[data-testid="contact-new"]').trigger('click')
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(true)
      expect(w.find('[data-testid="form-save"]').attributes('disabled')).toBeDefined()
      await w.find('[data-testid="field-givenName"]').setValue('Ada')
      await w.find('[data-testid="field-surname"]').setValue('Lovelace')
      await w.find('[data-testid="contact-form"]').trigger('submit')
      await flushPromises()
      expect(createContact).toHaveBeenCalledWith(personal.id, {
        givenName: 'Ada',
        surname: 'Lovelace'
      })
      expect(names(w)).toContain('Ada Lovelace')
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(false)
      expect(w.find('[data-testid="contact-detail"]').text()).toContain('Ada Lovelace')
    })

    it('saves only the changed fields of an edited contact, with its etag', async () => {
      updateContact.mockImplementation((_f: string, _id: string, input: object) =>
        Promise.resolve({ ...contact, ...input })
      )
      const w = await open()
      await marie(w).trigger('click')
      await w.find('[data-testid="contact-edit"]').trigger('click')
      expect((w.find('[data-testid="field-givenName"]').element as HTMLInputElement).value).toBe(
        'Marie'
      )
      await w.find('[data-testid="field-jobTitle"]').setValue('Director')
      await w.find('[data-testid="contact-form"]').trigger('submit')
      await flushPromises()
      expect(updateContact).toHaveBeenCalledWith(
        contact.parentFolderId,
        contact.id,
        { jobTitle: 'Director' },
        contact['@odata.etag']
      )
      expect(w.find('[data-testid="contact-detail"]').text()).toContain('Director')
    })

    it('keeps the form open when saving fails', async () => {
      updateContact.mockResolvedValue(undefined)
      const w = await open()
      await marie(w).trigger('click')
      await w.find('[data-testid="contact-edit"]').trigger('click')
      await w.find('[data-testid="field-jobTitle"]').setValue('Director')
      await w.find('[data-testid="contact-form"]').trigger('submit')
      await flushPromises()
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(true)
    })

    it('discards the form on cancel', async () => {
      const w = await open()
      await marie(w).trigger('click')
      await w.find('[data-testid="contact-edit"]').trigger('click')
      await w.find('[data-testid="form-cancel"]').trigger('click')
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(false)
      expect(updateContact).not.toHaveBeenCalled()
    })

    it('deletes a contact after a confirmation', async () => {
      deleteContact.mockResolvedValue(undefined)
      const w = await open()
      const before = names(w).length
      await marie(w).trigger('click')
      await w.find('[data-testid="contact-delete"]').trigger('click')
      expect(deleteContact).not.toHaveBeenCalled()
      await w.find('[data-testid="contact-delete-confirm"]').trigger('click')
      await flushPromises()
      expect(deleteContact).toHaveBeenCalledWith(
        contact.parentFolderId,
        contact.id,
        contact['@odata.etag']
      )
      expect(names(w)).toHaveLength(before - 1)
      expect(w.find('[data-testid="contact-detail"]').exists()).toBe(false)
    })

    it('offers no editing in a read-only address book', async () => {
      // The second address book of the fixtures is shared read only.
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[1].trigger('click')
      await flushPromises()
      await w.find('[data-testid="contact-item"]').trigger('click')
      expect(w.find('[data-testid="contact-detail"]').exists()).toBe(true)
      expect(w.find('[data-testid="contact-edit"]').exists()).toBe(false)
      expect(w.find('[data-testid="contact-delete"]').exists()).toBe(false)
      expect(w.find('[data-testid="contact-new"]').attributes('disabled')).toBeDefined()
    })
  })
})
