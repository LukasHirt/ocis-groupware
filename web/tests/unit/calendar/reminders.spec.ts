import { describe, expect, it } from 'vitest'
import { reminderLabel, reminderOptions } from '../../../src/calendar/reminders'

const gettext = (s: string, vars?: Record<string, string>) =>
  s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')

describe('reminderOptions', () => {
  it('offers the usual choices in order', () => {
    expect(reminderOptions(null)).toEqual([0, 5, 10, 15, 30, 60, 120, 1440, 2880, 10080])
  })

  it('adds the value an event already has when it is not among them', () => {
    expect(reminderOptions(45)).toEqual([0, 5, 10, 15, 30, 45, 60, 120, 1440, 2880, 10080])
    expect(reminderOptions(15)).toEqual(reminderOptions(null))
  })
})

describe('reminderLabel', () => {
  it('says minutes, hours, days and weeks the way a person would', () => {
    expect(reminderLabel(0, gettext)).toBe('At the time of the event')
    expect(reminderLabel(1, gettext)).toBe('1 minute before')
    expect(reminderLabel(15, gettext)).toBe('15 minutes before')
    expect(reminderLabel(60, gettext)).toBe('1 hour before')
    expect(reminderLabel(120, gettext)).toBe('2 hours before')
    expect(reminderLabel(1440, gettext)).toBe('1 day before')
    expect(reminderLabel(2880, gettext)).toBe('2 days before')
    expect(reminderLabel(10080, gettext)).toBe('1 week before')
    expect(reminderLabel(90, gettext)).toBe('90 minutes before')
  })
})
