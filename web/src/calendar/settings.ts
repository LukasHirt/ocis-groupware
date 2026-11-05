import { DateTime } from 'luxon'
import { ref } from 'vue'
import { useUserKey } from '../composables/useUserKey'
import { localZone } from './zones'

// CalendarSettings are the choices of one user about how the grid looks. They
// stay in the browser, not on the server.
export interface CalendarSettings {
  // An IANA zone, or empty for the browser's own.
  timeZone: string
  // 0 for Sunday to 6 for Saturday.
  firstDay: number
  weekNumbers: boolean
}

export const defaultSettings: CalendarSettings = { timeZone: '', firstDay: 1, weekNumbers: false }

const keyOf = (user: string) => `groupware.calendarSettings.${user}`

const knownZone = (zone: string) => DateTime.local().setZone(zone).isValid

// loadSettings reads the stored settings. A value that is missing or makes no
// sense falls back to its default, so the page works with any content.
export function loadSettings(user: string): CalendarSettings {
  let stored: Partial<Record<keyof CalendarSettings, unknown>> = {}
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(keyOf(user)) ?? '{}')
    if (parsed && typeof parsed === 'object') {
      stored = parsed
    }
  } catch {
    stored = {}
  }
  const { timeZone, firstDay, weekNumbers } = stored
  return {
    timeZone: typeof timeZone === 'string' && (timeZone === '' || knownZone(timeZone)) ? timeZone : defaultSettings.timeZone,
    firstDay: Number.isInteger(firstDay) && (firstDay as number) >= 0 && (firstDay as number) <= 6 ? (firstDay as number) : defaultSettings.firstDay,
    weekNumbers: typeof weekNumbers === 'boolean' ? weekNumbers : defaultSettings.weekNumbers
  }
}

export function saveSettings(user: string, settings: CalendarSettings) {
  try {
    localStorage.setItem(keyOf(user), JSON.stringify(settings))
  } catch {
    // The settings then last until the page closes.
  }
}

// useCalendarSettings gives the settings of the signed-in user, and the zone
// that follows from them.
export function useCalendarSettings() {
  const user = useUserKey()
  const settings = ref<CalendarSettings>(loadSettings(user))

  function update(next: CalendarSettings) {
    settings.value = next
    saveSettings(user, next)
  }

  // zone is where times are shown and events are written by default.
  const zone = () => settings.value.timeZone || localZone()

  return { settings, update, zone }
}
