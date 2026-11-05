<template>
  <oc-tr class="gw-copy-field">
    <oc-th scope="row" class="gw-copy-label oc-text-muted" width="shrink">{{ label }}</oc-th>
    <oc-td class="gw-copy-value" width="expand">
      <span :data-testid="`${id}-value`">{{ value }}</span>
    </oc-td>
    <oc-td width="shrink" align-h="right">
      <oc-button
        v-oc-tooltip="copied ? $gettext('Copied') : $gettext('Copy')"
        appearance="raw"
        :aria-label="$gettext('Copy %{what}', { what: label })"
        :data-testid="`copy-${id}`"
        @click="copy"
      >
        <oc-icon :name="copied ? 'check' : 'file-copy'" fill-type="line" size="small" />
      </oc-button>
      <span class="oc-invisible-sr" role="status">{{ copied ? $gettext('Copied') : '' }}</span>
    </oc-td>
  </oc-tr>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import { copyText } from './clipboard'

defineOptions({ name: 'CopyField' })

// One row of the connection table: what it is, the value, and a button that
// copies the value. The row goes inside an oc-tbody.
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
.gw-copy-label {
  white-space: nowrap;
  font-weight: normal;
  text-align: left;
}
.gw-copy-value {
  overflow-wrap: anywhere;
}
</style>
