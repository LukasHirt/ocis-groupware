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
import type {
  CalendarOptions,
  DateSelectArg,
  DatesSetArg,
  EventClickArg,
  EventInput
} from '@fullcalendar/core'

defineOptions({ name: 'CalendarGrid' })

const props = defineProps<{ events: EventInput[] }>()
const emit = defineEmits<{
  // The visible range changed; both ends are ISO instants.
  range: [start: string, end: string]
  // The user marked a span of days or times; both ends are ISO instants.
  select: [start: string, end: string, allDay: boolean]
  // The user opened an event, by its id.
  open: [id: string]
}>()

const { $gettext } = useGettext()

const options = computed<CalendarOptions>(() => ({
  plugins: [dayGridPlugin, timeGridPlugin, interactionPlugin],
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
  firstDay: 1,
  height: '100%',
  nowIndicator: true,
  dayMaxEvents: true,
  selectable: true,
  unselectAuto: true,
  events: props.events,
  select: (arg: DateSelectArg) => {
    emit('select', arg.start.toISOString(), arg.end.toISOString(), arg.allDay)
    arg.view.calendar.unselect()
  },
  eventClick: (arg: EventClickArg) => {
    arg.jsEvent.preventDefault()
    emit('open', arg.event.id)
  },
  datesSet: (arg: DatesSetArg) => emit('range', arg.start.toISOString(), arg.end.toISOString())
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
