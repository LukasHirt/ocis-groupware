<template>
  <modal-dialog :title="$gettext('Edit repeating event')" @close="emit('close')">
    <p class="oc-mt-rm">{{ $gettext('Do you want to change only this event or all events of the series?') }}</p>
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
        type="button"
        class="oc-button oc-button-m oc-button-primary"
        data-testid="scope-this"
        @click="emit('choose', 'this')"
      >
        {{ $gettext('This event') }}
      </button>
      <button
        type="button"
        class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
        data-testid="scope-all"
        @click="emit('choose', 'all')"
      >
        {{ $gettext('All events') }}
      </button>
    </div>
  </modal-dialog>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'

defineOptions({ name: 'ScopeDialog' })

const emit = defineEmits<{ close: []; choose: [scope: 'this' | 'all'] }>()
const { $gettext } = useGettext()
</script>
