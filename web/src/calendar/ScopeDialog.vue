<template>
  <gw-modal
    :title="remove ? $gettext('Delete repeating event') : $gettext('Edit repeating event')"
    initial-focus="[data-testid=scope-this]"
    @close="emit('close')"
  >
    <p class="oc-mt-rm">
      {{
        remove
          ? $gettext('Do you want to delete only this event or all events of the series?')
          : $gettext('Do you want to change only this event or all events of the series?')
      }}
    </p>
    <template #actions>
      <oc-button variation="passive" appearance="outline" data-testid="dialog-cancel" @click="emit('close')">
        {{ $gettext('Cancel') }}
      </oc-button>
      <oc-button variation="passive" appearance="outline" data-testid="scope-this" @click="emit('choose', 'this')">
        {{ $gettext('This event') }}
      </oc-button>
      <oc-button variation="primary" appearance="filled" data-testid="scope-all" @click="emit('choose', 'all')">
        {{ $gettext('All events') }}
      </oc-button>
    </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'

defineOptions({ name: 'ScopeDialog' })

// The question is the same for changing and for deleting; only the words differ.
defineProps<{ remove?: boolean }>()
const emit = defineEmits<{ close: []; choose: [scope: 'this' | 'all'] }>()
const { $gettext } = useGettext()
</script>
