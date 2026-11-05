import { ref } from 'vue'

const storageKey = 'groupware.hiddenCalendars'

function read(): string[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(storageKey) ?? '[]')
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : []
  } catch {
    return []
  }
}

function write(ids: string[]) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(ids))
  } catch {
    // Storage can be blocked; the choice then lasts until the page closes.
  }
}

// useHiddenCalendars keeps the ids of the calendars the user switched off.
// The choice is a per-browser convenience, so it lives in local storage and
// the page works without it.
export function useHiddenCalendars() {
  const hidden = ref<string[]>(read())

  const isHidden = (id: string) => hidden.value.includes(id)

  function toggle(id: string) {
    hidden.value = isHidden(id) ? hidden.value.filter((h) => h !== id) : [...hidden.value, id]
    write(hidden.value)
  }

  return { hidden, isHidden, toggle }
}
