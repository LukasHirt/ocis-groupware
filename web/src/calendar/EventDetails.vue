<template>
  <div class="gw-event-details" data-testid="event-details">
    <no-content-message v-if="!event" icon="calendar-event" icon-fill-type="line">
      <template #message>
        <span>{{ $gettext('Select an event to see its details') }}</span>
      </template>
    </no-content-message>
    <template v-else>
      <p v-if="notice" class="gw-note" data-testid="event-notice">
        <oc-icon :name="noticeIcon" fill-type="line" size="small" />
        <span>{{ notice }}</span>
      </p>

      <div class="gw-event-heading" :style="{ borderLeftColor: color }">
        <h3 class="gw-event-title" data-testid="event-title">{{ event.subject || $gettext('(No title)') }}</h3>
        <p class="gw-event-time" data-testid="event-when">{{ when }}</p>
        <p class="gw-event-zone oc-text-muted">{{ event.start.timeZone }}</p>
        <p v-if="repeats" class="gw-event-row" data-testid="event-repeats">
          <oc-icon name="repeat" fill-type="line" size="small" />
          <span>{{ repeats }}</span>
        </p>
        <p v-if="calendar" class="gw-event-row">
          <span class="gw-swatch" :style="{ backgroundColor: color }" aria-hidden="true" />
          <span data-testid="event-calendar-name">{{ calendar.name }}</span>
        </p>
      </div>

      <p v-if="event.location?.displayName" class="gw-event-row" data-testid="event-place">
        <oc-icon name="map-pin" fill-type="line" size="small" />
        <span>{{ event.location.displayName }}</span>
      </p>
      <p v-if="event.isReminderOn" class="gw-event-row" data-testid="event-reminder-text">
        <oc-icon name="alarm" fill-type="line" size="small" />
        <span>{{ reminderLabel(event.reminderMinutesBeforeStart, $gettext) }}</span>
      </p>
      <p v-if="event.showAs" class="gw-event-row" data-testid="event-status-text">
        <oc-icon name="time" fill-type="line" size="small" />
        <span>{{ showAsLabel(event.showAs, $gettext) }}</span>
      </p>
      <p v-if="event.categories.length > 0" class="gw-event-row gw-event-tags" data-testid="event-tags">
        <oc-icon name="price-tag-3" fill-type="line" size="small" />
        <oc-tag v-for="c in event.categories" :key="c" size="small">{{ c }}</oc-tag>
      </p>

      <template v-if="event.attendees.length > 0">
        <h3 class="gw-event-heading-small">{{ $gettext('Attendees') }}</h3>
        <ul class="oc-list" data-testid="event-attendees">
          <li v-for="a in event.attendees" :key="a.emailAddress.address" class="gw-attendee" data-testid="event-attendee">
            <oc-avatar :user-name="a.emailAddress.name || a.emailAddress.address" :width="24" />
            <span class="gw-attendee-who">
              <span class="oc-text-truncate">{{ a.emailAddress.name || a.emailAddress.address }}</span>
              <span v-if="a.emailAddress.name" class="oc-text-muted oc-text-truncate">{{ a.emailAddress.address }}</span>
            </span>
            <span class="oc-text-muted gw-attendee-status">{{ responseLabel(a.status?.response) }}</span>
          </li>
        </ul>
      </template>

      <template v-if="event.body?.content">
        <h3 class="gw-event-heading-small">{{ $gettext('Notes') }}</h3>
        <p class="gw-event-notes" data-testid="event-notes">{{ event.body.content }}</p>
      </template>

      <div v-if="canEdit" class="gw-event-actions">
        <oc-button variation="primary" appearance="filled" size="small" data-testid="event-edit" @click="emit('edit')">
          <oc-icon name="pencil" fill-type="line" size="small" />
          {{ $gettext('Edit') }}
        </oc-button>
        <oc-button variation="danger" appearance="outline" size="small" data-testid="event-delete" @click="emit('remove')">
          <oc-icon name="delete-bin" fill-type="line" size="small" />
          {{ $gettext('Delete') }}
        </oc-button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { DateTime } from 'luxon'
import { useGettext } from 'vue3-gettext'
import { NoContentMessage } from '@ownclouders/web-pkg'
import type { GraphCalendar, GraphEvent } from '../api/types'
import { calendarColor } from './colors'
import { recurrenceFormFromEvent, describeRecurrence } from './recurrence'
import { reminderLabel } from './reminders'
import { showAsLabel } from './showAs'

defineOptions({ name: 'EventDetails' })

// What the sidebar shows for the event that was clicked. Only rows that have
// something to say appear. Changing and deleting are offered when the user may.
const props = defineProps<{
  event?: GraphEvent
  calendar?: GraphCalendar
  // The series of an occurrence, which holds the pattern.
  series?: GraphEvent
  // Whether the event can be changed and deleted.
  canEdit?: boolean
  // Why it cannot, or what the editor cannot show.
  notice?: string
  noticeIcon?: string
}>()
const emit = defineEmits<{ edit: []; remove: [] }>()

const { $gettext } = useGettext()

const color = computed(() => (props.calendar ? calendarColor(props.calendar) : ''))

// The times are in the zone they were asked for, so they are written as they are.
function wall(value: { dateTime: string; timeZone: string }): DateTime {
  const parsed = DateTime.fromISO(value.dateTime, { zone: value.timeZone })
  return parsed.isValid ? parsed : DateTime.fromISO(value.dateTime, { zone: 'utc' })
}

const when = computed(() => {
  const e = props.event
  if (!e) {
    return ''
  }
  const start = wall(e.start)
  const end = wall(e.end)
  if (e.isAllDay) {
    // The end of an all-day event is the day after its last day.
    const last = end.minus({ days: 1 })
    return last > start
      ? `${start.toLocaleString(DateTime.DATE_FULL)} - ${last.toLocaleString(DateTime.DATE_FULL)}`
      : start.toLocaleString({ weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
  }
  const time = { hour: 'numeric', minute: '2-digit' } as const
  if (start.hasSame(end, 'day')) {
    return $gettext('%{date} · %{from} to %{to}', {
      date: start.toLocaleString({ weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }),
      from: start.toLocaleString(time),
      to: end.toLocaleString(time)
    })
  }
  return `${start.toLocaleString(DateTime.DATETIME_MED)} - ${end.toLocaleString(DateTime.DATETIME_MED)}`
})

// The pattern is on the series, which an occurrence does not carry.
const repeats = computed(() => {
  const source = props.series ?? (props.event?.type === 'seriesMaster' ? props.event : undefined)
  if (!source?.recurrence) {
    return ''
  }
  const form = recurrenceFormFromEvent(source.recurrence, wall(source.start).toFormat('yyyy-MM-dd'))
  return describeRecurrence(form, $gettext)
})

const responses: Record<string, () => string> = {
  accepted: () => $gettext('Accepted'),
  declined: () => $gettext('Declined'),
  tentativelyAccepted: () => $gettext('Maybe')
}
const responseLabel = (response?: string) => (response && responses[response]?.()) || $gettext('No answer')
</script>

<style>
.gw-event-details {
  display: flex;
  flex-direction: column;
  gap: var(--oc-space-small);
  padding: var(--oc-space-small) 0;
}
.gw-event-details p {
  margin: 0;
}
.gw-event-heading {
  display: flex;
  flex-direction: column;
  gap: var(--oc-space-xsmall);
  padding-left: var(--oc-space-small);
  border-left: 4px solid var(--oc-color-border);
}
.gw-event-title {
  margin: 0;
  font-size: var(--oc-font-size-large);
  font-weight: var(--oc-font-weight-semibold);
  overflow-wrap: anywhere;
}
.gw-event-time {
  font-weight: var(--oc-font-weight-semibold);
}
.gw-event-row {
  display: flex;
  align-items: center;
  gap: var(--oc-space-small);
  min-width: 0;
}
.gw-event-tags {
  flex-wrap: wrap;
}
.gw-event-heading-small {
  margin: var(--oc-space-small) 0 0;
  font-size: var(--oc-font-size-default);
  font-weight: var(--oc-font-weight-semibold);
}
.gw-event-notes {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.gw-note {
  display: flex;
  align-items: flex-start;
  gap: var(--oc-space-small);
  padding: var(--oc-space-small);
  border-radius: 5px;
  background: var(--oc-color-background-muted);
  color: var(--oc-color-text-default);
}
.gw-attendee {
  display: flex;
  align-items: center;
  gap: var(--oc-space-small);
  min-height: 2.25rem;
}
.gw-attendee-who {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-width: 0;
}
.gw-attendee-status {
  flex: none;
  font-size: var(--oc-font-size-small);
}
.gw-event-actions {
  display: flex;
  gap: var(--oc-space-small);
  margin-top: var(--oc-space-small);
}
</style>
