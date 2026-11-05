// zoneNames lists the IANA zones the browser knows, with UTC, and any extra
// name that has to be selectable, such as the zone an event already has.
export function zoneNames(...extra: string[]): string[] {
  let names: string[] = []
  try {
    names = Intl.supportedValuesOf('timeZone')
  } catch {
    names = []
  }
  return [...new Set(['UTC', ...names, ...extra.filter((z) => z !== '')])].sort((a, b) =>
    a === 'UTC' ? -1 : b === 'UTC' ? 1 : a.localeCompare(b)
  )
}

// localZone is the browser's own zone.
export function localZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}
