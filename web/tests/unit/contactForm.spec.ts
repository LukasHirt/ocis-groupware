import { describe, expect, it } from 'vitest'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'
import { buildInput, emptyDraft, toDraft } from '../../src/contacts/form'

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
