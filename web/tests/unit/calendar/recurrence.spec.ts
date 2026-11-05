import { describe, expect, it } from 'vitest'
import { Info } from 'luxon'
import {
  defaultRecurrence,
  describeRecurrence,
  type RecurrenceForm,
  recurrenceFromForm,
  recurrenceFormFromEvent,
  recurrenceProblem
} from '../../../src/calendar/recurrence'
import type { PatternedRecurrence } from '../../../src/api/types'
import eventSeries from '../../../../internal/graphapi/testdata/contract/event-series.json'

// 2026-11-05 is a Thursday, the first one of the month is the 5th.
const start = '2026-11-05'

describe('defaultRecurrence', () => {
  it('does not repeat, and takes its day values from the start date', () => {
    expect(defaultRecurrence(start)).toMatchObject({
      frequency: 'none',
      interval: 1,
      daysOfWeek: ['thursday'],
      dayOfMonth: 5,
      month: 11,
      index: 'first',
      monthlyBy: 'day',
      end: 'never'
    })
  })

  it('counts which weekday of the month the start date is, with the last as last', () => {
    expect(defaultRecurrence('2026-11-12').index).toBe('second')
    expect(defaultRecurrence('2026-11-26').index).toBe('fourth')
    // 30 November 2026 is a Monday, and the fifth one, so also the last.
    expect(defaultRecurrence('2026-11-30').index).toBe('last')
  })
})

describe('recurrenceFromForm', () => {
  const base = defaultRecurrence(start)

  it('is null for an event that does not repeat', () => {
    expect(recurrenceFromForm(base, start, 'UTC')).toBeNull()
  })

  it('writes a daily pattern with an interval and no end', () => {
    const r = recurrenceFromForm({ ...base, frequency: 'daily', interval: 3 }, start, 'UTC')!
    expect(r.pattern).toMatchObject({ type: 'daily', interval: 3 })
    expect(r.range).toEqual({
      type: 'noEnd',
      startDate: start,
      recurrenceTimeZone: 'UTC',
      numberOfOccurrences: 0
    })
  })

  it('writes weekly with its days and the first day of the week', () => {
    const r = recurrenceFromForm(
      { ...base, frequency: 'weekly', daysOfWeek: ['monday', 'thursday'] },
      start,
      'Europe/Prague'
    )!
    expect(r.pattern).toMatchObject({
      type: 'weekly',
      daysOfWeek: ['monday', 'thursday'],
      firstDayOfWeek: 'monday'
    })
    expect(r.range.recurrenceTimeZone).toBe('Europe/Prague')
  })

  it('writes monthly by day of the month or by weekday', () => {
    const byDay = recurrenceFromForm({ ...base, frequency: 'monthly', dayOfMonth: 15 }, start, 'UTC')!
    expect(byDay.pattern).toMatchObject({ type: 'absoluteMonthly', dayOfMonth: 15 })
    const byWeekday = recurrenceFromForm(
      { ...base, frequency: 'monthly', monthlyBy: 'weekday', index: 'last', daysOfWeek: ['friday'] },
      start,
      'UTC'
    )!
    expect(byWeekday.pattern).toMatchObject({
      type: 'relativeMonthly',
      index: 'last',
      daysOfWeek: ['friday']
    })
  })

  it('writes yearly with a month', () => {
    const r = recurrenceFromForm({ ...base, frequency: 'yearly', month: 3, dayOfMonth: 8 }, start, 'UTC')!
    expect(r.pattern).toMatchObject({ type: 'absoluteYearly', month: 3, dayOfMonth: 8 })
    const rel = recurrenceFromForm(
      { ...base, frequency: 'yearly', monthlyBy: 'weekday', month: 3, index: 'second', daysOfWeek: ['monday'] },
      start,
      'UTC'
    )!
    expect(rel.pattern).toMatchObject({ type: 'relativeYearly', month: 3, index: 'second' })
  })

  it('writes the end as a count or a date', () => {
    const count = recurrenceFromForm({ ...base, frequency: 'daily', end: 'count', count: 5 }, start, 'UTC')!
    expect(count.range).toMatchObject({ type: 'numbered', numberOfOccurrences: 5 })
    const date = recurrenceFromForm(
      { ...base, frequency: 'daily', end: 'date', endDate: '2026-12-31' },
      start,
      'UTC'
    )!
    expect(date.range).toMatchObject({ type: 'endDate', endDate: '2026-12-31' })
  })
})

describe('recurrenceFormFromEvent', () => {
  it('reads the pattern of a series', () => {
    const form = recurrenceFormFromEvent(eventSeries.recurrence as PatternedRecurrence, '2026-11-06')
    expect(form).toMatchObject({
      frequency: 'weekly',
      interval: 1,
      daysOfWeek: ['friday'],
      end: 'count',
      count: 3
    })
  })

  it('takes a relative pattern as monthly by weekday', () => {
    const form = recurrenceFormFromEvent(
      {
        pattern: {
          type: 'relativeMonthly',
          interval: 2,
          month: 0,
          dayOfMonth: 0,
          daysOfWeek: ['tuesday'],
          firstDayOfWeek: 'sunday',
          index: 'third'
        },
        range: { type: 'endDate', startDate: start, endDate: '2027-01-31', numberOfOccurrences: 0 }
      },
      start
    )
    expect(form).toMatchObject({
      frequency: 'monthly',
      monthlyBy: 'weekday',
      interval: 2,
      index: 'third',
      daysOfWeek: ['tuesday'],
      end: 'date',
      endDate: '2027-01-31'
    })
  })

  it('does not repeat without a recurrence', () => {
    expect(recurrenceFormFromEvent(null, start).frequency).toBe('none')
  })

  it('is the same when written and read again', () => {
    const form = { ...defaultRecurrence(start), frequency: 'yearly' as const, month: 7, dayOfMonth: 14, end: 'count' as const, count: 4 }
    const written = recurrenceFromForm(form, start, 'UTC')!
    expect(recurrenceFormFromEvent(written, start)).toMatchObject(form)
  })
})

describe('recurrenceProblem', () => {
  const base = { ...defaultRecurrence(start), frequency: 'daily' as const }

  it('accepts a sound form', () => {
    expect(recurrenceProblem(base, start)).toBeUndefined()
    expect(recurrenceProblem(defaultRecurrence(start), start)).toBeUndefined()
  })

  it('refuses an interval outside 1 to 99', () => {
    expect(recurrenceProblem({ ...base, interval: 0 }, start)).toBe('interval')
    expect(recurrenceProblem({ ...base, interval: 100 }, start)).toBe('interval')
  })

  it('refuses a weekly pattern without days', () => {
    expect(recurrenceProblem({ ...base, frequency: 'weekly', daysOfWeek: [] }, start)).toBe('days')
  })

  it('refuses a count outside 1 to 999 and an end date before the start', () => {
    expect(recurrenceProblem({ ...base, end: 'count', count: 0 }, start)).toBe('count')
    expect(recurrenceProblem({ ...base, end: 'date', endDate: '2026-11-01' }, start)).toBe('endDate')
    expect(recurrenceProblem({ ...base, end: 'date', endDate: '' }, start)).toBe('endDate')
  })

  it('refuses a day of the month that is not 1 to 31', () => {
    expect(recurrenceProblem({ ...base, frequency: 'monthly', dayOfMonth: 32 }, start)).toBe('dayOfMonth')
  })
})

describe('describeRecurrence', () => {
  const same = (s: string, vars: Record<string, string> = {}) =>
    s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars[k] ?? '')
  const form = (change: Partial<RecurrenceForm>): RecurrenceForm => ({
    ...defaultRecurrence('2026-11-10'),
    ...change
  })
  const names = Info.weekdays('long')

  it('says nothing for an event that does not repeat', () => {
    expect(describeRecurrence(form({ frequency: 'none' }), same)).toBe('')
  })

  it('writes daily and weekly patterns', () => {
    expect(describeRecurrence(form({ frequency: 'daily' }), same)).toBe('Every day')
    expect(describeRecurrence(form({ frequency: 'daily', interval: 3 }), same)).toBe('Every 3 days')
    expect(describeRecurrence(form({ frequency: 'weekly', daysOfWeek: ['monday', 'friday'] }), same)).toBe(
      `Every week on ${names[0]}, ${names[4]}`
    )
    expect(describeRecurrence(form({ frequency: 'weekly', interval: 2, daysOfWeek: ['tuesday'] }), same)).toBe(
      `Every 2 weeks on ${names[1]}`
    )
  })

  it('writes monthly and yearly patterns', () => {
    expect(describeRecurrence(form({ frequency: 'monthly', dayOfMonth: 5 }), same)).toBe('Every month on day 5')
    expect(
      describeRecurrence(form({ frequency: 'monthly', monthlyBy: 'weekday', index: 'second', daysOfWeek: ['tuesday'] }), same)
    ).toBe(`Every month on the second ${names[1]}`)
    expect(describeRecurrence(form({ frequency: 'yearly', month: 11, dayOfMonth: 10 }), same)).toBe(
      `Every year on ${Info.months('long')[10]} 10`
    )
  })

  it('adds how it ends', () => {
    expect(describeRecurrence(form({ frequency: 'daily', end: 'count', count: 4 }), same)).toBe('Every day, 4 times')
    expect(describeRecurrence(form({ frequency: 'daily', end: 'date', endDate: '2026-12-24' }), same)).toMatch(
      /^Every day, until .*2026/
    )
  })
})
