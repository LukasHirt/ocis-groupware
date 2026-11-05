<template>
  <gw-modal
    :title="title"
    :variation="variation"
    :icon="icon"
    initial-focus="[data-testid=dialog-cancel]"
    @close="emit('cancel')"
  >
    <p class="oc-mt-rm">{{ message }}</p>
    <template #actions>
      <oc-button variation="passive" appearance="outline" data-testid="dialog-cancel" @click="emit('cancel')">
        {{ $gettext('Cancel') }}
      </oc-button>
      <oc-button :variation="variation" appearance="filled" data-testid="dialog-confirm" @click="emit('confirm')">
        {{ confirmLabel }}
      </oc-button>
    </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import GwModal from './GwModal.vue'

defineOptions({ name: 'ConfirmDialog' })

// A question with a safe way out. The cancel button has the focus when the
// dialog opens. The confirm button is danger by default, since most questions
// here delete something.
withDefaults(
  defineProps<{
    title: string
    message: string
    confirmLabel: string
    variation?: 'danger' | 'primary' | 'warning'
    icon?: string
  }>(),
  { variation: 'danger', icon: 'delete-bin' }
)
const emit = defineEmits<{ confirm: []; cancel: [] }>()

const { $gettext } = useGettext()
</script>
