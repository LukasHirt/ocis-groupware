<template>
  <page-layout
    :title="$gettext('Calendar')"
    :loading="loading"
    :empty="calendars.length === 0"
    :empty-text="$gettext('You have no calendars yet.')"
  >
    <ul class="oc-list oc-mt-m">
      <li
        v-for="calendar in calendars"
        :key="calendar.id"
        class="oc-flex oc-flex-middle oc-py-xs"
        data-testid="calendar-item"
      >
        <span
          class="oc-mr-s groupware-swatch"
          :style="{ backgroundColor: calendar.hexColor || undefined }"
          aria-hidden="true"
        />
        <span class="name">{{ calendar.name }}</span>
        <span class="oc-text-muted oc-ml-s">
          {{ calendar.owner.name }}<template v-if="!calendar.canEdit">, {{ $gettext('read only') }}</template>
        </span>
      </li>
    </ul>
  </page-layout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import PageLayout from '../components/PageLayout.vue'
import { useApi } from '../composables/useApi'
import type { GraphCalendar } from '../api/types'

defineOptions({ name: 'CalendarPage' })

const { $gettext } = useGettext()
const { run } = useApi()
const calendars = ref<GraphCalendar[]>([])
const loading = ref(true)

onMounted(async () => {
  calendars.value = (await run($gettext('Could not load the calendars'), (c) => c.listCalendars())) ?? []
  loading.value = false
})
</script>

<style>
.groupware-swatch {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: 50%;
  background-color: var(--oc-color-swatch-passive-default);
  flex: none;
}
</style>
