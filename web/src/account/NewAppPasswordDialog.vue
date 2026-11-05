<template>
  <modal-dialog :title="title" @close="dismiss">
    <template v-if="issued">
      <p class="oc-mt-rm">
        {{ $gettext('Enter this password in your app now. It is not shown again.') }}
      </p>
      <div class="oc-flex oc-flex-middle">
        <code class="groupware-secret" data-testid="app-password-secret">{{ issued.password }}</code>
        <button
          type="button"
          class="oc-button oc-button-m oc-button-passive oc-ml-s"
          data-testid="app-password-copy"
          @click="copy"
        >
          {{ copied ? $gettext('Copied') : $gettext('Copy') }}
        </button>
      </div>
      <p v-if="copyFailed" class="oc-text-muted oc-mt-s" role="status">
        {{ $gettext('Copying is not possible here. Select the password and copy it by hand.') }}
      </p>
      <div class="groupware-modal-actions">
        <button
          type="button"
          class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
          data-testid="app-password-done"
          @click="emit('done')"
        >
          {{ $gettext('Done') }}
        </button>
      </div>
    </template>
    <form v-else novalidate @submit.prevent="submit">
      <label class="oc-text-bold" for="groupware-app-password-name">{{ $gettext('Name') }}</label>
      <input
        id="groupware-app-password-name"
        v-model="name"
        class="oc-text-input oc-width-1-1 oc-mb-xs"
        :class="{ 'oc-text-input-danger': nameError }"
        type="text"
        autocomplete="off"
        :placeholder="$gettext('For example, Phone')"
        :aria-invalid="nameError ? 'true' : 'false'"
        :aria-describedby="nameError ? 'groupware-app-password-name-error' : undefined"
        data-testid="app-password-name"
      />
      <p
        v-if="nameError"
        id="groupware-app-password-name-error"
        class="oc-text-input-danger oc-mt-rm"
        data-testid="app-password-name-error"
      >
        {{ nameError }}
      </p>
      <label class="oc-text-bold oc-mt-s oc-display-block" for="groupware-app-password-expiry">
        {{ $gettext('Expires after') }}
      </label>
      <select
        id="groupware-app-password-expiry"
        v-model="days"
        class="oc-select oc-width-1-1"
        data-testid="app-password-expiry"
      >
        <option v-for="d in lifetimes" :key="d" :value="String(d)">
          {{ d === defaultDays ? $gettext('%{n} days (default)', { n: String(d) }) : $gettext('%{n} days', { n: String(d) }) }}
        </option>
      </select>
      <div class="groupware-modal-actions">
        <button
          type="button"
          class="oc-button oc-button-m oc-button-passive"
          data-testid="dialog-cancel"
          @click="emit('cancel')"
        >
          {{ $gettext('Cancel') }}
        </button>
        <button
          type="submit"
          class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
          :disabled="busy"
          data-testid="dialog-confirm"
        >
          {{ $gettext('Create') }}
        </button>
      </div>
    </form>
  </modal-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'
import { useApi } from '../composables/useApi'
import type { AppPasswordInput, IssuedAppPassword } from '../api/types'
import { copyText } from './clipboard'

defineOptions({ name: 'NewAppPasswordDialog' })

const emit = defineEmits<{ cancel: []; done: [] }>()

const { $gettext } = useGettext()
const { run } = useApi()

// The server's limit is 100 bytes of text, and its default lifetime is the
// longest one it accepts.
const maxNameBytes = 100
const defaultDays = 365
const lifetimes = [defaultDays, 90, 30, 7]

const name = ref('')
const days = ref(String(defaultDays))
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
  nameError.value = validate()
  if (nameError.value || busy.value) {
    return
  }
  const input: AppPasswordInput = { name: name.value.trim() }
  if (Number(days.value) !== defaultDays) {
    input.expires_in_days = Number(days.value)
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

// Escape and a click outside close the form. Once the password is shown they
// do nothing, so it cannot be lost by accident; Done closes it.
function dismiss() {
  if (!issued.value) {
    emit('cancel')
  }
}

</script>

<style>
.groupware-secret {
  font-size: 1.1rem;
  word-break: break-all;
  user-select: all;
}
</style>
