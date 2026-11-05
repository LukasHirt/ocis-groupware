<template>
  <div class="oc-flex oc-flex-middle oc-mb-xs groupware-copy-field">
    <span class="groupware-copy-label oc-text-muted">{{ label }}</span>
    <code class="groupware-copy-value" :data-testid="`${id}-value`">{{ value }}</code>
    <button
      type="button"
      class="oc-button oc-button-s oc-button-passive oc-ml-s"
      :aria-label="$gettext('Copy %{what}', { what: label })"
      :data-testid="`copy-${id}`"
      @click="copy"
    >
      {{ copied ? $gettext('Copied') : $gettext('Copy') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import { copyText } from './clipboard'

defineOptions({ name: 'CopyField' })

const props = defineProps<{ id: string; label: string; value: string }>()

const { $gettext } = useGettext()
const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  if (await copyText(props.value)) {
    copied.value = true
    clearTimeout(timer)
    timer = setTimeout(() => (copied.value = false), 2000)
  }
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<style>
.groupware-copy-label {
  min-width: 9rem;
}
.groupware-copy-value {
  word-break: break-all;
}
</style>
