<template>
  <modal-dialog :title="title" @close="emit('cancel')">
    <p class="oc-mt-rm">{{ message }}</p>
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
        type="button"
        class="oc-button oc-button-m oc-button-danger oc-button-danger-filled"
        data-testid="dialog-confirm"
        @click="emit('confirm')"
      >
        {{ confirmLabel }}
      </button>
    </div>
  </modal-dialog>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import ModalDialog from './ModalDialog.vue'

defineOptions({ name: 'ConfirmDialog' })

defineProps<{ title: string; message: string; confirmLabel: string }>()
const emit = defineEmits<{ confirm: []; cancel: [] }>()

const { $gettext } = useGettext()
</script>
