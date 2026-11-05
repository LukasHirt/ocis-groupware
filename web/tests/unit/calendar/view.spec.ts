import { describe, expect, it } from 'vitest'
import { nextLabel, previousLabel } from '../../../src/calendar/view'

const same = (s: string) => s

describe('the labels of the toolbar', () => {
  it('name the step after the view', () => {
    expect(previousLabel('dayGridMonth', same)).toBe('Previous month')
    expect(previousLabel('timeGridWeek', same)).toBe('Previous week')
    expect(previousLabel('timeGridDay', same)).toBe('Previous day')
    expect(nextLabel('dayGridMonth', same)).toBe('Next month')
    expect(nextLabel('timeGridWeek', same)).toBe('Next week')
    expect(nextLabel('timeGridDay', same)).toBe('Next day')
  })
})
