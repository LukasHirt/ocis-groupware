<template>
  <gw-modal
    :title="title"
    wide
    form
    initial-focus="input[data-testid=event-subject]"
    @close="emit('close')"
    @submit="submit"
  >
    <div class="gw-event-form">
      <oc-text-input
        id="gw-event-subject"
        v-model="edited.subject"
        :label="$gettext('Title')"
        class="oc-mb-s"
        data-testid="event-subject"
      />

      <oc-select
        v-if="!existing"
        id="gw-event-calendar"
        v-model="edited.calendarId"
        :label="$gettext('Calendar')"
        :options="calendarOptions"
        :reduce="(o: Choice<string>) => o.value"
        :clearable="false"
        :searchable="false"
        class="oc-mb-s"
        data-testid="event-calendar"
      />

      <oc-checkbox v-model="edited.allDay" :label="$gettext('All day')" class="oc-mb-s" data-testid="event-all-day" />

      <div class="gw-event-when" :class="{ 'gw-event-when-all-day': edited.allDay }">
        <oc-text-input
          id="gw-event-start-date"
          v-model="edited.startDate"
          type="date"
          :label="$gettext('Starts')"
          :error-message="problem === 'start' ? startMessage : ''"
          data-testid="event-start-date"
        />
        <oc-text-input
          v-if="!edited.allDay"
          id="gw-event-start-time"
          v-model="edited.startTime"
          type="time"
          :label="$gettext('Start time')"
          data-testid="event-start-time"
        />
        <oc-text-input
          id="gw-event-end-date"
          v-model="edited.endDate"
          type="date"
          :label="$gettext('Ends')"
          :error-message="problem === 'end' && edited.allDay ? endMessage : ''"
          data-testid="event-end-date"
        />
        <oc-text-input
          v-if="!edited.allDay"
          id="gw-event-end-time"
          v-model="edited.endTime"
          type="time"
          :label="$gettext('End time')"
          :error-message="problem === 'end' ? endMessage : ''"
          data-testid="event-end-time"
        />
      </div>

      <oc-select
        id="gw-event-zone"
        v-model="edited.timeZone"
        :label="$gettext('Time zone')"
        :options="zones"
        :clearable="false"
        searchable
        class="oc-mb-s"
        data-testid="event-zone"
      />

      <template v-if="!hideRecurrence">
        <oc-select
          id="gw-repeat-frequency"
          v-model="edited.recurrence.frequency"
          :label="$gettext('Repeat')"
          :options="frequencyOptions"
          :reduce="(o: Choice<string>) => o.value"
          :clearable="false"
          :searchable="false"
          class="oc-mb-s"
          data-testid="repeat-frequency"
        />

        <div v-if="rec.frequency !== 'none'" class="gw-event-repeat" data-testid="repeat-pattern">
          <div class="gw-event-repeat-row">
            <oc-text-input
              id="gw-repeat-interval"
              :model-value="String(rec.interval)"
              type="number"
              min="1"
              max="99"
              :label="$gettext('Every')"
              :error-message="repeatProblem === 'interval' ? repeatMessage : ''"
              class="gw-narrow"
              data-testid="repeat-interval"
              @update:model-value="(v: string) => (rec.interval = Number(v))"
            />
            <span class="gw-event-unit">{{ unit }}</span>
          </div>

          <div v-if="rec.frequency === 'monthly' || rec.frequency === 'yearly'" class="gw-event-repeat-row">
            <oc-select
              id="gw-repeat-by"
              v-model="rec.monthlyBy"
              :label="$gettext('Repeat on')"
              :options="byOptions"
              :reduce="(o: Choice<string>) => o.value"
              :clearable="false"
              :searchable="false"
              data-testid="repeat-monthly-by"
            />
            <oc-select
              v-if="rec.frequency === 'yearly'"
              id="gw-repeat-month"
              v-model="rec.month"
              :label="$gettext('Month')"
              :options="monthOptions"
              :reduce="(o: Choice<number>) => o.value"
              :clearable="false"
              :searchable="false"
              data-testid="repeat-month"
            />
            <oc-text-input
              v-if="rec.monthlyBy === 'day'"
              id="gw-repeat-day-of-month"
              :model-value="String(rec.dayOfMonth)"
              type="number"
              min="1"
              max="31"
              :label="$gettext('Day')"
              :error-message="repeatProblem === 'dayOfMonth' ? repeatMessage : ''"
              class="gw-narrow"
              data-testid="repeat-day-of-month"
              @update:model-value="(v: string) => (rec.dayOfMonth = Number(v))"
            />
            <oc-select
              v-else
              id="gw-repeat-index"
              v-model="rec.index"
              :label="$gettext('Which one')"
              :options="indexOptions"
              :reduce="(o: Choice<string>) => o.value"
              :clearable="false"
              :searchable="false"
              data-testid="repeat-index"
            />
          </div>

          <div
            v-if="rec.frequency === 'weekly' || (rec.frequency !== 'daily' && rec.monthlyBy === 'weekday')"
            class="gw-event-days"
          >
            <span id="gw-repeat-days-label" class="gw-field-label">{{ $gettext('Repeat on') }}</span>
            <div class="gw-event-chips" role="group" aria-labelledby="gw-repeat-days-label">
              <oc-button
                v-for="d in dayOptions"
                :key="d.value"
                size="small"
                variation="primary"
                :appearance="rec.daysOfWeek.includes(d.value) ? 'filled' : 'outline'"
                :aria-pressed="rec.daysOfWeek.includes(d.value) ? 'true' : 'false'"
                :aria-label="d.label"
                :data-testid="`repeat-day-${d.value}`"
                @click="toggleDay(d.value)"
              >
                {{ d.short }}
              </oc-button>
            </div>
            <p
              v-if="repeatProblem === 'days'"
              id="gw-repeat-days-message"
              class="gw-event-error"
              role="alert"
              data-testid="repeat-days-error"
            >
              {{ repeatMessage }}
            </p>
          </div>

          <div class="gw-event-repeat-row">
            <oc-select
              id="gw-repeat-end"
              v-model="rec.end"
              :label="$gettext('Ends')"
              :options="endOptions"
              :reduce="(o: Choice<string>) => o.value"
              :clearable="false"
              :searchable="false"
              data-testid="repeat-end"
            />
            <oc-text-input
              v-if="rec.end === 'count'"
              id="gw-repeat-count"
              :model-value="String(rec.count)"
              type="number"
              min="1"
              max="999"
              :label="$gettext('Times')"
              :error-message="repeatProblem === 'count' ? repeatMessage : ''"
              class="gw-narrow"
              data-testid="repeat-count"
              @update:model-value="(v: string) => (rec.count = Number(v))"
            />
            <oc-text-input
              v-if="rec.end === 'date'"
              id="gw-repeat-end-date"
              v-model="rec.endDate"
              type="date"
              :label="$gettext('End date')"
              :error-message="repeatProblem === 'endDate' ? repeatMessage : ''"
              data-testid="repeat-end-date"
            />
          </div>
        </div>
      </template>

      <oc-text-input
        id="gw-event-location"
        v-model="edited.location"
        :label="$gettext('Place')"
        class="oc-mb-s"
        data-testid="event-location"
      />

      <oc-select
        id="gw-event-reminder"
        v-model="reminderValue"
        :label="$gettext('Reminder')"
        :options="reminderChoiceOptions"
        :reduce="(o: Choice<string>) => o.value"
        :clearable="false"
        :searchable="false"
        class="oc-mb-s"
        data-testid="event-reminder"
      />

      <oc-select
        id="gw-event-status"
        v-model="edited.showAs"
        :label="$gettext('Show as')"
        :options="statuses"
        :reduce="(o: Choice<string>) => o.value"
        :clearable="false"
        :searchable="false"
        class="oc-mb-s"
        data-testid="event-status"
      />

      <oc-text-input
        id="gw-event-categories"
        v-model="edited.categories"
        :label="$gettext('Categories')"
        :description-message="$gettext('Separate names with commas')"
        class="oc-mb-s"
        data-testid="event-categories"
      />

      <h3 class="gw-field-label">{{ $gettext('Attendees') }}</h3>
      <ul v-if="edited.attendees.length > 0" class="oc-list oc-mb-xs">
        <li v-for="a in edited.attendees" :key="a.address" class="gw-attendee" data-testid="attendee">
          <oc-avatar :user-name="a.name || a.address" :width="24" />
          <span class="gw-attendee-who oc-text-truncate">{{ a.name ? `${a.name} <${a.address}>` : a.address }}</span>
          <oc-button
            appearance="raw"
            :aria-label="$gettext('Remove %{address}', { address: a.address })"
            data-testid="attendee-remove"
            @click="removeAttendee(a.address)"
          >
            <oc-icon name="close" fill-type="line" size="small" />
          </oc-button>
        </li>
      </ul>
      <div class="gw-event-attendee-add">
        <oc-text-input
          id="gw-attendee-input"
          v-model="attendeeText"
          type="email"
          :label="$gettext('Email address')"
          :error-message="attendeeMessage"
          class="oc-mb-s"
          data-testid="attendee-input"
          @keydown.enter.prevent="addAttendee"
        />
        <oc-button variation="passive" appearance="outline" data-testid="attendee-add" @click="addAttendee">
          {{ $gettext('Add') }}
        </oc-button>
      </div>

      <oc-textarea
        id="gw-event-notes"
        v-model="edited.notes"
        :label="$gettext('Notes')"
        rows="4"
        data-testid="event-notes"
      />
    </div>
    <template #actions>
      <oc-button variation="passive" appearance="outline" data-testid="dialog-cancel" @click="emit('close')">
        {{ $gettext('Cancel') }}
      </oc-button>
      <oc-button variation="primary" appearance="filled" submit="submit" data-testid="dialog-confirm">
        {{ $gettext('Save') }}
      </oc-button>
    </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { Info } from 'luxon'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'
import type { EventInput, GraphCalendar } from '../api/types'
import {
  attendeeError,
  changeFromForms,
  formProblem,
  inputFromForm,
  type EventForm
} from './eventForm'
import { reminderLabel, reminderOptions } from './reminders'
import { showAsOptions } from './showAs'
import { weekdays } from './recurrence'
import { zoneNames } from './zones'

defineOptions({ name: 'EventDialog' })

const props = defineProps<{
  title: string
  form: EventForm
  // The calendars the event can be created in.
  calendars: GraphCalendar[]
  // The event exists already: the dialog edits only what changed.
  existing?: boolean
  // One occurrence of a series is edited, so its pattern is not.
  hideRecurrence?: boolean
}>()
const emit = defineEmits<{
  close: []
  // A new event carries the whole input; an existing one only the changes.
  save: [save: { calendarId: string; input: EventInput }]
}>()

const { $gettext } = useGettext()

// The props are reactive proxies, which structuredClone refuses.
const copy = (): EventForm => JSON.parse(JSON.stringify(props.form))
const original = copy()
const edited = reactive<EventForm>(copy())
const zones = computed(() => zoneNames(original.timeZone, edited.timeZone))

interface Choice<T> {
  label: string
  value: T
}

const calendarOptions = computed<Choice<string>[]>(() =>
  props.calendars.map((c) => ({ label: c.name, value: c.id }))
)
const statuses = computed(() => showAsOptions($gettext))
const frequencyOptions = computed<Choice<string>[]>(() => [
  { value: 'none', label: $gettext('Does not repeat') },
  { value: 'daily', label: $gettext('Daily') },
  { value: 'weekly', label: $gettext('Weekly') },
  { value: 'monthly', label: $gettext('Monthly') },
  { value: 'yearly', label: $gettext('Yearly') }
])
const byOptions = computed<Choice<string>[]>(() => [
  { value: 'day', label: $gettext('On a day of the month') },
  { value: 'weekday', label: $gettext('On a weekday') }
])
const endOptions = computed<Choice<string>[]>(() => [
  { value: 'never', label: $gettext('Never') },
  { value: 'count', label: $gettext('After a number of times') },
  { value: 'date', label: $gettext('On a date') }
])

const rec = computed(() => edited.recurrence)
const unit = computed(() =>
  ({
    none: '',
    daily: $gettext('day(s)'),
    weekly: $gettext('week(s)'),
    monthly: $gettext('month(s)'),
    yearly: $gettext('year(s)')
  })[rec.value.frequency]
)
const monthNames = computed(() => [
  $gettext('January'),
  $gettext('February'),
  $gettext('March'),
  $gettext('April'),
  $gettext('May'),
  $gettext('June'),
  $gettext('July'),
  $gettext('August'),
  $gettext('September'),
  $gettext('October'),
  $gettext('November'),
  $gettext('December')
])
// The weekdays as toggle buttons: a letter to read, the whole name to announce.
const dayOptions = computed(() => {
  const long = Info.weekdays('long')
  const narrow = Info.weekdays('narrow')
  return weekdays.map((value, i) => ({ value, label: long[i], short: narrow[i] }))
})
function toggleDay(day: string) {
  const days = rec.value.daysOfWeek
  const i = days.indexOf(day)
  if (i >= 0) {
    days.splice(i, 1)
  } else {
    days.push(day)
  }
}
const monthOptions = computed<Choice<number>[]>(() =>
  monthNames.value.map((label, i) => ({ label, value: i + 1 }))
)
const indexOptions = computed<Choice<string>[]>(() => [
  { value: 'first', label: $gettext('First') },
  { value: 'second', label: $gettext('Second') },
  { value: 'third', label: $gettext('Third') },
  { value: 'fourth', label: $gettext('Fourth') },
  { value: 'last', label: $gettext('Last') }
])
const repeatMessages = computed<Record<string, string>>(() => ({
  interval: $gettext('Enter a whole number between 1 and 99 for how often it repeats.'),
  days: $gettext('Pick at least one day of the week.'),
  dayOfMonth: $gettext('The day of the month must be between 1 and 31.'),
  count: $gettext('The number of times must be between 1 and 999.'),
  endDate: $gettext('The last date must not be before the start.')
}))

const reminderChoiceOptions = computed<Choice<string>[]>(() => [
  { value: 'none', label: $gettext('No reminder') },
  ...reminderOptions(original.reminder).map((m) => ({ value: String(m), label: reminderLabel(m, $gettext) }))
])
// The select holds text, and no reminder is "none".
const reminderValue = computed({
  get: () => (edited.reminder === null ? 'none' : String(edited.reminder)),
  set: (value: string) => (edited.reminder = value === 'none' ? null : Number(value))
})

const attendeeText = ref('')
const attendeeProblem = ref<'invalid' | 'duplicate' | undefined>()
const submitted = ref(false)

const anyProblem = computed(() => (submitted.value ? formProblem(edited) : undefined))
const problem = computed(() => (anyProblem.value === 'start' || anyProblem.value === 'end' ? anyProblem.value : undefined))
const repeatProblem = computed(() => (anyProblem.value && !problem.value ? anyProblem.value : undefined))
const startMessage = computed(() => $gettext('Enter a start date and time.'))
const endMessage = computed(() => $gettext('The end must be on or after the start.'))
const repeatMessage = computed(() => (repeatProblem.value ? repeatMessages.value[repeatProblem.value] : ''))
const attendeeMessage = computed(() =>
  attendeeProblem.value === 'invalid'
    ? $gettext('Enter a valid email address.')
    : attendeeProblem.value === 'duplicate'
      ? $gettext('This person is already invited.')
      : ''
)

// addAttendee takes the address from the field. It answers whether the field
// is empty or its content was added, so a half-typed address blocks the save.
function addAttendee(): boolean {
  const address = attendeeText.value.trim()
  if (address === '') {
    attendeeProblem.value = undefined
    return true
  }
  attendeeProblem.value = attendeeError(address, edited.attendees)
  if (attendeeProblem.value) {
    return false
  }
  edited.attendees.push({ type: 'required', name: '', address })
  attendeeText.value = ''
  return true
}

function removeAttendee(address: string) {
  const i = edited.attendees.findIndex((a) => a.address === address)
  if (i >= 0) {
    edited.attendees.splice(i, 1)
  }
}

function submit() {
  submitted.value = true
  if (!addAttendee() || formProblem(edited)) {
    return
  }
  if (!props.existing) {
    emit('save', { calendarId: edited.calendarId, input: inputFromForm(edited) })
    return
  }
  const change = changeFromForms(original, edited)
  if (Object.keys(change).length === 0) {
    emit('close')
    return
  }
  emit('save', { calendarId: edited.calendarId, input: change })
}
</script>

<style>
.gw-event-form {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.gw-event-when {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 7.5rem minmax(0, 1fr) 7.5rem;
  gap: var(--oc-space-small);
  margin-bottom: var(--oc-space-small);
}
.gw-event-when-all-day {
  grid-template-columns: 1fr 1fr;
}
@media (max-width: 639px) {
  .gw-event-when {
    grid-template-columns: minmax(0, 1fr) 7.5rem;
  }
  .gw-event-when-all-day {
    grid-template-columns: 1fr;
  }
}
/* The pattern is indented under Repeat, with a rule at its left. */
.gw-event-repeat {
  margin: 0 0 var(--oc-space-small) var(--oc-space-small);
  padding-left: var(--oc-space-medium);
  border-left: 2px solid var(--oc-color-border);
}
.gw-event-repeat-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--oc-space-small);
  margin-bottom: var(--oc-space-small);
}
.gw-event-repeat-row > .v-select,
.gw-event-repeat-row > div {
  min-width: 0;
}
.gw-event-unit {
  padding-bottom: var(--oc-space-small);
}
.gw-narrow {
  width: 6rem;
}
.gw-event-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--oc-space-xsmall);
  margin-top: var(--oc-space-xsmall);
}
.gw-event-days {
  margin-bottom: var(--oc-space-small);
}
.gw-event-error {
  margin: var(--oc-space-xsmall) 0 0;
  color: var(--oc-color-swatch-danger-default);
}
.gw-attendee-who {
  flex: 1 1 auto;
  min-width: 0;
}
.gw-event-attendee-add {
  display: flex;
  align-items: flex-end;
  gap: var(--oc-space-small);
}
.gw-event-attendee-add > div:first-child {
  flex: 1 1 auto;
}
/* The pickers of the browser follow the theme. */
.gw-modal input[type='date'],
.gw-modal input[type='time'] {
  color-scheme: light dark;
}
</style>
