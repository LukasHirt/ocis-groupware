<template>
  <gw-modal
    :title="title"
    form
    initial-focus="input[data-testid=calendar-name]"
    @close="emit('close')"
    @submit="submit"
  >
    <oc-text-input
      id="gw-calendar-name"
      v-model="name"
      :label="$gettext('Name')"
      :error-message="error"
      :fix-message-line="true"
      maxlength="255"
      data-testid="calendar-name"
    />

    <span id="gw-calendar-color" class="gw-field-label">{{ $gettext('Colour') }}</span>
    <div class="gw-swatches" role="radiogroup" aria-labelledby="gw-calendar-color">
      <button
        v-for="(hex, i) in palette"
        :key="hex"
        :ref="(el: unknown) => (swatchButtons[i] = el as HTMLButtonElement | null)"
        type="button"
        role="radio"
        class="gw-swatch-button"
        :class="{ 'gw-swatch-selected': hex === chosen }"
        :style="{ backgroundColor: hex }"
        :aria-checked="hex === chosen ? 'true' : 'false'"
        :aria-label="colorName(hex, $gettext)"
        :tabindex="i === tabStop ? 0 : -1"
        :data-testid="`calendar-color-${hex}`"
        @click="color = hex"
        @keydown="onKey($event, i)"
      />
      <!-- The custom colour is the picker of the browser, which covers a
           round swatch. A key press on the swatch opens the picker. -->
      <span
        role="radio"
        class="gw-swatch-button gw-swatch-custom"
        :class="{ 'gw-swatch-selected': custom }"
        :style="custom ? { backgroundColor: color } : undefined"
        :aria-checked="custom ? 'true' : 'false'"
        :aria-label="$gettext('Custom colour')"
        tabindex="0"
        @keydown.enter.prevent="picker?.click()"
        @keydown.space.prevent="picker?.click()"
      >
        <oc-icon name="pencil" fill-type="line" size="small" :color="custom ? contrast : undefined" />
        <input
          ref="picker"
          type="color"
          class="gw-swatch-picker"
          tabindex="-1"
          :value="color"
          :aria-label="$gettext('Custom colour')"
          data-testid="calendar-color-custom"
          @input="color = ($event.target as HTMLInputElement).value"
        />
      </span>
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
import { computed, nextTick, ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'
import type { GraphCalendar } from '../api/types'
import { calendarColor, colorName, palette, readableTextColor } from './colors'

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
// The message goes when the name is edited.
watch(name, () => (error.value = ''))

// A colour that is not on the palette is a custom one, shown on the last swatch.
const chosen = computed(() => color.value.toLowerCase())
const custom = computed(() => !palette.includes(chosen.value))
const contrast = computed(() => readableTextColor(color.value))
const picker = ref<HTMLInputElement>()

// The swatches are one tab stop: the chosen one, or the first when the colour
// is custom. The arrow keys move the choice, around the palette.
const swatchButtons = ref<(HTMLButtonElement | null)[]>([])
const tabStop = computed(() => Math.max(0, palette.indexOf(chosen.value)))

async function onKey(event: KeyboardEvent, index: number) {
  const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[event.key]
  if (step === undefined) {
    return
  }
  event.preventDefault()
  const next = (index + step + palette.length) % palette.length
  color.value = palette[next]
  await nextTick()
  swatchButtons.value[next]?.focus()
}

function submit() {
  const trimmed = name.value.trim()
  if (trimmed === '') {
    error.value = $gettext('Enter a name for the calendar.')
    return
  }
  error.value = ''
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
.gw-field-label {
  display: block;
  margin-top: var(--oc-space-medium);
  font-size: var(--oc-font-size-default);
  font-weight: var(--oc-font-weight-semibold);
}
.gw-swatches {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--oc-space-small);
  margin-top: var(--oc-space-small);
}
.gw-swatch-button {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  padding: 0;
  border: 2px solid transparent;
  border-radius: 50%;
  cursor: pointer;
}
.gw-swatch-button:focus-visible {
  outline: 2px solid var(--oc-color-swatch-primary-default);
  outline-offset: 2px;
}
.gw-swatch-selected {
  border-color: var(--oc-color-text-default);
  box-shadow: inset 0 0 0 2px var(--oc-color-background-secondary);
}
.gw-swatch-custom {
  background: var(--oc-color-background-muted);
  border-color: var(--oc-color-border);
}
.gw-swatch-custom.gw-swatch-selected {
  border-color: var(--oc-color-text-default);
}
.gw-swatch-picker {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  opacity: 0;
  cursor: pointer;
}
</style>
