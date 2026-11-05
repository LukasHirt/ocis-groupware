import { beforeEach, describe, expect, it } from 'vitest'
import { useHiddenCalendars } from '../../../src/calendar/visibility'

describe('useHiddenCalendars', () => {
  beforeEach(() => localStorage.clear())

  it('shows every calendar at first', () => {
    const { isHidden } = useHiddenCalendars()
    expect(isHidden('a')).toBe(false)
  })

  it('hides and shows a calendar, and remembers it', () => {
    const first = useHiddenCalendars()
    first.toggle('a')
    expect(first.isHidden('a')).toBe(true)
    expect(useHiddenCalendars().isHidden('a')).toBe(true)
    first.toggle('a')
    expect(useHiddenCalendars().isHidden('a')).toBe(false)
  })

  it('works when the browser refuses storage', () => {
    const original = Storage.prototype.getItem
    Storage.prototype.getItem = () => {
      throw new Error('denied')
    }
    try {
      const { isHidden, toggle } = useHiddenCalendars()
      toggle('a')
      expect(isHidden('a')).toBe(true)
    } finally {
      Storage.prototype.getItem = original
    }
  })
})
