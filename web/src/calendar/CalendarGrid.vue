<template>
  <div ref="root" class="gw-calendar-grid" :class="{ 'gw-compact': compact }" data-testid="calendar-grid">
    <full-calendar ref="calendar" :options="options" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import { DateTime } from 'luxon'
import FullCalendar from '@fullcalendar/vue3'
import dayGridPlugin from '@fullcalendar/daygrid'
import timeGridPlugin from '@fullcalendar/timegrid'
import interactionPlugin from '@fullcalendar/interaction'
import luxonPlugin from '@fullcalendar/luxon3'
import type {
  CalendarOptions,
  DateSelectArg,
  DatesSetArg,
  EventClickArg,
  EventInput,
  EventMountArg
} from '@fullcalendar/core'
import type { ViewState, ViewType } from './view'

defineOptions({ name: 'CalendarGrid' })

const props = defineProps<{
  events: EventInput[]
  // The view the grid opens with.
  initialView?: ViewType
  // The zone the grid shows times in; empty for the browser's own.
  timeZone?: string
  // 0 for Sunday to 6 for Saturday.
  firstDay?: number
  weekNumbers?: boolean
  // The grid is as tall as its rows and the page scrolls, where a short screen
  // would otherwise give the grid a scroll area of its own inside the page.
  autoHeight?: boolean
  // A narrow window: the month shows a dot for each event and no text.
  compact?: boolean
}>()
const emit = defineEmits<{
  // The visible range changed; both ends are ISO instants.
  range: [start: string, end: string]
  // What the toolbar needs to show about the range and the view.
  view: [state: ViewState]
  // The user marked a span of days or times. Both ends are ISO strings in the
  // grid's zone, with an offset, or plain dates for an all-day span.
  select: [start: string, end: string, allDay: boolean]
  // The user opened an event, by its id.
  open: [id: string]
}>()

const gettext = useGettext()
const { $gettext } = gettext

// The toolbar is drawn by the page, which drives the grid through these.
const calendar = ref<{ getApi: () => import('@fullcalendar/core').CalendarApi }>()
const api = () => calendar.value?.getApi()
defineExpose({
  prev: () => api()?.prev(),
  next: () => api()?.next(),
  today: () => api()?.today(),
  changeView: (type: ViewType) => api()?.changeView(type)
})

// The library lays the grid out for the size it had and listens to the window
// only. The box changes when the sidebar opens or closes, with no window
// resize, so the grid is told to measure again whenever its box changes.
const root = ref<HTMLElement>()
let observer: ResizeObserver | undefined
let frame = 0
onMounted(() => {
  if (!root.value || typeof ResizeObserver === 'undefined') {
    return
  }
  observer = new ResizeObserver(() => {
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => api()?.updateSize())
  })
  observer.observe(root.value)
})
onBeforeUnmount(() => {
  cancelAnimationFrame(frame)
  observer?.disconnect()
})

const timeFormat = { hour: 'numeric', minute: '2-digit' } as const

// The label read out for an event: its title, when it is and which calendar.
function eventLabel(arg: EventMountArg): string {
  const start = arg.event.start
  let when = ''
  if (start) {
    const from = DateTime.fromJSDate(start)
    when = arg.event.allDay
      ? from.toLocaleString(DateTime.DATE_FULL)
      : from.toLocaleString(DateTime.DATETIME_MED)
  }
  const name = arg.event.extendedProps.calendarName as string | undefined
  return [arg.event.title, when, name].filter(Boolean).join(', ')
}

const options = computed<CalendarOptions>(() => ({
  plugins: [dayGridPlugin, timeGridPlugin, interactionPlugin, luxonPlugin],
  timeZone: props.timeZone || 'local',
  locale: gettext.current || undefined,
  initialView: props.initialView ?? 'dayGridMonth',
  // The page draws the toolbar with the buttons of the design system.
  headerToolbar: false,
  views: {
    dayGridMonth: { dayHeaderFormat: { weekday: 'short' } },
    timeGridWeek: { dayHeaderFormat: { weekday: 'short', day: 'numeric' } },
    timeGridDay: { dayHeaderFormat: { weekday: 'long', day: 'numeric' } }
  },
  firstDay: props.firstDay ?? 1,
  weekNumbers: props.weekNumbers ?? false,
  weekText: $gettext('W'),
  weekTextLong: $gettext('Week'),
  allDayText: $gettext('All day'),
  moreLinkText: (n: number) => $gettext('+%{n} more', { n: String(n) }),
  noEventsText: $gettext('No events'),
  slotLabelFormat: timeFormat,
  eventTimeFormat: timeFormat,
  displayEventEnd: false,
  scrollTime: '07:00:00',
  expandRows: true,
  height: props.autoHeight ? 'auto' : '100%',
  nowIndicator: true,
  dayMaxEvents: props.compact ? 2 : true,
  selectable: true,
  unselectAuto: true,
  events: props.events,
  select: (arg: DateSelectArg) => {
    emit('select', arg.startStr, arg.endStr, arg.allDay)
    arg.view.calendar.unselect()
  },
  eventClick: (arg: EventClickArg) => {
    arg.jsEvent.preventDefault()
    emit('open', arg.event.id)
  },
  // A truncated title stays readable through the tooltip, and the label of
  // the link says what a screen reader needs.
  eventDidMount: (arg: EventMountArg) => {
    arg.el.title = [arg.timeText, arg.event.title].filter(Boolean).join(' ')
    arg.el.setAttribute('aria-label', eventLabel(arg))
  },
  // The strings carry the offset of the grid's zone; the Date objects of a
  // named zone do not stand for the same instants.
  datesSet: (arg: DatesSetArg) => {
    emit('range', new Date(arg.startStr).toISOString(), new Date(arg.endStr).toISOString())
    const now = new Date()
    emit('view', {
      type: arg.view.type as ViewType,
      title: arg.view.title,
      isToday: arg.start <= now && now < arg.end
    })
  }
}))
</script>

<style>
/* FullCalendar reads these variables; they follow the theme of oCIS Web, so
   the dark theme needs no rules of its own. */
.gw-calendar-grid {
  --fc-small-font-size: var(--oc-font-size-xsmall);
  --fc-page-bg-color: var(--oc-color-background-default);
  --fc-neutral-bg-color: var(--oc-color-background-muted);
  --fc-neutral-text-color: var(--oc-color-text-muted);
  --fc-border-color: var(--oc-color-border);
  /* Today is marked on its day number. */
  --fc-today-bg-color: transparent;
  --fc-highlight-color: color-mix(in srgb, var(--oc-color-swatch-primary-default) 15%, transparent);
  --fc-non-business-color: transparent;
  --fc-now-indicator-color: var(--oc-color-swatch-danger-default);
  --fc-event-border-color: transparent;
  --fc-more-link-bg-color: transparent;
  --fc-more-link-text-color: var(--oc-color-swatch-primary-default);
  --fc-list-event-hover-bg-color: var(--oc-color-background-hover);
  font-family: var(--oc-font-family);
  font-size: var(--oc-font-size-small);
  color: var(--oc-color-text-default);
  flex: 1 1 auto;
  min-height: 32rem;
}
.gw-calendar-grid .fc-theme-standard td,
.gw-calendar-grid .fc-theme-standard th {
  border-color: var(--oc-color-border);
}
.gw-calendar-grid .fc-scrollgrid {
  border: none;
}
.gw-calendar-grid a {
  color: inherit;
}
.gw-calendar-grid .fc-col-header-cell {
  padding: var(--oc-space-xsmall) 0;
  font-weight: var(--oc-font-weight-semibold);
  font-size: var(--oc-font-size-xsmall);
  letter-spacing: 0.03em;
  text-transform: uppercase;
  color: var(--oc-color-text-muted);
}
.gw-calendar-grid .fc-col-header-cell.fc-day-today a {
  color: var(--oc-color-swatch-primary-default);
}
.gw-calendar-grid .fc-daygrid-day-top {
  flex-direction: row;
  padding: var(--oc-space-xsmall);
}
.gw-calendar-grid .fc-daygrid-day-number {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 1.75rem;
  height: 1.75rem;
  padding: 0;
  border-radius: 50%;
  color: var(--oc-color-text-default);
  text-decoration: none;
}
.gw-calendar-grid .fc-day-today .fc-daygrid-day-number {
  background: var(--oc-color-swatch-primary-default);
  color: var(--oc-color-swatch-primary-contrast);
  font-weight: var(--oc-font-weight-bold);
}
/* The library fades the whole top of a day of another month, which leaves its
   number below the contrast that text needs; the muted colour is enough. */
.gw-calendar-grid .fc-day-other .fc-daygrid-day-top {
  opacity: 1;
}
.gw-calendar-grid .fc-day-other .fc-daygrid-day-number {
  color: var(--oc-color-text-muted);
}
.gw-calendar-grid .fc-day-sat,
.gw-calendar-grid .fc-day-sun {
  background: var(--oc-color-background-muted);
}
.gw-calendar-grid .fc-daygrid-week-number {
  background: transparent;
  color: var(--oc-color-text-muted);
  font-size: var(--oc-font-size-xsmall);
}
.gw-calendar-grid .fc-timegrid-slot {
  height: 2.5rem;
}
.gw-calendar-grid .fc-timegrid-slot-minor {
  border-top-style: none;
}
.gw-calendar-grid .fc-timegrid-slot-label,
.gw-calendar-grid .fc-timegrid-axis {
  vertical-align: top;
  color: var(--oc-color-text-muted);
  font-size: var(--oc-font-size-xsmall);
}
.gw-calendar-grid .fc-event {
  padding: 1px var(--oc-space-xsmall);
  border-radius: 4px;
  cursor: pointer;
}
.gw-calendar-grid .fc-event:focus-visible {
  outline: 2px solid var(--oc-color-swatch-primary-default);
  outline-offset: 2px;
}
/* A timed event in the month is a dot, a time and a title in the page's colour. */
.gw-calendar-grid .fc-daygrid-dot-event .fc-event-title,
.gw-calendar-grid .fc-daygrid-dot-event .fc-event-time {
  color: var(--oc-color-text-default);
}
.gw-calendar-grid .fc-daygrid-dot-event .fc-event-title {
  font-weight: var(--oc-font-weight-normal);
}
.gw-calendar-grid .fc-daygrid-dot-event:hover,
.gw-calendar-grid .fc-daygrid-dot-event:focus {
  background: var(--oc-color-background-hover);
}
.gw-calendar-grid .fc-event-time {
  font-weight: var(--oc-font-weight-semibold);
}
/* On a narrow window the month has no room for text, so an event is a dot. */
.gw-calendar-grid.gw-compact .fc-daygrid-dot-event .fc-event-title,
.gw-calendar-grid.gw-compact .fc-daygrid-dot-event .fc-event-time {
  display: none;
}
.gw-calendar-grid.gw-compact .fc-daygrid-dot-event {
  display: inline-flex;
  padding: 2px;
}
/* A short event has one line: the time, then the title. */
.gw-calendar-grid .fc-timegrid-event-short .fc-event-main-frame {
  flex-direction: row;
  align-items: center;
  gap: var(--oc-space-xsmall);
}
.gw-calendar-grid .fc-timegrid-event-short .fc-event-time::after {
  content: none;
}
.gw-calendar-grid .fc-timegrid-event-short .fc-event-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.gw-calendar-grid .gw-event-selected {
  box-shadow: 0 0 0 2px var(--oc-color-swatch-primary-default);
}
.gw-calendar-grid .fc-popover {
  z-index: 10;
  border: 1px solid var(--oc-color-border);
  border-radius: 5px;
  background: var(--oc-color-background-secondary);
  box-shadow: 0 4px 16px rgb(0 0 0 / 20%);
}
.gw-calendar-grid .fc-popover-header {
  padding: var(--oc-space-small);
  background: transparent;
  font-weight: var(--oc-font-weight-semibold);
}
</style>
