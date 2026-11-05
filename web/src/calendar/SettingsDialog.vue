<template>
  <modal-dialog :title="$gettext('Calendar settings')" @close="emit('close')">
    <form novalidate @submit.prevent="submit">
      <label class="oc-text-bold" for="groupware-settings-zone">{{ $gettext('Time zone') }}</label>
      <select
        id="groupware-settings-zone"
        v-model="edited.timeZone"
        class="oc-select oc-width-1-1"
        data-testid="settings-zone"
      >
        <option value="">{{ $gettext('Browser time zone (%{zone})', { zone: browserZone }) }}</option>
        <option v-for="z in zones" :key="z" :value="z">{{ z }}</option>
      </select>

      <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-settings-first-day">{{
        $gettext('First day of the week')
      }}</label>
      <select
        id="groupware-settings-first-day"
        v-model.number="edited.firstDay"
        class="oc-select oc-width-1-1"
        data-testid="settings-first-day"
      >
        <option v-for="(name, i) in dayNames" :key="name" :value="i">{{ name }}</option>
      </select>

      <label class="oc-flex oc-flex-middle oc-mt-m">
        <input
          v-model="edited.weekNumbers"
          type="checkbox"
          class="groupware-check oc-mr-s"
          data-testid="settings-week-numbers"
        />
        {{ $gettext('Show week numbers') }}
      </label>

      <p class="oc-text-muted oc-mt-m">
        {{ $gettext('These settings are kept in this browser.') }}
      </p>

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
import { computed, reactive } from 'vue'
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'
import type { CalendarSettings } from './settings'
import { zoneNames } from './zones'

defineOptions({ name: 'SettingsDialog' })

const props = defineProps<{ settings: CalendarSettings; browserZone: string }>()
const emit = defineEmits<{ close: []; save: [settings: CalendarSettings] }>()

const { $gettext } = useGettext()

const edited = reactive<CalendarSettings>({ ...props.settings })
const zones = computed(() => zoneNames(props.settings.timeZone))
const dayNames = computed(() => [
  $gettext('Sunday'),
  $gettext('Monday'),
  $gettext('Tuesday'),
  $gettext('Wednesday'),
  $gettext('Thursday'),
  $gettext('Friday'),
  $gettext('Saturday')
])

function submit() {
  emit('save', { ...edited })
}
</script>
