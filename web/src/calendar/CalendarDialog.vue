<template>
  <modal-dialog :title="title" @close="emit('close')">
    <form novalidate @submit.prevent="submit">
      <label class="oc-text-bold" for="groupware-calendar-name">{{ $gettext('Name') }}</label>
      <input
        id="groupware-calendar-name"
        v-model="name"
        class="oc-text-input oc-width-1-1 oc-mb-xs"
        type="text"
        maxlength="255"
        data-testid="calendar-name"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="error ? 'groupware-calendar-name-error' : undefined"
      />
      <p
        v-if="error"
        id="groupware-calendar-name-error"
        class="oc-text-input-danger oc-mt-rm"
        role="alert"
        data-testid="calendar-name-error"
      >
        {{ error }}
      </p>

      <span id="groupware-calendar-color" class="oc-text-bold oc-display-block oc-mt-m">{{
        $gettext('Colour')
      }}</span>
      <div class="groupware-swatches" role="radiogroup" aria-labelledby="groupware-calendar-color">
        <button
          v-for="hex in swatches"
          :key="hex"
          type="button"
          role="radio"
          class="groupware-color-button"
          :class="{ 'is-selected': hex === color }"
          :style="{ backgroundColor: hex }"
          :aria-checked="hex === color ? 'true' : 'false'"
          :aria-label="hex"
          :data-testid="`calendar-color-${hex}`"
          @click="color = hex"
        />
        <label class="groupware-color-custom oc-ml-s">
          <span class="oc-invisible-sr">{{ $gettext('Choose another colour') }}</span>
          <input
            type="color"
            :value="color"
            data-testid="calendar-color-custom"
            @input="color = ($event.target as HTMLInputElement).value"
          />
        </label>
      </div>

      <div class="groupware-modal-actions">
        <button
          type="button"
          class="oc-button oc-button-m oc-button-passive"
          data-testid="dialog-cancel"
          @click="emit('close')"
        >
          {{ $gettext('Cancel') }}
        </button>
        <button
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
import { computed, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'
import type { GraphCalendar } from '../api/types'
import { calendarColor, palette } from './colors'

defineOptions({ name: 'CalendarDialog' })

const props = defineProps<{
  title: string
  // The calendar to edit; none for a new one.
  calendar?: Pick<GraphCalendar, 'id' | 'name' | 'hexColor'>
}>()
const emit = defineEmits<{
  close: []
  // Only the fields that changed. A new calendar always carries both.
  save: [change: { name?: string; hexColor?: string }]
}>()

const { $gettext } = useGettext()

const initialColor = props.calendar ? calendarColor(props.calendar) : palette[0]
const name = ref(props.calendar?.name ?? '')
const color = ref(initialColor)
const error = ref('')

// The swatches are the palette, plus the current colour when it is not on it.
const swatches = computed(() => (palette.includes(color.value) ? palette : [...palette, color.value]))

function submit() {
  const trimmed = name.value.trim()
  if (trimmed === '') {
    error.value = $gettext('Enter a name for the calendar.')
    return
  }
  if (!props.calendar) {
    emit('save', { name: trimmed, hexColor: color.value })
    return
  }
  const change: { name?: string; hexColor?: string } = {}
  if (trimmed !== props.calendar.name) {
    change.name = trimmed
  }
  if (color.value !== initialColor) {
    change.hexColor = color.value
  }
  if (Object.keys(change).length === 0) {
    emit('close')
    return
  }
  emit('save', change)
}
</script>

<style>
.groupware-swatches {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-top: 0.25rem;
}
.groupware-color-button {
  width: 1.75rem;
  height: 1.75rem;
  border-radius: 50%;
  border: 2px solid transparent;
  cursor: pointer;
}
.groupware-color-button.is-selected {
  border-color: var(--oc-color-text-default);
  outline: 2px solid var(--oc-color-background-default);
  outline-offset: -4px;
}
.groupware-color-custom input {
  width: 2rem;
  height: 2rem;
  padding: 0;
  border: 0;
  background: none;
  cursor: pointer;
}
</style>
