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
const getContact = vi.fn()
vi.mock('../../src/composables/useApi', () => {
  const client = {
    listContactFolders: () => Promise.resolve(folders.value),
    listContacts: (id: string) => listContacts(id),
    createContact: (...a: unknown[]) => createContact(...a),
    updateContact: (...a: unknown[]) => updateContact(...a),
    deleteContact: (...a: unknown[]) => deleteContact(...a),
    getContact: (...a: unknown[]) => getContact(...a),
    getContactPhoto: () => Promise.resolve(new Blob(['x'], { type: 'image/png' }))
  }
  return { useApi: () => ({ client, run: (_t: string, f: (c: unknown) => unknown) => f(client) }) }
})

import ContactsPage from '../../src/pages/ContactsPage.vue'
import ContactPhoto from '../../src/contacts/ContactPhoto.vue'

const mountOptions = { global: { mocks: { $gettext: (s: string) => s } } }
const [personal, team] = folders.value

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:photo')
  URL.revokeObjectURL = vi.fn()
  listContacts.mockReset()
  createContact.mockReset()
  updateContact.mockReset()
  deleteContact.mockReset()
  getContact.mockReset()
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

  describe('photos', () => {
    it('shows the photo control of the selected contact and reloads the contact after a change', async () => {
      getContact.mockResolvedValue({
        ...contact,
        'groupware.hasPhoto': true,
        '@odata.etag': 'W/"new"'
      })
      const w = await open()
      await w
        .findAll('[data-testid="contact-item"]')
        .find((i) => i.text().includes('Marie'))!
        .trigger('click')
      const photo = w.findComponent(ContactPhoto)
      expect(photo.exists()).toBe(true)
      expect(photo.props('canEdit')).toBe(true)
      photo.vm.$emit('changed')
      await flushPromises()
      expect(getContact).toHaveBeenCalledWith(contact.parentFolderId, contact.id)
      expect(w.findComponent(ContactPhoto).props('contact')['groupware.hasPhoto']).toBe(true)
    })
  })

  describe('groups', () => {
    const groupNames = (w: Awaited<ReturnType<typeof open>>) =>
      w.findAll('[data-testid="group-item"] .name').map((n) => n.text())
    const chooseGroup = async (w: Awaited<ReturnType<typeof open>>, name: string) => {
      await w
        .findAll('[data-testid="group-item"]')
        .find((g) => g.find('.name').text() === name)!
        .trigger('click')
    }

    it('lists the categories of the shown contacts and the group cards', async () => {
      const w = await open()
      expect(groupNames(w)).toEqual(expect.arrayContaining(['Friends', 'Physicists']))
      expect(w.find('[data-testid="groupcard-item"]').text()).toContain('Physicists')
    })

    it('shows only the members of the chosen group', async () => {
      const w = await open()
      await chooseGroup(w, 'Friends')
      expect(names(w)).toEqual(['Albert Einstein'])
      await w.find('[data-testid="group-all"]').trigger('click')
      expect(names(w).length).toBeGreaterThan(1)
    })

    it('adds a contact to a group and removes it again', async () => {
      updateContact.mockImplementation((_f: string, _id: string, input: object) =>
        Promise.resolve({ ...contact, ...input })
      )
      const w = await open()
      await w
        .findAll('[data-testid="contact-item"]')
        .find((i) => i.text().includes('Marie'))!
        .trigger('click')
      await w.find('[data-testid="membership-input"]').setValue('Colleagues')
      await w.find('[data-testid="membership-add"]').trigger('click')
      await flushPromises()
      expect(updateContact).toHaveBeenLastCalledWith(
        contact.parentFolderId,
        contact.id,
        { categories: ['Physicists', 'Nobel, laureates', 'Colleagues'] },
        contact['@odata.etag']
      )
      expect(groupNames(w)).toContain('Colleagues')

      await w.find('[data-testid="membership-remove"]').trigger('click')
      await flushPromises()
      expect(updateContact).toHaveBeenLastCalledWith(
        contact.parentFolderId,
        contact.id,
        { categories: ['Nobel, laureates', 'Colleagues'] },
        expect.anything()
      )
    })

    it('renames a group on every contact in it', async () => {
      updateContact.mockImplementation((_f: string, id: string, input: object) =>
        Promise.resolve({ ...contact, id, ...input })
      )
      const w = await open()
      await chooseGroup(w, 'Physicists')
      await w.find('[data-testid="group-rename"]').trigger('click')
      await w.find('[data-testid="group-rename-input"]').setValue('Scientists')
      await w.find('[data-testid="group-rename-save"]').trigger('click')
      await flushPromises()
      const calls = updateContact.mock.calls
      expect(calls.length).toBeGreaterThan(0)
      for (const call of calls) expect(call[2].categories).toContain('Scientists')
      expect(groupNames(w)).toContain('Scientists')
      // The copy in the read-only address book keeps its old group.
      expect(groupNames(w)).toContain('Physicists')
    })

    it('deletes a group after a confirmation and keeps the contacts', async () => {
      updateContact.mockImplementation((_f: string, id: string, input: object) =>
        Promise.resolve({ ...contact, id, ...input })
      )
      const w = await open()
      const before = names(w).length
      await chooseGroup(w, 'Friends')
      await w.find('[data-testid="group-delete"]').trigger('click')
      expect(updateContact).not.toHaveBeenCalled()
      await w.find('[data-testid="group-delete-confirm"]').trigger('click')
      await flushPromises()
      expect(updateContact).toHaveBeenCalledTimes(1)
      expect(updateContact.mock.calls[0][2]).toEqual({ categories: [] })
      expect(groupNames(w)).not.toContain('Friends')
      expect(names(w)).toHaveLength(before)
    })

    it('leaves contacts of read-only address books alone when a group changes', async () => {
      updateContact.mockImplementation((_f: string, id: string, input: object) =>
        Promise.resolve({ ...contact, id, ...input })
      )
      const w = await open()
      await chooseGroup(w, 'Physicists')
      await w.find('[data-testid="group-delete"]').trigger('click')
      await w.find('[data-testid="group-delete-confirm"]').trigger('click')
      await flushPromises()
      for (const call of updateContact.mock.calls) expect(call[0]).toBe(personal.id)
    })

    it('does not offer group management for a group card', async () => {
      const w = await open()
      await w.find('[data-testid="groupcard-item"]').trigger('click')
      expect(w.find('[data-testid="group-rename"]').exists()).toBe(false)
      expect(w.find('[data-testid="group-delete"]').exists()).toBe(false)
    })
  })
})
