export type ViewType = 'dayGridMonth' | 'timeGridWeek' | 'timeGridDay'

// What the toolbar of the page needs to know about the grid.
export interface ViewState {
  type: ViewType
  // The text for the range shown, such as "November 2026".
  title: string
  // Whether today lies in the range.
  isToday: boolean
}

type Gettext = (message: string) => string

// The unit a step of the toolbar moves by, named for the buttons.
export function previousLabel(type: ViewType, $gettext: Gettext): string {
  return type === 'dayGridMonth'
    ? $gettext('Previous month')
    : type === 'timeGridWeek'
      ? $gettext('Previous week')
      : $gettext('Previous day')
}

export function nextLabel(type: ViewType, $gettext: Gettext): string {
  return type === 'dayGridMonth'
    ? $gettext('Next month')
    : type === 'timeGridWeek'
      ? $gettext('Next week')
      : $gettext('Next day')
}
