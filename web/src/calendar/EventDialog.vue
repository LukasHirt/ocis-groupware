<template>
  <modal-dialog :title="title" wide @close="emit('close')">
    <form novalidate class="groupware-event-form" @submit.prevent="submit">
      <p v-if="notice" class="oc-text-muted oc-mt-rm" data-testid="event-notice">{{ notice }}</p>

      <fieldset :disabled="readOnly" class="groupware-fieldset">
        <label class="oc-text-bold" for="groupware-event-subject">{{ $gettext('Title') }}</label>
        <input
          id="groupware-event-subject"
          v-model="edited.subject"
          class="oc-text-input oc-width-1-1"
          type="text"
          data-testid="event-subject"
        />

        <template v-if="!existing">
          <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-event-calendar">{{
            $gettext('Calendar')
          }}</label>
          <select
            id="groupware-event-calendar"
            v-model="edited.calendarId"
            class="oc-select oc-width-1-1"
            data-testid="event-calendar"
          >
            <option v-for="c in calendars" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </template>

        <label class="oc-flex oc-flex-middle oc-mt-s">
          <input
            v-model="edited.allDay"
            type="checkbox"
            class="groupware-check oc-mr-s"
            data-testid="event-all-day"
          />
          {{ $gettext('All day') }}
        </label>

        <div class="groupware-row oc-mt-xs">
          <div>
            <label for="groupware-event-start-date">{{ $gettext('Starts') }}</label>
            <div class="groupware-row">
              <input
                id="groupware-event-start-date"
                v-model="edited.startDate"
                type="date"
                class="oc-text-input"
                data-testid="event-start-date"
                :aria-invalid="problem === 'start' ? 'true' : undefined"
              />
              <input
                v-if="!edited.allDay"
                v-model="edited.startTime"
                type="time"
                class="oc-text-input"
                :aria-label="$gettext('Start time')"
                data-testid="event-start-time"
              />
            </div>
          </div>
          <div>
            <label for="groupware-event-end-date">{{ $gettext('Ends') }}</label>
            <div class="groupware-row">
              <input
                id="groupware-event-end-date"
                v-model="edited.endDate"
                type="date"
                class="oc-text-input"
                data-testid="event-end-date"
                :aria-invalid="problem === 'end' ? 'true' : undefined"
                :aria-describedby="problem ? 'groupware-event-end-error' : undefined"
              />
              <input
                v-if="!edited.allDay"
                v-model="edited.endTime"
                type="time"
                class="oc-text-input"
                :aria-label="$gettext('End time')"
                data-testid="event-end-time"
              />
            </div>
          </div>
        </div>
        <p
          v-if="problem"
          id="groupware-event-end-error"
          class="oc-text-input-danger oc-mt-xs"
          role="alert"
          :data-testid="`event-${problem}-error`"
        >
          {{
            problem === 'start'
              ? $gettext('Enter a start date and time.')
              : $gettext('The end must be on or after the start.')
          }}
        </p>

        <label class="oc-mt-s oc-display-block" for="groupware-event-zone">{{ $gettext('Time zone') }}</label>
        <select
          id="groupware-event-zone"
          v-model="edited.timeZone"
          class="oc-select oc-width-1-1"
          data-testid="event-zone"
        >
          <option v-for="zone in zones" :key="zone" :value="zone">{{ zone }}</option>
        </select>

        <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-event-location">{{
          $gettext('Place')
        }}</label>
        <input
          id="groupware-event-location"
          v-model="edited.location"
          class="oc-text-input oc-width-1-1"
          type="text"
          data-testid="event-location"
        />

        <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-event-status">{{
          $gettext('Show as')
        }}</label>
        <select
          id="groupware-event-status"
          v-model="edited.showAs"
          class="oc-select oc-width-1-1"
          data-testid="event-status"
        >
          <option v-for="option in statuses" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>

        <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-event-categories">{{
          $gettext('Categories')
        }}</label>
        <input
          id="groupware-event-categories"
          v-model="edited.categories"
          class="oc-text-input oc-width-1-1"
          type="text"
          :placeholder="$gettext('Separate names with commas')"
          data-testid="event-categories"
        />

        <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-attendee-input">{{
          $gettext('Attendees')
        }}</label>
        <ul class="oc-list oc-mb-xs">
          <li
            v-for="a in edited.attendees"
            :key="a.address"
            class="oc-flex oc-flex-middle"
            data-testid="attendee"
          >
            <span>{{ a.name ? `${a.name} <${a.address}>` : a.address }}</span>
            <button
              v-if="!readOnly"
              type="button"
              class="oc-button oc-button-s oc-button-passive oc-ml-s"
              :aria-label="$gettext('Remove %{address}', { address: a.address })"
              data-testid="attendee-remove"
              @click="removeAttendee(a.address)"
            >
              {{ $gettext('Remove') }}
            </button>
          </li>
        </ul>
        <div v-if="!readOnly" class="groupware-row">
          <input
            id="groupware-attendee-input"
            v-model="attendeeText"
            class="oc-text-input groupware-grow"
            type="email"
            :placeholder="$gettext('Email address')"
            data-testid="attendee-input"
            @keydown.enter.prevent="addAttendee"
          />
          <button
            type="button"
            class="oc-button oc-button-m oc-button-passive"
            data-testid="attendee-add"
            @click="addAttendee"
          >
            {{ $gettext('Add') }}
          </button>
        </div>
        <p
          v-if="attendeeProblem"
          class="oc-text-input-danger oc-mt-xs"
          role="alert"
          data-testid="attendee-error"
        >
          {{
            attendeeProblem === 'invalid'
              ? $gettext('Enter a valid email address.')
              : $gettext('This person is already invited.')
          }}
        </p>

        <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-event-notes">{{
          $gettext('Notes')
        }}</label>
        <textarea
          id="groupware-event-notes"
          v-model="edited.notes"
          class="oc-textarea oc-width-1-1"
          rows="3"
          data-testid="event-notes"
        />
      </fieldset>

      <div class="groupware-modal-actions">
        <button
          v-if="existing && !readOnly"
          type="button"
          class="oc-button oc-button-m oc-button-danger groupware-push"
          data-testid="event-delete"
          @click="emit('remove')"
        >
          {{ $gettext('Delete') }}
        </button>
        <button
          type="button"
          class="oc-button oc-button-m oc-button-passive"
          data-testid="dialog-cancel"
          @click="emit('close')"
        >
          {{ readOnly ? $gettext('Close') : $gettext('Cancel') }}
        </button>
        <button
          v-if="!readOnly"
          type="submit"
          class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
          data-testid="dialog-confirm"
        >
          {{ $gettext('Save') }}
        </button>
      </div>
    </form>
  </modal-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'
import type { EventInput, GraphCalendar } from '../api/types'
import {
  attendeeError,
  changeFromForms,
  formProblem,
  inputFromForm,
  type EventForm
} from './eventForm'
import { zoneNames } from './zones'

defineOptions({ name: 'EventDialog' })

const props = defineProps<{
  title: string
  form: EventForm
  // The calendars the event can be created in.
  calendars: GraphCalendar[]
  // The event exists already: the dialog edits it and offers delete.
  existing?: boolean
  readOnly?: boolean
  notice?: string
}>()
const emit = defineEmits<{
  close: []
  remove: []
  // A new event carries the whole input; an existing one only the changes.
  save: [save: { calendarId: string; input: EventInput }]
}>()

const { $gettext } = useGettext()

// The props are reactive proxies, which structuredClone refuses.
const copy = (): EventForm => JSON.parse(JSON.stringify(props.form))
const original = copy()
const edited = reactive<EventForm>(copy())
const zones = computed(() => zoneNames(original.timeZone, edited.timeZone))

const statuses = computed(() => [
  { value: 'free', label: $gettext('Free') },
  { value: 'tentative', label: $gettext('Tentative') },
  { value: 'busy', label: $gettext('Busy') },
  { value: 'oof', label: $gettext('Away') },
  { value: 'workingElsewhere', label: $gettext('Working elsewhere') },
  { value: 'unknown', label: $gettext('Unknown') }
])

const attendeeText = ref('')
const attendeeProblem = ref<'invalid' | 'duplicate' | undefined>()
const submitted = ref(false)

const problem = computed(() => (submitted.value ? formProblem(edited) : undefined))

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
.groupware-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.groupware-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.groupware-grow {
  flex: 1 1 auto;
}
.groupware-push {
  margin-right: auto;
}
.groupware-event-form .oc-display-block {
  display: block;
}
</style>
