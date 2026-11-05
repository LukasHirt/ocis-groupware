<template>
  <li class="gw-calendar-row" data-testid="calendar-item">
    <div class="gw-list-item">
      <oc-checkbox
        :model-value="!hidden.includes(calendar.id)"
        :label="$gettext('Show %{name}', { name: calendar.name })"
        label-hidden
        data-testid="calendar-toggle"
        @update:model-value="emit('toggle', calendar.id)"
      />
      <span class="gw-swatch" :style="{ backgroundColor: colorOf(calendar) }" aria-hidden="true" />
      <span class="name oc-text-truncate">{{ calendar.name }}</span>
      <oc-button
        :id="menuId"
        appearance="raw"
        class="gw-calendar-kebab"
        :aria-label="$gettext('Actions for %{name}', { name: calendar.name })"
        :aria-expanded="nested ? String(open) : undefined"
        data-testid="calendar-actions"
        @click="open = !open"
      >
        <oc-icon name="more-2" fill-type="line" size="small" />
      </oc-button>
      <oc-drop
        v-if="!nested"
        :drop-id="`${menuId}-drop`"
        :toggle="`#${menuId}`"
        mode="click"
        close-on-click
        padding-size="small"
      >
        <calendar-menu :calendar="calendar" v-on="forward" />
      </oc-drop>
    </div>
    <!-- Inside the folded list a drop cannot open another one, so the menu opens in place. -->
    <calendar-menu v-if="nested && open" :calendar="calendar" v-on="forward" />
    <p v-if="!calendar.canShare && calendar.owner.name" class="gw-calendar-meta">
      {{ calendar.owner.name }} · {{ calendar.canEdit ? $gettext('Can edit') : $gettext('Read only') }}
    </p>
  </li>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphCalendar } from '../api/types'
import { calendarColor } from './colors'
import CalendarMenu from './CalendarMenu.vue'

defineOptions({ name: 'CalendarRow' })

defineProps<{ calendar: GraphCalendar; hidden: string[]; menuId: string; nested?: boolean }>()
const emit = defineEmits<{
  toggle: [id: string]
  edit: [calendar: GraphCalendar]
  share: [calendar: GraphCalendar]
  remove: [calendar: GraphCalendar]
  export: [calendar: GraphCalendar]
  import: [calendar: GraphCalendar]
}>()

const { $gettext } = useGettext()
const open = ref(false)
// The menu says what was asked, and the row passes it on.
const forward = {
  edit: (c: GraphCalendar) => (close(), emit('edit', c)),
  share: (c: GraphCalendar) => (close(), emit('share', c)),
  import: (c: GraphCalendar) => (close(), emit('import', c)),
  export: (c: GraphCalendar) => (close(), emit('export', c)),
  remove: (c: GraphCalendar) => (close(), emit('remove', c))
}
function close() {
  open.value = false
}
const colorOf = (c: GraphCalendar) => calendarColor(c)
</script>

<style>
.gw-swatch {
  flex: none;
  width: 0.75rem;
  height: 0.75rem;
  border-radius: 50%;
}
.gw-calendar-meta {
  margin: 0 0 var(--oc-space-xsmall) 3.25rem;
  overflow-wrap: anywhere;
  font-size: var(--oc-font-size-small);
  color: var(--oc-color-text-muted);
}
/* The menu button shows on hover and focus, and always on a touch screen. */
.gw-calendar-kebab {
  margin-left: auto;
  opacity: 0;
}
.gw-calendar-row:hover .gw-calendar-kebab,
.gw-calendar-kebab:focus-visible,
.gw-calendar-kebab[aria-expanded='true'] {
  opacity: 1;
}
@media (hover: none) {
  .gw-calendar-kebab {
    opacity: 1;
  }
}
.gw-calendar-row .gw-list-item .name {
  min-width: 0;
}
</style>
