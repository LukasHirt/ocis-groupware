<template>
  <ul class="oc-list">
    <li v-if="calendar.canShare">
      <oc-button
        appearance="raw"
        justify-content="left"
        class="oc-width-1-1"
        data-testid="calendar-edit"
        @click="emit('edit', calendar)"
      >
        <oc-icon name="pencil" fill-type="line" size="small" />
        {{ $gettext('Edit') }}
      </oc-button>
    </li>
    <li v-if="calendar.canShare">
      <oc-button
        appearance="raw"
        justify-content="left"
        class="oc-width-1-1"
        data-testid="calendar-share"
        @click="emit('share', calendar)"
      >
        <oc-icon name="share-forward" fill-type="line" size="small" />
        {{ $gettext('Share') }}
      </oc-button>
    </li>
    <li v-if="calendar.canEdit">
      <oc-button
        appearance="raw"
        justify-content="left"
        class="oc-width-1-1"
        data-testid="calendar-import"
        @click="emit('import', calendar)"
      >
        <oc-icon name="upload" fill-type="line" size="small" />
        {{ $gettext('Import') }}
      </oc-button>
    </li>
    <li>
      <oc-button
        appearance="raw"
        justify-content="left"
        class="oc-width-1-1"
        data-testid="calendar-export"
        @click="emit('export', calendar)"
      >
        <oc-icon name="download" fill-type="line" size="small" />
        {{ $gettext('Export') }}
      </oc-button>
    </li>
    <li v-if="calendar.canShare && calendar.isRemovable">
      <oc-button
        appearance="raw"
        variation="danger"
        justify-content="left"
        class="oc-width-1-1"
        data-testid="calendar-delete"
        @click="emit('remove', calendar)"
      >
        <oc-icon name="delete-bin" fill-type="line" size="small" />
        {{ $gettext('Delete') }}
      </oc-button>
    </li>
  </ul>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import type { GraphCalendar } from '../api/types'

defineOptions({ name: 'CalendarMenu' })

// What can be done with a calendar, by what the user may do with it: the
// owner changes, shares and deletes it, whoever may write can import into it,
// and everyone can export it.
defineProps<{ calendar: GraphCalendar }>()
const emit = defineEmits<{
  edit: [calendar: GraphCalendar]
  share: [calendar: GraphCalendar]
  import: [calendar: GraphCalendar]
  export: [calendar: GraphCalendar]
  remove: [calendar: GraphCalendar]
}>()

const { $gettext } = useGettext()
</script>
