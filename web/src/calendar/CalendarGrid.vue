<template>
  <div class="groupware-calendar-grid" data-testid="calendar-grid">
    <full-calendar :options="options" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
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
  EventInput
} from '@fullcalendar/core'

defineOptions({ name: 'CalendarGrid' })

const props = defineProps<{
  events: EventInput[]
  // The zone the grid shows times in; empty for the browser's own.
  timeZone?: string
  // 0 for Sunday to 6 for Saturday.
  firstDay?: number
  weekNumbers?: boolean
}>()
const emit = defineEmits<{
  // The visible range changed; both ends are ISO instants.
  range: [start: string, end: string]
  // The user marked a span of days or times. Both ends are ISO strings in the
  // grid's zone, with an offset, or plain dates for an all-day span.
  select: [start: string, end: string, allDay: boolean]
  // The user opened an event, by its id.
  open: [id: string]
}>()

const { $gettext } = useGettext()

const options = computed<CalendarOptions>(() => ({
  plugins: [dayGridPlugin, timeGridPlugin, interactionPlugin, luxonPlugin],
  timeZone: props.timeZone || 'local',
  initialView: 'dayGridMonth',
  headerToolbar: {
    left: 'prev,next today',
    center: 'title',
    right: 'dayGridMonth,timeGridWeek,timeGridDay'
  },
  buttonText: {
    today: $gettext('Today'),
    month: $gettext('Month'),
    week: $gettext('Week'),
    day: $gettext('Day')
  },
  buttonHints: {
    prev: $gettext('Previous'),
    next: $gettext('Next'),
    today: $gettext('Today')
  },
  firstDay: props.firstDay ?? 1,
  weekNumbers: props.weekNumbers ?? false,
  weekText: $gettext('W'),
  weekTextLong: $gettext('Week'),
  height: '100%',
  nowIndicator: true,
  dayMaxEvents: true,
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
  // The strings carry the offset of the grid's zone; the Date objects of a
  // named zone do not stand for the same instants.
  datesSet: (arg: DatesSetArg) =>
    emit('range', new Date(arg.startStr).toISOString(), new Date(arg.endStr).toISOString())
}))
</script>

<style>
/* FullCalendar reads these variables; they follow the theme of oCIS Web. */
.groupware-calendar-grid {
  --fc-border-color: var(--oc-color-border);
  --fc-page-bg-color: var(--oc-color-background-default);
  --fc-neutral-bg-color: var(--oc-color-background-muted);
  --fc-today-bg-color: var(--oc-color-background-highlight);
  --fc-list-event-hover-bg-color: var(--oc-color-background-highlight);
  --fc-button-bg-color: var(--oc-color-swatch-primary-default);
  --fc-button-border-color: var(--oc-color-swatch-primary-default);
  --fc-button-hover-bg-color: var(--oc-color-swatch-primary-hover);
  --fc-button-hover-border-color: var(--oc-color-swatch-primary-hover);
  --fc-button-active-bg-color: var(--oc-color-swatch-primary-hover);
  --fc-button-active-border-color: var(--oc-color-swatch-primary-hover);
  --fc-now-indicator-color: var(--oc-color-swatch-danger-default);
  color: var(--oc-color-text-default);
  min-height: 36rem;
  height: calc(100vh - 12rem);
}
.groupware-calendar-grid a {
  color: inherit;
}
</style>
