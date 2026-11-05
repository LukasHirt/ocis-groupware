import { describe, expect, it } from 'vitest'
import contacts from '../../../internal/graphapi/testdata/contract/contacts.json'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'
import { contactName, filterContacts, sortContacts } from '../../src/contacts/model'

const base = contact as unknown as GraphContact
const make = (over: Partial<GraphContact>): GraphContact => ({ ...base, ...over })

describe('contactName', () => {
  it('prefers the display name', () => {
    expect(contactName(base)).toBe('Dr. Marie Salomea Curie')
  })

  it('falls back to the parts of the name, then email, then company', () => {
    expect(contactName(make({ displayName: '' }))).toBe('Marie Curie')
    expect(contactName(make({ displayName: '', givenName: '', surname: '' }))).toBe(
      'marie@example.org'
    )
    expect(
      contactName(make({ displayName: '', givenName: '', surname: '', emailAddresses: [] }))
    ).toBe('Sorbonne')
  })

  it('says so for a contact without any of them', () => {
    const empty = make({
      displayName: '',
      givenName: '',
      surname: '',
      emailAddresses: [],
      companyName: ''
    })
    expect(contactName(empty)).toBe('')
  })
})

describe('filterContacts', () => {
  const list = contacts.value as unknown as GraphContact[]

  it('keeps everything for an empty query', () => {
    expect(filterContacts(list, '  ')).toHaveLength(list.length)
  })

  it('matches names, companies, emails, phones and categories without regard to case', () => {
    expect(filterContacts(list, 'EINSTEIN').map(contactName)).toContain('Albert Einstein')
    expect(filterContacts(list, 'patent office').length).toBeGreaterThan(0)
    expect(filterContacts(list, '+41 79').length).toBeGreaterThan(0)
    expect(filterContacts(list, 'friends').length).toBeGreaterThan(0)
  })

  it('requires every word to match', () => {
    expect(filterContacts(list, 'albert nobody')).toEqual([])
    expect(filterContacts(list, 'einstein albert').length).toBeGreaterThan(0)
  })
})

describe('sortContacts', () => {
  const a = make({ id: 'a', displayName: 'Zoe Adams', givenName: 'Zoe', surname: 'Adams', companyName: 'Zed' })
  const b = make({ id: 'b', displayName: 'Adam Zimmer', givenName: 'Adam', surname: 'Zimmer', companyName: '' })
  const c = make({ id: 'c', displayName: 'émile Bern', givenName: 'émile', surname: 'Bern', companyName: 'Acme' })

  it('sorts by display name', () => {
    expect(sortContacts([a, b, c], 'name').map((x) => x.id)).toEqual(['b', 'c', 'a'])
  })

  it('sorts by surname, ignoring accents and case', () => {
    expect(sortContacts([b, a, c], 'surname').map((x) => x.id)).toEqual(['a', 'c', 'b'])
  })

  it('sorts by company and puts contacts without one last', () => {
    expect(sortContacts([b, a, c], 'company').map((x) => x.id)).toEqual(['c', 'a', 'b'])
  })

  it('sorts in reverse and still puts contacts without a value last', () => {
    expect(sortContacts([b, a, c], 'name', true).map((x) => x.id)).toEqual(['a', 'c', 'b'])
    expect(sortContacts([b, a, c], 'company', true).map((x) => x.id)).toEqual(['a', 'c', 'b'])
  })

  it('does not change its input', () => {
    const input = [a, b, c]
    sortContacts(input, 'name')
    expect(input.map((x) => x.id)).toEqual(['a', 'b', 'c'])
  })
})
