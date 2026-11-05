<template>
  <div class="oc-flex oc-flex-middle oc-flex-between oc-mb-s">
    <h2 class="groupware-side-title">{{ $gettext('Calendars') }}</h2>
    <button
      type="button"
      class="oc-button oc-button-s oc-button-passive"
      data-testid="calendar-add"
      @click="emit('add')"
    >
      {{ $gettext('Add calendar') }}
    </button>
  </div>
  <ul class="oc-list groupware-calendar-list" :aria-label="$gettext('Calendars')">
    <li
      v-for="calendar in calendars"
      :key="calendar.id"
      class="oc-flex oc-flex-middle oc-flex-wrap oc-py-xs"
      data-testid="calendar-item"
    >
      <label class="oc-flex oc-flex-middle groupware-calendar-label">
        <input
          type="checkbox"
          class="groupware-check oc-mr-s"
          :checked="!hidden.includes(calendar.id)"
          :aria-label="$gettext('Show %{name}', { name: calendar.name })"
          data-testid="calendar-toggle"
          @change="emit('toggle', calendar.id)"
        />
        <span
          class="oc-mr-s groupware-swatch"
          :style="{ backgroundColor: colorOf(calendar) }"
          aria-hidden="true"
        />
        <span class="name">{{ calendar.name }}</span>
      </label>
      <span v-if="!calendar.isDefaultCalendar && calendar.owner.name" class="oc-text-muted oc-ml-s">
        {{ calendar.owner.name }}<template v-if="!calendar.canShare"
          >, {{ calendar.canEdit ? $gettext('can edit') : $gettext('read only') }}</template
        >
      </span>
      <span class="groupware-calendar-actions">
        <button
          v-if="calendar.canShare"
          type="button"
          class="oc-button oc-button-s oc-button-passive"
          :aria-label="$gettext('Share %{name}', { name: calendar.name })"
          data-testid="calendar-share"
          @click="emit('share', calendar)"
        >
          {{ $gettext('Share') }}
        </button>
        <button
          v-if="calendar.canEdit"
          type="button"
          class="oc-button oc-button-s oc-button-passive"
          :aria-label="$gettext('Edit %{name}', { name: calendar.name })"
          data-testid="calendar-edit"
          @click="emit('edit', calendar)"
        >
          {{ $gettext('Edit') }}
        </button>
        <button
          v-if="calendar.isRemovable"
          type="button"
          class="oc-button oc-button-s oc-button-passive"
          :aria-label="$gettext('Delete %{name}', { name: calendar.name })"
          data-testid="calendar-delete"
          @click="emit('remove', calendar)"
        >
          {{ $gettext('Delete') }}
        </button>
      </span>
    </li>
  </ul>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import type { GraphCalendar } from '../api/types'
import { calendarColor } from './colors'

defineOptions({ name: 'CalendarList' })

defineProps<{ calendars: GraphCalendar[]; hidden: string[] }>()
const emit = defineEmits<{
  toggle: [id: string]
  add: []
  edit: [calendar: GraphCalendar]
  share: [calendar: GraphCalendar]
  remove: [calendar: GraphCalendar]
}>()

const { $gettext } = useGettext()
const colorOf = (c: GraphCalendar) => calendarColor(c)
</script>

<style>
.groupware-swatch {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: 50%;
  background-color: var(--oc-color-swatch-passive-default);
  flex: none;
}
.groupware-check {
  width: 1rem;
  height: 1rem;
  margin-left: 0;
  accent-color: var(--oc-color-swatch-primary-default);
  flex: none;
}
.groupware-side-title {
  font-size: 1rem;
  margin: 0;
}
.groupware-calendar-actions {
  margin-left: auto;
  display: flex;
  gap: 0.25rem;
}
.groupware-calendar-label {
  cursor: pointer;
}
</style>
