<template>
  <page-layout
    :title="$gettext('Calendar')"
    :loading="loading"
    :empty="calendars.length === 0"
    :empty-text="$gettext('You have no calendars yet.')"
  >
    <div class="oc-flex groupware-calendar-page oc-mt-m">
      <aside class="oc-mr-m groupware-calendar-side">
        <calendar-list :calendars="calendars" :hidden="hidden" @toggle="toggle" />
      </aside>
      <calendar-grid class="groupware-calendar-main" :events="shown" @range="onRange" />
    </div>
  </page-layout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import PageLayout from '../components/PageLayout.vue'
import CalendarGrid from '../calendar/CalendarGrid.vue'
import CalendarList from '../calendar/CalendarList.vue'
import { useApi } from '../composables/useApi'
import { calendarIdOf, toCalendarEvent } from '../calendar/mapping'
import { useHiddenCalendars } from '../calendar/visibility'
import type { GraphCalendar, GraphEvent } from '../api/types'

defineOptions({ name: 'CalendarPage' })

const { $gettext } = useGettext()
const { run } = useApi()
const { hidden, isHidden, toggle } = useHiddenCalendars()

const calendars = ref<GraphCalendar[]>([])
const events = ref<GraphEvent[]>([])
const loading = ref(true)

const byId = computed(() => new Map(calendars.value.map((c) => [c.id, c])))
const shown = computed(() =>
  events.value
    .filter((e) => !isHidden(calendarIdOf(e.id)))
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
