import { DateTime, Info } from 'luxon'
import type { PatternedRecurrence } from '../api/types'

export type Frequency = 'none' | 'daily' | 'weekly' | 'monthly' | 'yearly'

// RecurrenceForm is what the dialog edits for the pattern of a series. Monthly
// and yearly patterns are either on a day of the month ("the 5th") or on a
// weekday of it ("the second Thursday").
export interface RecurrenceForm {
  frequency: Frequency
  interval: number
  daysOfWeek: string[]
  dayOfMonth: number
  month: number
  index: string
  monthlyBy: 'day' | 'weekday'
  end: 'never' | 'count' | 'date'
  count: number
  endDate: string
}

export const weekdays = [
  'monday',
  'tuesday',
  'wednesday',
  'thursday',
  'friday',
  'saturday',
  'sunday'
]
export const indexes = ['first', 'second', 'third', 'fourth', 'last']

function startOf(startDate: string): DateTime {
  return DateTime.fromISO(startDate, { zone: 'utc' })
}

// defaultRecurrence is a form that does not repeat, with the values a pattern
// would start from when the user picks one: the weekday, day of the month and
// month of the start date.
export function defaultRecurrence(startDate: string): RecurrenceForm {
  const start = startOf(startDate)
  const valid = start.isValid
  const ordinal = valid ? Math.ceil(start.day / 7) : 1
  return {
    frequency: 'none',
    interval: 1,
    daysOfWeek: [weekdays[valid ? start.weekday - 1 : 0]],
    dayOfMonth: valid ? start.day : 1,
    month: valid ? start.month : 1,
    index: ordinal >= 5 ? 'last' : indexes[ordinal - 1],
    monthlyBy: 'day',
    end: 'never',
    count: 10,
    endDate: ''
  }
}

export function recurrenceFormFromEvent(
  recurrence: PatternedRecurrence | null,
  startDate: string
): RecurrenceForm {
  const form = defaultRecurrence(startDate)
  if (!recurrence) {
    return form
  }
  const { pattern, range } = recurrence
  switch (pattern.type) {
    case 'daily':
      form.frequency = 'daily'
      break
    case 'weekly':
      form.frequency = 'weekly'
      break
    case 'absoluteMonthly':
      form.frequency = 'monthly'
      break
    case 'relativeMonthly':
      form.frequency = 'monthly'
      form.monthlyBy = 'weekday'
      break
    case 'absoluteYearly':
      form.frequency = 'yearly'
      break
    case 'relativeYearly':
      form.frequency = 'yearly'
      form.monthlyBy = 'weekday'
      break
    default:
      return form
  }
  form.interval = pattern.interval || 1
  if (pattern.daysOfWeek.length > 0) {
    form.daysOfWeek = [...pattern.daysOfWeek]
  }
  if (pattern.dayOfMonth > 0) {
    form.dayOfMonth = pattern.dayOfMonth
  }
  if (pattern.month > 0) {
    form.month = pattern.month
  }
  if (indexes.includes(pattern.index)) {
    form.index = pattern.index
  }
  if (range.type === 'numbered') {
    form.end = 'count'
    form.count = range.numberOfOccurrences
  } else if (range.type === 'endDate') {
    form.end = 'date'
    form.endDate = range.endDate ?? ''
  }
  return form
}

// recurrenceFromForm builds the Graph recurrence, or null for an event that
// does not repeat. The range starts on the start date of the event and runs
// in its zone.
export function recurrenceFromForm(
  form: RecurrenceForm,
  startDate: string,
  timeZone: string
): PatternedRecurrence | null {
  if (form.frequency === 'none') {
    return null
  }
  const relative = form.monthlyBy === 'weekday'
  const type = {
    daily: 'daily',
    weekly: 'weekly',
    monthly: relative ? 'relativeMonthly' : 'absoluteMonthly',
    yearly: relative ? 'relativeYearly' : 'absoluteYearly'
  }[form.frequency]
  const usesDays = form.frequency === 'weekly' || ((form.frequency === 'monthly' || form.frequency === 'yearly') && relative)
  const usesDayOfMonth = (form.frequency === 'monthly' || form.frequency === 'yearly') && !relative
  return {
    pattern: {
      type,
      interval: form.interval,
      month: form.frequency === 'yearly' ? form.month : 0,
      dayOfMonth: usesDayOfMonth ? form.dayOfMonth : 0,
      daysOfWeek: usesDays ? weekdays.filter((d) => form.daysOfWeek.includes(d)) : [],
      firstDayOfWeek: 'monday',
      index: relative ? form.index : 'first'
    },
    range: {
      type: { never: 'noEnd', count: 'numbered', date: 'endDate' }[form.end],
      startDate,
      ...(form.end === 'date' ? { endDate: form.endDate } : {}),
      recurrenceTimeZone: timeZone,
      numberOfOccurrences: form.end === 'count' ? form.count : 0
    }
  }
}

export type RecurrenceProblem = 'interval' | 'days' | 'dayOfMonth' | 'count' | 'endDate'

// recurrenceProblem names the first field the server would refuse.
export function recurrenceProblem(form: RecurrenceForm, startDate: string): RecurrenceProblem | undefined {
  if (form.frequency === 'none') {
    return undefined
  }
  if (!Number.isInteger(form.interval) || form.interval < 1 || form.interval > 99) {
    return 'interval'
  }
  const needsDays =
    form.frequency === 'weekly' || ((form.frequency === 'monthly' || form.frequency === 'yearly') && form.monthlyBy === 'weekday')
  if (needsDays && form.daysOfWeek.length === 0) {
    return 'days'
  }
  const needsDay = (form.frequency === 'monthly' || form.frequency === 'yearly') && form.monthlyBy === 'day'
  if (needsDay && (!Number.isInteger(form.dayOfMonth) || form.dayOfMonth < 1 || form.dayOfMonth > 31)) {
    return 'dayOfMonth'
  }
  if (form.end === 'count' && (!Number.isInteger(form.count) || form.count < 1 || form.count > 999)) {
    return 'count'
  }
  if (form.end === 'date' && (form.endDate === '' || form.endDate < startDate)) {
    return 'endDate'
  }
  return undefined
}

type Gettext = (message: string, vars?: Record<string, string>) => string

// describeRecurrence says in words how a series repeats, for the details of an
// event: "Every week on Monday, Friday, 4 times".
export function describeRecurrence(form: RecurrenceForm, $gettext: Gettext): string {
  if (form.frequency === 'none') {
    return ''
  }
  const n = String(form.interval)
  const one = form.interval === 1
  const names = Info.weekdays('long')
  const days = weekdays
    .filter((d) => form.daysOfWeek.includes(d))
    .map((d) => names[weekdays.indexOf(d)])
    .join(', ')
  const month = Info.months('long')[form.month - 1] ?? ''
  const which = {
    first: $gettext('first'),
    second: $gettext('second'),
    third: $gettext('third'),
    fourth: $gettext('fourth'),
    last: $gettext('last')
  }[form.index] ?? form.index
  const weekday = form.monthlyBy === 'weekday'
  let text = ''
  if (form.frequency === 'daily') {
    text = one ? $gettext('Every day') : $gettext('Every %{n} days', { n })
  } else if (form.frequency === 'weekly') {
    text = one
      ? $gettext('Every week on %{days}', { days })
      : $gettext('Every %{n} weeks on %{days}', { n, days })
  } else if (form.frequency === 'monthly') {
    const vars = { n, day: String(form.dayOfMonth), which, days }
    text = weekday
      ? one
        ? $gettext('Every month on the %{which} %{days}', vars)
        : $gettext('Every %{n} months on the %{which} %{days}', vars)
      : one
        ? $gettext('Every month on day %{day}', vars)
        : $gettext('Every %{n} months on day %{day}', vars)
  } else {
    const vars = { n, day: String(form.dayOfMonth), which, days, month }
    text = weekday
      ? one
        ? $gettext('Every year on the %{which} %{days} of %{month}', vars)
        : $gettext('Every %{n} years on the %{which} %{days} of %{month}', vars)
      : one
        ? $gettext('Every year on %{month} %{day}', vars)
        : $gettext('Every %{n} years on %{month} %{day}', vars)
  }
  if (form.end === 'count') {
    return `${text}, ${$gettext('%{n} times', { n: String(form.count) })}`
  }
  if (form.end === 'date' && form.endDate) {
    const date = DateTime.fromISO(form.endDate).toLocaleString(DateTime.DATE_FULL)
    return `${text}, ${$gettext('until %{date}', { date })}`
  }
  return text
}
