import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import folders from '../../../internal/graphapi/testdata/contract/contact-folders.json'
import contacts from '../../../internal/graphapi/testdata/contract/contacts.json'
import importReport from '../../../internal/graphapi/testdata/contract/import-report-contacts.json'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import { eventBus, isSideBarOpen } from './mocks/webPkg'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const listContacts = vi.fn()
const createContact = vi.fn()
const updateContact = vi.fn()
const deleteContact = vi.fn()
const getContact = vi.fn()
const exportAddressBook = vi.fn()
const importAddressBook = vi.fn()
const createContactFolder = vi.fn()
const updateContactFolder = vi.fn()
const deleteContactFolder = vi.fn()
vi.mock('../../src/composables/useApi', () => {
  const client = {
    listContactFolders: () => Promise.resolve(structuredClone(folders.value)),
    listContacts: (id: string) => listContacts(id),
    createContact: (...a: unknown[]) => createContact(...a),
    updateContact: (...a: unknown[]) => updateContact(...a),
    deleteContact: (...a: unknown[]) => deleteContact(...a),
    getContact: (...a: unknown[]) => getContact(...a),
    exportAddressBook: (...a: unknown[]) => exportAddressBook(...a),
    importAddressBook: (...a: unknown[]) => importAddressBook(...a),
    createContactFolder: (...a: unknown[]) => createContactFolder(...a),
    updateContactFolder: (...a: unknown[]) => updateContactFolder(...a),
    deleteContactFolder: (...a: unknown[]) => deleteContactFolder(...a),
    getContactPhoto: () => Promise.resolve(new Blob(['x'], { type: 'image/png' }))
  }
  return { useApi: () => ({ client, run: (_t: string, f: (c: unknown) => unknown) => f(client) }) }
})

import ContactsPage from '../../src/pages/ContactsPage.vue'
import ContactPhoto from '../../src/contacts/ContactPhoto.vue'

// The design system and the host's services are not here; see mocks/webPkg.
vi.mock('@ownclouders/web-pkg', async () => (await import('./mocks/webPkg')).webPkgMock())

// The host's gettext fills %{name} placeholders; so does this stand-in.
const gettext = (s: string, params: Record<string, string> = {}) =>
  s.replace(/%\{(\w+)\}/g, (_, k: string) => params[k] ?? '')
const mountOptions = { global: { mocks: { $gettext: gettext } } }
const [personal, team] = folders.value

beforeEach(() => {
  // A wide window, so that the list pane sits beside the table.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn()
  })) as unknown as typeof window.matchMedia
  // The sidebar opens and closes through the event bus of oCIS Web.
  isSideBarOpen.value = false
  eventBus.publish.mockReset()
  eventBus.publish.mockImplementation((topic: string) => {
    if (topic === 'sidebar.open') isSideBarOpen.value = true
    if (topic === 'sidebar.close') isSideBarOpen.value = false
  })
  URL.createObjectURL = vi.fn(() => 'blob:photo')
  URL.revokeObjectURL = vi.fn()
  listContacts.mockReset()
  createContact.mockReset()
  updateContact.mockReset()
  deleteContact.mockReset()
  getContact.mockReset()
  exportAddressBook.mockReset()
  importAddressBook.mockReset()
  createContactFolder.mockReset()
  updateContactFolder.mockReset()
  deleteContactFolder.mockReset()
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

// chooseOption picks an entry of an oc-select by its label. The stand-in is a
// native select whose options are numbered.
async function chooseOption(w: Awaited<ReturnType<typeof open>>, testId: string, label: string) {
  const select = w.find(`[data-testid="${testId}"]`)
  const option = select.findAll('option').find((o) => o.text() === label)!
  await select.setValue((option.element as HTMLOptionElement).value)
}

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
    await chooseOption(w, 'contact-sort', 'Company')
    const company = names(w)
    expect(company.indexOf('Albert Einstein')).toBeGreaterThanOrEqual(0)
    await chooseOption(w, 'contact-sort', 'Surname')
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
      await w.find('form').trigger('submit')
      await flushPromises()
      expect(w.find('[data-testid="group-rename-input"]').exists()).toBe(false)
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
      expect(w.find('[role="dialog"]').text()).toContain('Remove this group from its contacts?')
      await w.find('[data-testid="dialog-confirm"]').trigger('click')
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
      await w.find('[data-testid="dialog-confirm"]').trigger('click')
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

  describe('address books', () => {
    const books = (w: Awaited<ReturnType<typeof open>>) =>
      w.findAll('[data-testid="folder-item"] .name').map((n) => n.text())

    it('creates an address book and shows it', async () => {
      createContactFolder.mockResolvedValue({
        ...personal,
        id: 'new-book',
        displayName: 'Clients',
        'groupware.isDefault': false
      })
      const w = await open()
      await w.find('[data-testid="book-new"]').trigger('click')
      await w.find('[data-testid="book-new-input"]').setValue('  Clients ')
      await w.find('form').trigger('submit')
      await flushPromises()
      expect(createContactFolder).toHaveBeenCalledWith({ displayName: 'Clients' })
      expect(books(w)).toContain('Clients')
      expect(w.find('[data-testid="book-new-input"]').exists()).toBe(false)
    })

    it('renames the chosen address book', async () => {
      updateContactFolder.mockImplementation((id: string, input: { displayName: string }) =>
        Promise.resolve({ ...personal, id, ...input })
      )
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[0].trigger('click')
      await flushPromises()
      await w.find('[data-testid="book-rename"]').trigger('click')
      await w.find('[data-testid="book-rename-input"]').setValue('Friends and family')
      await w.find('form').trigger('submit')
      await flushPromises()
      expect(updateContactFolder).toHaveBeenCalledWith(personal.id, {
        displayName: 'Friends and family'
      })
      expect(books(w)[0]).toBe('Friends and family')
    })

    it('deletes an address book after a confirmation, but not the default one', async () => {
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[0].trigger('click')
      await flushPromises()
      expect(w.find('[data-testid="book-delete"]').exists()).toBe(false)
      createContactFolder.mockResolvedValue({
        ...personal,
        id: 'new-book',
        displayName: 'Clients',
        'groupware.isDefault': false
      })
      listContacts.mockResolvedValue([])
      deleteContactFolder.mockResolvedValue(true)
      await w.find('[data-testid="book-new"]').trigger('click')
      await w.find('[data-testid="book-new-input"]').setValue('Clients')
      await w.find('form').trigger('submit')
      await flushPromises()
      await w.findAll('[data-testid="folder-item"]')[2].trigger('click')
      await flushPromises()
      await w.find('[data-testid="book-delete"]').trigger('click')
      expect(deleteContactFolder).not.toHaveBeenCalled()
      await w.find('[data-testid="dialog-confirm"]').trigger('click')
      await flushPromises()
      expect(deleteContactFolder).toHaveBeenCalledWith('new-book')
      expect(books(w)).not.toContain('Clients')
    })

    it('offers no management for an address book that is shared with the user', async () => {
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[1].trigger('click')
      await flushPromises()
      expect(w.find('[data-testid="book-rename"]').exists()).toBe(false)
      expect(w.find('[data-testid="book-delete"]').exists()).toBe(false)
    })
  })

  describe('vCard files', () => {
    const chooseBook = async (w: Awaited<ReturnType<typeof open>>, i: number) => {
      await w.findAll('[data-testid="folder-item"]')[i].trigger('click')
      await flushPromises()
    }

    it('downloads the chosen address book as a .vcf file', async () => {
      exportAddressBook.mockResolvedValue(new Blob(['BEGIN:VCARD'], { type: 'text/vcard' }))
      const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
      const w = await open()
      expect(w.find('[data-testid="book-download"]').exists()).toBe(false)
      await chooseBook(w, 0)
      await w.find('[data-testid="book-download"]').trigger('click')
      await flushPromises()
      expect(exportAddressBook).toHaveBeenCalledWith(personal.id)
      expect(click).toHaveBeenCalledTimes(1)
      expect((click.mock.contexts[0] as HTMLAnchorElement).download).toBe('Contacts.vcf')
      click.mockRestore()
    })

    it('imports a chosen file, shows what failed and reloads the contacts', async () => {
      importAddressBook.mockResolvedValue(importReport)
      const w = await open()
      await chooseBook(w, 0)
      listContacts.mockClear()
      const input = w.find('[data-testid="book-import-input"]')
      const file = new File(['BEGIN:VCARD'], 'cards.vcf', { type: 'text/vcard' })
      Object.defineProperty(input.element, 'files', { value: [file] })
      await input.trigger('change')
      await flushPromises()
      expect(importAddressBook).toHaveBeenCalledWith(personal.id, file)
      expect(listContacts).toHaveBeenCalledWith(personal.id)
      const report = w.find('[data-testid="import-report"]')
      expect(report.text()).toContain('2')
      expect(report.find('[data-testid="import-failure"]').text()).toContain('missing END:VCARD')
      expect(w.get('[data-testid="import-summary"]').text()).toMatch(/\d+ created/)
      await w.find('[data-testid="import-report-close"]').trigger('click')
      expect(w.find('[data-testid="import-report"]').exists()).toBe(false)
    })

    it('offers no import into a read-only address book, but a download', async () => {
      const w = await open()
      await chooseBook(w, 1)
      expect(w.find('[data-testid="book-import-input"]').exists()).toBe(false)
      expect(w.find('[data-testid="book-download"]').exists()).toBe(true)
    })
  })
  describe('the layout', () => {
    it('has one h1 with the name of the chosen address book or group', async () => {
      const w = await open()
      expect(w.findAll('h1')).toHaveLength(1)
      expect(w.get('h1').text()).toBe('All contacts')
      await w.findAll('[data-testid="folder-item"]')[1].trigger('click')
      expect(w.get('h1').text()).toBe('Team')
      await w.find('[data-testid="group-all"]').trigger('click')
      await w.find('[data-testid="group-item"]').trigger('click')
      expect(w.get('h1').text()).not.toBe('Team')
    })

    it('lists the address books and groups in the list pane with their counts', async () => {
      const w = await open()
      const pane = w.get('[data-testid="list-pane"]')
      expect(pane.find('[data-testid="folder-all"] .gw-count').text()).toBe('3')
      const books = pane.findAll('[data-testid="folder-item"]')
      expect(books[0].find('.gw-count').text()).toBe('2')
      expect(books[1].find('.gw-count').text()).toBe('1')
      expect(pane.find('[data-testid="group-all"]').attributes('aria-current')).toBe('true')
    })

    it('shows who shares an address book and with which access, on a second line', async () => {
      const w = await open()
      const books = w.findAll('[data-testid="folder-item"]')
      expect(books[0].text()).not.toContain('Albert Einstein')
      expect(books[1].text()).toContain('Marie Curie')
      expect(books[1].text()).toContain('Read only')
    })

    it('marks the chosen address book and group for assistive technology', async () => {
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[0].trigger('click')
      expect(w.findAll('[data-testid="folder-item"]')[0].attributes('aria-current')).toBe('true')
      expect(w.find('[data-testid="folder-all"]').attributes('aria-current')).toBe('false')
    })

    it('shows the contacts in a table with their email, phone and company', async () => {
      const w = await open()
      const row = w.findAll('tbody tr').find((r) => r.text().includes('Marie'))!
      expect(row.text()).toContain('marie@example.org')
      expect(row.text()).toContain('+33 1 55 55 01 01')
      expect(row.text()).toContain('Sorbonne')
      expect(w.findAll('thead th').map((h) => h.text())).toEqual(
        expect.arrayContaining(['Name', 'Email', 'Phone', 'Company'])
      )
    })

    it('says how many contacts it shows', async () => {
      const w = await open()
      expect(w.get('[data-testid="contact-count"]').text()).toBe('3 contacts')
      await w.find('[data-testid="contact-search"]').setValue('albert')
      expect(w.get('[data-testid="contact-count"]').text()).toBe('One contact')
    })

    it('sorts descending when the sort is asked for again', async () => {
      const w = await open()
      w.findComponent({ name: 'OcTable' }).vm.$emit('sort', { sortBy: 'name', sortDir: 'desc' })
      await flushPromises()
      const desc = names(w)
      expect(desc).toEqual([...desc].sort((a, b) => b.localeCompare(a)))
    })
  })

  describe('the details sidebar', () => {
    it('opens through the event bus when a contact is chosen', async () => {
      const w = await open()
      expect(w.find('[data-testid="sidebar"]').exists()).toBe(false)
      await w.find('[data-testid="contact-item"]').trigger('click')
      expect(eventBus.publish).toHaveBeenCalledWith('sidebar.open')
      expect(w.find('[data-testid="sidebar"] [data-testid="contact-detail"]').exists()).toBe(true)
    })

    it('invites to choose a contact while none is chosen', async () => {
      const w = await open()
      isSideBarOpen.value = true
      await flushPromises()
      expect(w.get('[data-testid="sidebar"]').text()).toContain('Select a contact to see its details')
    })

    it('opens with an empty form for a new contact', async () => {
      const w = await open()
      await w.find('[data-testid="contact-new"]').trigger('click')
      expect(eventBus.publish).toHaveBeenCalledWith('sidebar.open')
      expect(w.find('[data-testid="sidebar"] [data-testid="contact-form"]').exists()).toBe(true)
    })

    it('drops a form that is open when the sidebar is closed', async () => {
      const w = await open()
      await w.find('[data-testid="contact-new"]').trigger('click')
      isSideBarOpen.value = false
      await flushPromises()
      isSideBarOpen.value = true
      await flushPromises()
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(false)
    })
  })

  describe('states', () => {
    it('says so, with a way forward, when an address book has no contacts', async () => {
      listContacts.mockResolvedValue([])
      const w = await open()
      const empty = w.get('[data-testid="no-contacts"]')
      expect(empty.attributes('data-icon')).toBe('contacts-book')
      expect(empty.text()).toContain('There are no contacts here yet.')
      await empty.get('button').trigger('click')
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(true)
    })

    it('offers no new contact from the empty state of a read-only address book', async () => {
      listContacts.mockResolvedValue([])
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[1].trigger('click')
      expect(w.get('[data-testid="no-contacts"]').find('button').exists()).toBe(false)
    })

    it('says so when nothing matches the search', async () => {
      const w = await open()
      await w.find('[data-testid="contact-search"]').setValue('zzz')
      expect(w.get('[data-testid="no-match"]').attributes('data-icon')).toBe('search')
      expect(w.find('table').exists()).toBe(false)
    })

    it('shows a spinner while the contacts load', async () => {
      listContacts.mockReturnValue(new Promise(() => {}))
      const w = mount(ContactsPage, mountOptions)
      await flushPromises()
      expect(w.find('[role="status"]').exists()).toBe(true)
      expect(w.find('table').exists()).toBe(false)
    })
  })

  describe('the buttons of the app bar', () => {
    it('has new contact as the filled primary button', async () => {
      const w = await open()
      const button = w
        .findAllComponents({ name: 'OcButton' })
        .find((c) => c.attributes('data-testid') === 'contact-new')!
      expect(button.props('variation')).toBe('primary')
      expect(button.props('appearance')).toBe('filled')
    })

    it('offers sharing, renaming and deleting only to the owner, and no delete for the default book', async () => {
      const w = await open()
      expect(w.find('[data-testid="book-share"]').exists()).toBe(false)
      expect(w.find('[data-testid="book-actions"]').exists()).toBe(false)
      await w.findAll('[data-testid="folder-item"]')[0].trigger('click')
      expect(w.find('[data-testid="book-share"]').exists()).toBe(true)
      expect(w.find('[data-testid="book-rename"]').exists()).toBe(true)
      expect(w.find('[data-testid="book-delete"]').exists()).toBe(false)
      await w.findAll('[data-testid="folder-item"]')[1].trigger('click')
      expect(w.find('[data-testid="book-share"]').exists()).toBe(false)
      expect(w.find('[data-testid="book-actions"]').exists()).toBe(false)
    })

    it('imports into the default address book while all contacts are shown', async () => {
      importAddressBook.mockResolvedValue(importReport)
      const w = await open()
      const input = w.find('[data-testid="book-import-input"]')
      expect(input.exists()).toBe(true)
      const file = new File(['BEGIN:VCARD'], 'cards.vcf', { type: 'text/vcard' })
      Object.defineProperty(input.element, 'files', { value: [file] })
      await input.trigger('change')
      await flushPromises()
      expect(importAddressBook).toHaveBeenCalledWith(personal.id, file)
    })

    it('offers no download for all contacts', async () => {
      const w = await open()
      expect(w.find('[data-testid="book-download"]').exists()).toBe(false)
    })

    it('shows the group actions instead of the address book actions for a group', async () => {
      const w = await open()
      await w.findAll('[data-testid="folder-item"]')[0].trigger('click')
      await w.find('[data-testid="group-item"]').trigger('click')
      expect(w.find('[data-testid="group-rename"]').exists()).toBe(true)
      expect(w.find('[data-testid="book-share"]').exists()).toBe(false)
    })
  })

  describe('the dialogs', () => {
    it('closes the address book dialog with Escape without creating anything', async () => {
      const w = await open()
      await w.find('[data-testid="book-new"]').trigger('click')
      expect(w.find('[role="dialog"]').exists()).toBe(true)
      await w.find('[role="dialog"]').trigger('keydown', { key: 'Escape' })
      expect(w.find('[role="dialog"]').exists()).toBe(false)
      expect(createContactFolder).not.toHaveBeenCalled()
    })

    it('does not save a blank name', async () => {
      const w = await open()
      await w.find('[data-testid="book-new"]').trigger('click')
      await w.find('[data-testid="book-new-input"]').setValue('   ')
      expect(w.get('[data-testid="book-new-save"]').attributes('disabled')).toBeDefined()
    })

    it('asks before deleting an address book, and Cancel leaves it', async () => {
      createContactFolder.mockResolvedValue({
        ...personal,
        id: 'new-book',
        displayName: 'Clients',
        'groupware.isDefault': false
      })
      const w = await open()
      await w.find('[data-testid="book-new"]').trigger('click')
      await w.find('[data-testid="book-new-input"]').setValue('Clients')
      await w.find('form').trigger('submit')
      await flushPromises()
      await w.find('[data-testid="book-delete"]').trigger('click')
      expect(w.find('[role="dialog"]').text()).toContain('Delete the address book Clients with all its contacts?')
      await w.find('[data-testid="dialog-cancel"]').trigger('click')
      expect(w.find('[role="dialog"]').exists()).toBe(false)
      expect(deleteContactFolder).not.toHaveBeenCalled()
    })
  })

  describe('the keyboard', () => {
    it('focuses the search with / and starts a new contact with n, but not while typing', async () => {
      const w = mount(ContactsPage, { ...mountOptions, attachTo: document.body })
      await flushPromises()
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
      expect(document.activeElement).toBe(w.get('[data-testid="contact-search"]').element)
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', bubbles: true }))
      await flushPromises()
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(false)
      ;(document.activeElement as HTMLElement).blur()
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', bubbles: true }))
      await flushPromises()
      expect(w.find('[data-testid="contact-form"]').exists()).toBe(true)
      w.unmount()
    })
  })
})
