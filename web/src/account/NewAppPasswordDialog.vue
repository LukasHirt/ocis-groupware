<template>
  <gw-modal
    :title="title"
    form
    :persistent="!!issued"
    initial-focus="[data-testid=app-password-name]"
    @close="emit('cancel')"
    @submit="submit"
  >
    <template v-if="issued">
      <p class="oc-mt-rm">
        {{ $gettext('Enter this password in your app. Each app needs its own.') }}
      </p>
      <div class="oc-flex oc-flex-bottom gw-secret-row">
        <oc-text-input
          :model-value="issued.password"
          :label="$gettext('App password')"
          read-only
          class="oc-width-expand"
          data-testid="app-password-secret"
        />
        <oc-button
          variation="passive"
          appearance="outline"
          data-testid="app-password-copy"
          @click="copy"
        >
          <oc-icon :name="copied ? 'check' : 'file-copy'" fill-type="line" size="small" />
          {{ copied ? $gettext('Copied') : $gettext('Copy') }}
        </oc-button>
      </div>
      <inline-notice variation="warning" class="oc-mt-s">
        {{ $gettext('Copy it now. It will not be shown again.') }}
      </inline-notice>
      <p v-if="copyFailed" class="oc-text-muted oc-mt-s" role="status">
        {{ $gettext('Copying is not possible here. Select the password and copy it by hand.') }}
      </p>
    </template>
    <template v-else>
      <oc-text-input
        v-model="name"
        :label="$gettext('Name')"
        :placeholder="$gettext('For example, Phone')"
        :error-message="nameError"
        :fix-message-line="true"
        autocomplete="off"
        data-testid="app-password-name"
      />
      <oc-select
        v-model="chosen"
        :label="$gettext('Expires after')"
        :options="lifetimes"
        :searchable="false"
        :clearable="false"
        class="oc-mt-s"
        data-testid="app-password-expiry"
      />
    </template>
    <template #actions>
      <oc-button
        v-if="!issued"
        variation="passive"
        appearance="outline"
        data-testid="dialog-cancel"
        @click="emit('cancel')"
      >
        {{ $gettext('Cancel') }}
      </oc-button>
      <!-- One button for both steps, so the one the user pressed to create the
           password stays in place and keeps the focus when it turns into Done. -->
      <oc-button
        variation="primary"
        appearance="filled"
        :submit="issued ? 'button' : 'submit'"
        :disabled="busy"
        :data-testid="issued ? 'app-password-done' : 'dialog-confirm'"
        @click="issued ? emit('done') : undefined"
      >
        {{ issued ? $gettext('Done') : $gettext('Create') }}
      </oc-button>
    </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'
import { useApi } from '../composables/useApi'
import type { AppPasswordInput, IssuedAppPassword } from '../api/types'
import InlineNotice from '../components/InlineNotice.vue'
import { copyText } from './clipboard'

defineOptions({ name: 'NewAppPasswordDialog' })

const props = defineProps<{
  // The longest lifetime the server accepts, which is also its default.
  maxLifetimeDays: number
}>()
const emit = defineEmits<{ cancel: []; done: [] }>()

const { $gettext } = useGettext()
const { run } = useApi()

// The server's limit is 100 bytes of text. Its default lifetime is the longest
// one it accepts, so the choices are that and the shorter standard ones.
const maxNameBytes = 100
const defaultDays = computed(() => props.maxLifetimeDays)
const lifetimes = computed(() =>
  [...new Set([props.maxLifetimeDays, 90, 30, 7])]
    .filter((d) => d <= props.maxLifetimeDays)
    .sort((a, b) => b - a)
    .map((days) => ({
      days,
      label:
        days === props.maxLifetimeDays
          ? $gettext('%{n} days (default)', { n: String(days) })
          : $gettext('%{n} days', { n: String(days) })
    }))
)
// The chosen option; the longest one until the user picks another.
const picked = ref<{ days: number; label: string }>()
const chosen = computed({
  get: () => picked.value ?? lifetimes.value[0],
  set: (option) => (picked.value = option)
})

const name = ref('')
const nameError = ref('')
const busy = ref(false)
const issued = ref<IssuedAppPassword>()
const copied = ref(false)
const copyFailed = ref(false)

const title = computed(() =>
  issued.value ? $gettext('Your new app password') : $gettext('New app password')
)

function validate(): string {
  const text = name.value.trim()
  if (text === '') {
    return $gettext('Enter a name, so you can tell the devices apart.')
  }
  if (new TextEncoder().encode(text).length > maxNameBytes) {
    return $gettext('The name is too long.')
  }
  return ''
}

async function submit() {
  if (issued.value) {
    return
  }
  nameError.value = validate()
  if (nameError.value || busy.value) {
    return
  }
  const input: AppPasswordInput = { name: name.value.trim() }
  if (chosen.value.days !== defaultDays.value) {
    input.expires_in_days = chosen.value.days
  }
  busy.value = true
  try {
    issued.value = await run($gettext('Could not create the app password'), (c) =>
      c.createAppPassword(input)
    )
  } finally {
    busy.value = false
  }
}

async function copy() {
  copyFailed.value = !(await copyText(issued.value?.password ?? ''))
  copied.value = !copyFailed.value
}
</script>

<style>
.gw-secret-row {
  gap: var(--oc-space-small);
}
</style>
