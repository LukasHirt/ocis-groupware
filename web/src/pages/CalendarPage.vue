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
      <calendar-grid class="groupware-calendar-main" :events="shown" @range="onRange" />
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
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { useApi } from '../composables/useApi'
import { calendarIdOf, toCalendarEvent } from '../calendar/mapping'
import { useHiddenCalendars } from '../calendar/visibility'
import type { CalendarInput, GraphCalendar, GraphEvent } from '../api/types'

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
>()

const byId = computed(() => new Map(calendars.value.map((c) => [c.id, c])))
const shown = computed(() =>
  events.value
    .filter((e) => byId.value.has(calendarIdOf(e.id)) && !isHidden(calendarIdOf(e.id)))
    .map((e) => toCalendarEvent(e, byId.value, $gettext('(No title)')))
)

// The grid reports the range it draws, and the events are read for exactly
// that range. A slow answer for a range that was left meanwhile is dropped.
let requested = ''
async function onRange(start: string, end: string) {
  const key = `${start}/${end}`
  requested = key
  const loaded = await run($gettext('Could not load the events'), (c) => c.calendarView({ start, end }))
  if (loaded && requested === key) {
    events.value = loaded
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
  flex: 1 1 auto;
  min-width: 0;
}
</style>
