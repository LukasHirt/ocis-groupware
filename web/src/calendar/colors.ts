import type { GraphCalendar } from '../api/types'

// Colours for calendars that have none of their own. They are mid-tone so
// that both white and black text stay readable in light and dark themes.
export const palette = [
  '#3b82c4',
  '#2f9e6e',
  '#d1752b',
  '#8e5bc0',
  '#c2415d',
  '#2a9aa8',
  '#a38a1f',
  '#5f7388'
]

type Gettext = (message: string) => string

// colorName is the name of a palette colour, for people who cannot tell the
// swatches apart by sight. A colour outside the palette is a custom one.
export function colorName(hex: string, $gettext: Gettext): string {
  switch (hex.toLowerCase()) {
    case palette[0]:
      return $gettext('Blue')
    case palette[1]:
      return $gettext('Green')
    case palette[2]:
      return $gettext('Orange')
    case palette[3]:
      return $gettext('Purple')
    case palette[4]:
      return $gettext('Red')
    case palette[5]:
      return $gettext('Teal')
    case palette[6]:
      return $gettext('Olive')
    case palette[7]:
      return $gettext('Slate')
    default:
      return $gettext('Custom colour')
  }
}

function hash(text: string): number {
  let h = 0
  for (let i = 0; i < text.length; i++) {
    h = (h * 31 + text.charCodeAt(i)) >>> 0
  }
  return h
}

// calendarColor is the colour the calendar was given, or one from the palette
// that depends only on the calendar's id, so it is the same on every load.
export function calendarColor(calendar: Pick<GraphCalendar, 'id' | 'hexColor'>): string {
  if (/^#[0-9a-f]{6}$/i.test(calendar.hexColor)) {
    return calendar.hexColor
  }
  return palette[hash(calendar.id) % palette.length]
}

// readableTextColor chooses black or white text for a background colour,
// whichever has the higher contrast ratio as WCAG defines it. The larger of
// the two is at least 4.58, so text always passes the 4.5 that AA asks for.
export function readableTextColor(background: string): string {
  const n = parseInt(background.slice(1), 16)
  const channel = (v: number) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  const l =
    0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255)
  const withWhite = 1.05 / (l + 0.05)
  const withBlack = (l + 0.05) / 0.05
  return withBlack > withWhite ? '#000000' : '#ffffff'
}
