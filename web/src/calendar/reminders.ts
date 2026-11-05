// The choices offered for a reminder, in minutes before the start.
const choices = [0, 5, 10, 15, 30, 60, 120, 1440, 2880, 10080]

type Gettext = (message: string, vars?: Record<string, string>) => string

// reminderOptions is the list of choices, with the value an event already has
// when that is not one of them, so it is shown and kept as it is.
export function reminderOptions(current: number | null): number[] {
  if (current === null || choices.includes(current)) {
    return choices
  }
  return [...choices, current].sort((a, b) => a - b)
}

export function reminderLabel(minutes: number, $gettext: Gettext): string {
  if (minutes === 0) {
    return $gettext('At the time of the event')
  }
  const units: [number, string, string][] = [
    [10080, '1 week before', '%{n} weeks before'],
    [1440, '1 day before', '%{n} days before'],
    [60, '1 hour before', '%{n} hours before']
  ]
  for (const [size, one, many] of units) {
    if (minutes % size === 0) {
      const n = minutes / size
      return n === 1 ? $gettext(one) : $gettext(many, { n: String(n) })
    }
  }
  return minutes === 1 ? $gettext('1 minute before') : $gettext('%{n} minutes before', { n: String(minutes) })
}
