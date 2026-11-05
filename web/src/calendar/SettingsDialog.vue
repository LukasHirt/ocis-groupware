<template>
  <gw-modal
    :title="$gettext('Calendar settings')"
    form
    initial-focus="[data-testid=settings-zone] input"
    @close="emit('close')"
    @submit="submit"
  >
    <oc-select
      v-model="edited.timeZone"
      :label="$gettext('Time zone')"
      :options="zoneOptions"
      :reduce="(o: Choice<string>) => o.value"
      :clearable="false"
      searchable
      class="oc-mb-s"
      data-testid="settings-zone"
    />
    <oc-select
      v-model="edited.firstDay"
      :label="$gettext('First day of the week')"
      :options="dayOptions"
      :reduce="(o: Choice<number>) => o.value"
      :clearable="false"
      :searchable="false"
      class="oc-mb-m"
      data-testid="settings-first-day"
    />
    <oc-checkbox
      v-model="edited.weekNumbers"
      :label="$gettext('Show week numbers')"
      data-testid="settings-week-numbers"
    />

    <p class="oc-text-muted oc-text-small oc-mt-m oc-mb-rm">
      {{ $gettext('These settings are kept in this browser.') }}
    </p>

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
import { computed, reactive } from 'vue'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'
import type { CalendarSettings } from './settings'
import { zoneNames } from './zones'

defineOptions({ name: 'SettingsDialog' })

const props = defineProps<{ settings: CalendarSettings; browserZone: string }>()
const emit = defineEmits<{ close: []; save: [settings: CalendarSettings] }>()

const { $gettext } = useGettext()

const edited = reactive<CalendarSettings>({ ...props.settings })
const zones = computed(() => zoneNames(props.settings.timeZone))
interface Choice<T> {
  label: string
  value: T
}

const zoneOptions = computed<Choice<string>[]>(() => [
  { label: $gettext('Browser time zone (%{zone})', { zone: props.browserZone }), value: '' },
  ...zones.value.map((z) => ({ label: z, value: z }))
])
const dayNames = computed(() => [
  $gettext('Sunday'),
  $gettext('Monday'),
  $gettext('Tuesday'),
  $gettext('Wednesday'),
  $gettext('Thursday'),
  $gettext('Friday'),
  $gettext('Saturday')
])

const dayOptions = computed<Choice<number>[]>(() => dayNames.value.map((label, value) => ({ label, value })))

function submit() {
  emit('save', { ...edited })
}
</script>
