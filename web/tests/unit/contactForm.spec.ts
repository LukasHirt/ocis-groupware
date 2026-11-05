import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'
import { buildInput, emptyDraft, toDraft } from '../../src/contacts/form'
import ContactForm from '../../src/contacts/ContactForm.vue'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const marie = contact as unknown as GraphContact

describe('toDraft', () => {
  it('copies the fields a form edits', () => {
    const d = toDraft(marie)
    expect(d.givenName).toBe('Marie')
    expect(d.emails).toEqual(['marie@example.org', 'm.curie@home.example'])
    expect(d.homePhones).toEqual(['+33 1 55 55 02 02', '+33 1 55 55 03 03'])
    expect(d.homeAddress.city).toBe('Sceaux')
    expect(d.children).toEqual(['Irene'])
  })

  it('shows a birthday as a date, and none for a birthday without a year', () => {
    expect(toDraft({ ...marie, birthday: '1879-03-14T00:00:00Z' }).birthday).toBe('1879-03-14')
    expect(toDraft(marie).birthday).toBe('')
    expect(toDraft({ ...marie, birthday: null }).birthday).toBe('')
  })

  it('gives lists one empty row to type into', () => {
    const d = toDraft({ ...marie, emailAddresses: [], children: [] })
    expect(d.emails).toEqual([''])
    expect(d.children).toEqual([''])
  })
})

describe('buildInput', () => {
  it('sends nothing for an unchanged draft', () => {
    expect(buildInput(toDraft(marie), toDraft(marie))).toEqual({})
  })

  it('sends only the fields that changed', () => {
    const d = toDraft(marie)
    d.jobTitle = 'Director'
    d.emails = ['marie@example.org', 'new@example.org']
    expect(buildInput(toDraft(marie), d)).toEqual({
      jobTitle: 'Director',
      emailAddresses: [
        { name: 'marie@example.org', address: 'marie@example.org' },
        { name: 'new@example.org', address: 'new@example.org' }
      ]
    })
  })

  it('ignores blank rows and trims text', () => {
    const d = toDraft(marie)
    d.homePhones = ['  +33 1 55 55 02 02 ', '', '+33 1 55 55 03 03']
    d.surname = ' Curie '
    expect(buildInput(toDraft(marie), d)).toEqual({})
  })

  it('clears a field that was emptied', () => {
    const d = toDraft(marie)
    d.nickName = ''
    d.children = ['']
    d.homeAddress = { street: '', city: '', state: '', countryOrRegion: '', postalCode: '' }
    expect(buildInput(toDraft(marie), d)).toEqual({
      nickName: '',
      children: [],
      homeAddress: { street: '', city: '', state: '', countryOrRegion: '', postalCode: '' }
    })
  })

  it('sends a birthday as a date and null when it is removed', () => {
    const had = { ...marie, birthday: '1879-03-14T00:00:00Z' }
    const d = toDraft(had)
    d.birthday = '1900-01-02'
    expect(buildInput(toDraft(had), d)).toEqual({ birthday: '1900-01-02T00:00:00Z' })
    d.birthday = ''
    expect(buildInput(toDraft(had), d)).toEqual({ birthday: null })
  })

  it('leaves a birthday without a year alone', () => {
    expect(buildInput(toDraft(marie), toDraft(marie))).not.toHaveProperty('birthday')
  })

  it('sends every filled field for a new contact', () => {
    const d = emptyDraft()
    d.givenName = 'Ada'
    d.surname = 'Lovelace'
    d.emails = ['ada@example.org', '']
    expect(buildInput(emptyDraft(), d)).toEqual({
      givenName: 'Ada',
      surname: 'Lovelace',
      emailAddresses: [{ name: 'ada@example.org', address: 'ada@example.org' }]
    })
  })
})

describe('the contact form', () => {
  const folders = [
    { id: 'a', displayName: 'Contacts' },
    { id: 'b', displayName: 'Clients' }
  ] as never[]
  const mountIt = (props: Record<string, unknown> = {}) =>
    mount(ContactForm, {
      props: { folders: folders.slice(0, 1), ...props },
      global: { mocks: { $gettext: (s: string) => s } }
    })
  const find = (w: ReturnType<typeof mountIt>, id: string) => w.find(`[data-testid="${id}"]`)

  it('starts a new contact empty, with a hint, and cannot be saved yet', () => {
    const w = mountIt()
    expect(find(w, 'form-hint').exists()).toBe(true)
    expect(find(w, 'form-save').attributes('disabled')).toBeDefined()
    expect(find(w, 'field-givenName').exists()).toBe(true)
  })

  it('saves what was typed into the address book it was started for', async () => {
    const w = mountIt()
    await find(w, 'field-givenName').setValue('Ada')
    await find(w, 'emails-0').setValue('ada@example.org')
    expect(find(w, 'form-hint').exists()).toBe(false)
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toEqual([
      [
        { givenName: 'Ada', emailAddresses: [{ name: 'ada@example.org', address: 'ada@example.org' }] },
        'a'
      ]
    ])
  })

  it('lets the user choose the address book of a new contact when there are several', async () => {
    const w = mountIt({ folders })
    const select = find(w, 'form-folder')
    expect(select.exists()).toBe(true)
    const option = select.findAll('option').find((o) => o.text() === 'Clients')!
    await select.setValue((option.element as HTMLOptionElement).value)
    await find(w, 'field-givenName').setValue('Ada')
    await w.find('form').trigger('submit')
    expect(w.emitted('save')![0][1]).toBe('b')
    expect(mountIt({ folders: folders.slice(0, 1) }).find('[data-testid="form-folder"]').exists()).toBe(false)
  })

  it('shows no choice of address book while editing', () => {
    const w = mountIt({ contact: marie, folders })
    expect(find(w, 'form-folder').exists()).toBe(false)
    expect(find(w, 'form-hint').exists()).toBe(false)
  })

  it('keeps the rarely used name fields behind a button, unless the contact has one', async () => {
    const fresh = mountIt()
    expect(find(fresh, 'field-nickName').exists()).toBe(false)
    await find(fresh, 'name-more').trigger('click')
    expect(find(fresh, 'field-nickName').exists()).toBe(true)
    expect(find(fresh, 'name-more').attributes('aria-expanded')).toBe('true')
    // Marie has a nickname and a middle name.
    expect(find(mountIt({ contact: marie }), 'field-nickName').exists()).toBe(true)
  })

  it('opens the home address for a new contact and the addresses that hold data', async () => {
    const fresh = mountIt()
    expect(find(fresh, 'homeAddress-city').exists()).toBe(true)
    expect(find(fresh, 'businessAddress-city').exists()).toBe(false)
    await find(fresh, 'businessAddress-toggle').trigger('click')
    expect(find(fresh, 'businessAddress-city').exists()).toBe(true)

    const edit = mountIt({ contact: marie })
    expect(find(edit, 'homeAddress-city').exists()).toBe(true)
    expect(find(edit, 'businessAddress-city').exists()).toBe(true)
    expect(find(edit, 'otherAddress-city').exists()).toBe(false)
    // An address that is closed again keeps what was typed.
    await find(edit, 'homeAddress-toggle').trigger('click')
    expect(find(edit, 'homeAddress-city').exists()).toBe(false)
    await find(edit, 'homeAddress-toggle').trigger('click')
    expect((find(edit, 'homeAddress-city').element as HTMLInputElement).value).toBe('Sceaux')
  })

  it('opens the more-details group for a contact that has a value in it', async () => {
    expect(find(mountIt(), 'field-businessHomePage').exists()).toBe(false)
    expect(find(mountIt({ contact: marie }), 'field-businessHomePage').exists()).toBe(true)
    const fresh = mountIt()
    await find(fresh, 'details-more').trigger('click')
    expect(find(fresh, 'field-businessHomePage').exists()).toBe(true)
  })

  it('adds and removes rows of a field with several values, and keeps one row', async () => {
    const w = mountIt({ contact: marie })
    expect(find(w, 'emails-1').exists()).toBe(true)
    await find(w, 'emails-add').trigger('click')
    expect(find(w, 'emails-2').exists()).toBe(true)
    await find(w, 'emails-remove-0').trigger('click')
    await find(w, 'emails-remove-0').trigger('click')
    await find(w, 'emails-remove-0').trigger('click')
    expect(find(w, 'emails-0').exists()).toBe(true)
    expect(find(w, 'emails-1').exists()).toBe(false)
  })

  it('saves only what changed in an edited contact and not an unchanged one', async () => {
    const w = mountIt({ contact: marie })
    expect(find(w, 'form-save').attributes('disabled')).toBeDefined()
    await find(w, 'field-jobTitle').setValue('Director')
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toEqual([[{ jobTitle: 'Director' }, marie.parentFolderId]])
  })

  it('shows a note for a birthday without a year', () => {
    const w = mountIt({ contact: marie })
    expect(w.text()).toContain('birthday without a year')
  })

  it('asks to cancel', async () => {
    const w = mountIt()
    await find(w, 'form-cancel').trigger('click')
    expect(w.emitted('cancel')).toHaveLength(1)
  })
})
