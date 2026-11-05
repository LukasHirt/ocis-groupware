<template>
  <page-layout
    :title="$gettext('Calendar')"
    :loading="loading"
    :empty="calendars.length === 0"
    :empty-text="$gettext('You have no calendars yet.')"
  >
    <div class="oc-flex groupware-calendar-page oc-mt-m">
      <aside class="oc-mr-m groupware-calendar-side">
        <calendar-list
          :calendars="calendars"
          :hidden="hidden"
          @toggle="toggle"
          @add="dialog = { kind: 'create' }"
          @edit="(calendar: GraphCalendar) => (dialog = { kind: 'edit', calendar })"
          @remove="(calendar: GraphCalendar) => (dialog = { kind: 'delete', calendar })"
        />
      </aside>
      <div class="groupware-calendar-main">
        <div class="oc-flex oc-flex-right oc-mb-s">
          <button
            type="button"
            class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
            data-testid="event-add"
            @click="newEvent()"
          >
            {{ $gettext('New event') }}
          </button>
        </div>
        <calendar-grid :events="shown" @range="onRange" @select="onSelect" @open="openEvent" />
      </div>
    </div>
    <calendar-dialog
      v-if="dialog?.kind === 'create'"
      :title="$gettext('Add calendar')"
      @close="dialog = undefined"
      @save="create"
    />
    <calendar-dialog
      v-else-if="dialog?.kind === 'edit'"
      :title="$gettext('Edit calendar')"
      :calendar="dialog.calendar"
      @close="dialog = undefined"
      @save="update"
    />
    <event-dialog
      v-else-if="dialog?.kind === 'event-create'"
      :title="$gettext('New event')"
      :form="dialog.form"
      :calendars="writable"
      @close="dialog = undefined"
      @save="createEvent"
    />
    <event-dialog
      v-else-if="dialog?.kind === 'event-edit'"
      :title="dialog.readOnly ? $gettext('Event') : $gettext('Edit event')"
      :form="dialog.form"
      :calendars="writable"
      existing
      :read-only="dialog.readOnly"
      :notice="dialog.notice"
      @close="dialog = undefined"
      @save="changeEvent"
      @remove="askDeleteEvent"
    />
    <confirm-dialog
      v-else-if="dialog?.kind === 'event-delete'"
      :title="$gettext('Delete event')"
      :message="$gettext('Delete %{name}? This cannot be undone.', { name: dialog.event.subject || $gettext('this event') })"
      :confirm-label="$gettext('Delete')"
      @cancel="dialog = undefined"
      @confirm="deleteEvent"
    />
    <confirm-dialog
      v-else-if="dialog?.kind === 'delete'"
      :title="$gettext('Delete calendar')"
      :message="
        $gettext('Delete %{name} and all of its events? This cannot be undone.', {
          name: dialog.calendar.name
        })
      "
      :confirm-label="$gettext('Delete')"
      @cancel="dialog = undefined"
      @confirm="remove"
    />
  </page-layout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import PageLayout from '../components/PageLayout.vue'
import CalendarGrid from '../calendar/CalendarGrid.vue'
import CalendarDialog from '../calendar/CalendarDialog.vue'
import CalendarList from '../calendar/CalendarList.vue'
import EventDialog from '../calendar/EventDialog.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { useApi } from '../composables/useApi'
import { calendarIdOf, toCalendarEvent } from '../calendar/mapping'
import { useHiddenCalendars } from '../calendar/visibility'
import { blankForm, formFromEvent } from '../calendar/eventForm'
import { localZone } from '../calendar/zones'
import type { CalendarInput, EventInput, GraphCalendar, GraphEvent } from '../api/types'
import type { EventForm } from '../calendar/eventForm'

defineOptions({ name: 'CalendarPage' })

const { $gettext } = useGettext()
const { run } = useApi()
const { hidden, isHidden, toggle } = useHiddenCalendars()

const calendars = ref<GraphCalendar[]>([])
const events = ref<GraphEvent[]>([])
const loading = ref(true)
const dialog = ref<
  | { kind: 'create' }
  | { kind: 'edit'; calendar: GraphCalendar }
  | { kind: 'delete'; calendar: GraphCalendar }
  | { kind: 'event-create'; form: EventForm }
  | { kind: 'event-edit'; event: GraphEvent; form: EventForm; readOnly: boolean; notice?: string }
  | { kind: 'event-delete'; event: GraphEvent }
>()

const byId = computed(() => new Map(calendars.value.map((c) => [c.id, c])))
const shown = computed(() =>
  events.value
    .filter((e) => byId.value.has(calendarIdOf(e.id)) && !isHidden(calendarIdOf(e.id)))
    .map((e) => toCalendarEvent(e, byId.value, $gettext('(No title)')))
)

// The grid reports the range it draws, and the events are read for exactly
// that range. A slow answer for a range that was left meanwhile is dropped.
let range: { start: string; end: string } | undefined
async function loadEvents() {
  if (!range) {
    return
  }
  const asked = range
  const loaded = await run($gettext('Could not load the events'), (c) => c.calendarView({ ...asked, zone: localZone() }))
  if (loaded && range === asked) {
    events.value = loaded
  }
}

function onRange(start: string, end: string) {
  range = { start, end }
  return loadEvents()
}

// writable lists the calendars an event can be created in, the default one
// first.
const writable = computed(() =>
  calendars.value
    .filter((c) => c.canEdit)
    .sort((a, b) => Number(b.isDefaultCalendar) - Number(a.isDefaultCalendar))
)

function newEvent(selection?: { start: string; end: string; allDay: boolean }) {
  const calendar = writable.value[0]
  if (!calendar) {
    return
  }
  const zone = localZone()
  const start = selection?.start ?? nextHour()
  dialog.value = {
    kind: 'event-create',
    form: blankForm({
      calendarId: calendar.id,
      zone,
      start,
      end: selection?.end,
      allDay: selection?.allDay ?? false
    })
  }
}

function nextHour(): string {
  const d = new Date()
  d.setHours(d.getHours() + 1, 0, 0, 0)
  return d.toISOString()
}

function onSelect(start: string, end: string, allDay: boolean) {
  newEvent({ start, end, allDay })
}

function openEvent(id: string) {
  const event = events.value.find((e) => e.id === id)
  if (!event) {
    return
  }
  const calendar = byId.value.get(calendarIdOf(id))
  const repeats = event.type !== 'singleInstance'
  const readOnly = repeats || !calendar?.canEdit
  dialog.value = {
    kind: 'event-edit',
    event,
    form: formFromEvent(event),
    readOnly,
    notice: repeats
      ? $gettext('This event repeats. Editing repeating events is not available yet.')
      : !calendar?.canEdit
        ? $gettext('This calendar is shared with you as read only.')
        : undefined
  }
}

async function createEvent(save: { calendarId: string; input: EventInput }) {
  const created = await run($gettext('Could not create the event'), (c) =>
    c.createEvent(save.input, save.calendarId)
  )
  if (created) {
    dialog.value = undefined
    await loadEvents()
  }
}

async function changeEvent(save: { input: EventInput }) {
  if (dialog.value?.kind !== 'event-edit') {
    return
  }
  const event = dialog.value.event
  const changed = await run($gettext('Could not change the event'), (c) =>
    c.updateEvent(event.id, save.input, event['@odata.etag'])
  )
  if (changed) {
    dialog.value = undefined
    await loadEvents()
  }
}

function askDeleteEvent() {
  if (dialog.value?.kind === 'event-edit') {
    dialog.value = { kind: 'event-delete', event: dialog.value.event }
  }
}

async function deleteEvent() {
  if (dialog.value?.kind !== 'event-delete') {
    return
  }
  const event = dialog.value.event
  const done = await run($gettext('Could not delete the event'), async (c) => {
    await c.deleteEvent(event.id, event['@odata.etag'])
    return true
  })
  if (done) {
    dialog.value = undefined
    await loadEvents()
  }
}

async function create(input: CalendarInput) {
  const created = await run($gettext('Could not create the calendar'), (c) => c.createCalendar(input))
  if (created) {
    calendars.value = [...calendars.value, created]
    dialog.value = undefined
  }
}

async function update(input: CalendarInput) {
  const calendar = dialog.value?.kind === 'edit' ? dialog.value.calendar : undefined
  if (!calendar) {
    return
  }
  const changed = await run($gettext('Could not change the calendar'), (c) =>
    c.updateCalendar(calendar.id, input)
  )
  if (changed) {
    calendars.value = calendars.value.map((c) => (c.id === calendar.id ? changed : c))
    dialog.value = undefined
  }
}

async function remove() {
  const calendar = dialog.value?.kind === 'delete' ? dialog.value.calendar : undefined
  if (!calendar) {
    return
  }
  const done = await run($gettext('Could not delete the calendar'), async (c) => {
    await c.deleteCalendar(calendar.id)
    return true
  })
  if (done) {
    calendars.value = calendars.value.filter((c) => c.id !== calendar.id)
    dialog.value = undefined
  }
}

onMounted(async () => {
  calendars.value = (await run($gettext('Could not load the calendars'), (c) => c.listCalendars())) ?? []
  loading.value = false
})
</script>

<style>
.groupware-calendar-side {
  flex: 0 0 14rem;
  min-width: 0;
}
.groupware-calendar-main {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
}
</style>
