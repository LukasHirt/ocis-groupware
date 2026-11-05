<template>
  <div class="gw-calendar-list">
    <div class="gw-list-heading-row">
      <h2 class="gw-list-heading">{{ $gettext('Calendars') }}</h2>
      <oc-button
        appearance="raw"
        :aria-label="$gettext('Add calendar')"
        data-testid="calendar-add"
        @click="emit('add')"
      >
        <oc-icon name="add" fill-type="line" size="small" />
      </oc-button>
    </div>
    <ul class="oc-list" :aria-label="$gettext('Calendars')">
      <calendar-row
        v-for="calendar in own"
        :key="calendar.id"
        :calendar="calendar"
        :hidden="hidden"
        :menu-id="menuId(calendar)"
        :nested="nested"
        @toggle="emit('toggle', $event)"
        @edit="emit('edit', $event)"
        @share="emit('share', $event)"
        @remove="emit('remove', $event)"
        @import="emit('import', $event)"
        @export="emit('export', $event)"
      />
    </ul>
    <template v-if="shared.length > 0">
      <h2 class="gw-list-heading">{{ $gettext('Shared with you') }}</h2>
      <ul class="oc-list" :aria-label="$gettext('Shared with you')">
        <calendar-row
          v-for="calendar in shared"
          :key="calendar.id"
          :calendar="calendar"
          :hidden="hidden"
          :menu-id="menuId(calendar)"
          :nested="nested"
          @toggle="emit('toggle', $event)"
          @edit="emit('edit', $event)"
          @share="emit('share', $event)"
          @remove="emit('remove', $event)"
          @import="emit('import', $event)"
          @export="emit('export', $event)"
        />
      </ul>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphCalendar } from '../api/types'
import CalendarRow from './CalendarRow.vue'

defineOptions({ name: 'CalendarList' })

const props = defineProps<{
  calendars: GraphCalendar[]
  hidden: string[]
  // The list is inside a drop, so the menus of its rows are nested drops.
  nested?: boolean
}>()
const emit = defineEmits<{
  toggle: [id: string]
  add: []
  edit: [calendar: GraphCalendar]
  share: [calendar: GraphCalendar]
  remove: [calendar: GraphCalendar]
  export: [calendar: GraphCalendar]
  // The user asks to import into this calendar; the page offers the file.
  import: [calendar: GraphCalendar]
}>()

const { $gettext } = useGettext()

// A calendar the user may share is their own. The others are shared with them.
const own = computed(() => props.calendars.filter((c) => c.canShare))
const shared = computed(() => props.calendars.filter((c) => !c.canShare))

// The menu of a row is anchored to its button by an id.
const menuId = (c: GraphCalendar) => `gw-calendar-actions-${props.calendars.indexOf(c)}`
</script>

<style>
.gw-list-heading-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.gw-calendar-list .gw-list-heading-row .gw-list-heading {
  margin-top: 0;
}
</style>
