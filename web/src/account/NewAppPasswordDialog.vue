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
        {{ $gettext('Enter this password in your app now. It is not shown again.') }}
      </p>
      <div class="oc-flex oc-flex-middle">
        <code class="groupware-secret" data-testid="app-password-secret">{{ issued.password }}</code>
        <oc-button
          variation="passive"
          appearance="outline"
          class="oc-ml-s"
          data-testid="app-password-copy"
          @click="copy"
        >
          {{ copied ? $gettext('Copied') : $gettext('Copy') }}
        </oc-button>
      </div>
      <p v-if="copyFailed" class="oc-text-muted oc-mt-s" role="status">
        {{ $gettext('Copying is not possible here. Select the password and copy it by hand.') }}
      </p>
    </template>
    <template v-else>
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
)

const name = ref('')
const days = ref(String(props.maxLifetimeDays))
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
  if (Number(days.value) !== defaultDays.value) {
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

</script>

<style>
.groupware-secret {
  font-size: 1.1rem;
  word-break: break-all;
  user-select: all;
}
</style>
