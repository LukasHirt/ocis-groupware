<template>
  <oc-button
    variation="passive"
    appearance="outline"
    size="small"
    class="oc-mr-xs"
    :disabled="view.isToday"
    data-testid="calendar-today"
    @click="emit('today')"
  >
    {{ $gettext('Today') }}
  </oc-button>
  <oc-button
    appearance="raw"
    :aria-label="previousLabel(view.type, $gettext)"
    data-testid="calendar-prev"
    @click="emit('prev')"
  >
    <oc-icon name="arrow-left-s" fill-type="line" />
  </oc-button>
  <oc-button
    appearance="raw"
    :aria-label="nextLabel(view.type, $gettext)"
    data-testid="calendar-next"
    @click="emit('next')"
  >
    <oc-icon name="arrow-right-s" fill-type="line" />
  </oc-button>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import { nextLabel, previousLabel, type ViewState } from './view'

defineOptions({ name: 'CalendarSteps' })

// The buttons that move the grid: back to today, and one step either way.
defineProps<{ view: ViewState }>()
const emit = defineEmits<{ today: []; prev: []; next: [] }>()

const { $gettext } = useGettext()
</script>
