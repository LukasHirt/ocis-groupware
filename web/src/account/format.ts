// A date the way the account page shows it: day, short month and year, in
// the language of the browser.
export function formatDay(value: Date | string): string {
  return new Date(value).toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: 'numeric'
  })
}
