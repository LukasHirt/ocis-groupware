import { describe, expect, it } from 'vitest'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'
import {
  addToGroup,
  inGroup,
  listGroupCards,
  listGroups,
  removeFromGroup,
  renameGroup
} from '../../src/contacts/groups'

const base = contact as unknown as GraphContact
const make = (categories: string[], groups: GraphContact['groupware.groups'] = []) =>
  ({ ...base, categories, 'groupware.groups': groups }) as GraphContact

describe('listGroups', () => {
  it('counts the contacts of each category and sorts by name', () => {
    const list = [make(['Work', 'friends']), make(['Friends']), make(['Work']), make([])]
    expect(listGroups(list)).toEqual([
      { name: 'Friends', count: 1 },
      { name: 'friends', count: 1 },
      { name: 'Work', count: 2 }
    ])
  })
})

describe('listGroupCards', () => {
  it('lists the group cards that contacts belong to, once each', () => {
    const g = { id: 'g1', displayName: 'Physicists' }
    expect(listGroupCards([make([], [g]), make([], [g]), make([])])).toEqual([
      { id: 'g1', name: 'Physicists', count: 2 }
    ])
  })
})

describe('changing categories', () => {
  it('adds a group once, ignoring blanks and case', () => {
    expect(addToGroup(['Work'], ' Friends ')).toEqual(['Work', 'Friends'])
    expect(addToGroup(['Work'], 'work')).toEqual(['Work'])
    expect(addToGroup(['Work'], '  ')).toEqual(['Work'])
  })

  it('removes a group', () => {
    expect(removeFromGroup(['Work', 'Friends'], 'Work')).toEqual(['Friends'])
  })

  it('renames a group without making a duplicate', () => {
    expect(renameGroup(['Work', 'Friends'], 'Work', 'Colleagues')).toEqual(['Colleagues', 'Friends'])
    expect(renameGroup(['Work', 'Friends'], 'Work', 'Friends')).toEqual(['Friends'])
  })
})

describe('inGroup', () => {
  it('matches a category or a group card', () => {
    const c = make(['Work'], [{ id: 'g1', displayName: 'Physicists' }])
    expect(inGroup(c, { kind: 'category', name: 'Work' })).toBe(true)
    expect(inGroup(c, { kind: 'category', name: 'Other' })).toBe(false)
    expect(inGroup(c, { kind: 'card', id: 'g1' })).toBe(true)
    expect(inGroup(c, null)).toBe(true)
  })
})
