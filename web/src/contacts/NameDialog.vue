<template>
  <gw-modal
    :title="title"
    form
    :initial-focus="`#${inputId}`"
    @close="emit('close')"
    @submit="save"
  >
    <oc-text-input
      :id="inputId"
      v-model="name"
      :label="label"
      maxlength="255"
      :data-testid="`${testid}-input`"
    />
    <template #actions>
      <oc-button variation="passive" appearance="outline" data-testid="dialog-cancel" @click="emit('close')">
        {{ $gettext('Cancel') }}
      </oc-button>
      <oc-button
        variation="primary"
        appearance="filled"
        submit="submit"
        :disabled="!name.trim()"
        :data-testid="`${testid}-save`"
      >
        {{ confirmLabel }}
      </oc-button>
    </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'

defineOptions({ name: 'NameDialog' })

// A dialog with one field for a name, used to create and rename an address
// book and to rename a group. The test ids of the field and the confirm
// button start with testid, as in book-new-input and book-new-save.
const props = defineProps<{
  title: string
  label: string
  confirmLabel: string
  testid: string
  initial?: string
}>()
const emit = defineEmits<{ close: []; save: [name: string] }>()

const { $gettext } = useGettext()
const name = ref(props.initial ?? '')
const inputId = `gw-${props.testid}-name`

function save() {
  const trimmed = name.value.trim()
  if (trimmed) emit('save', trimmed)
}
</script>
