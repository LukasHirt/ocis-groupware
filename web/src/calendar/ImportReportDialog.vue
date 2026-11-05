<template>
  <modal-dialog :title="$gettext('Import into %{name}', { name })" @close="emit('close')">
    <p class="oc-mt-rm" data-testid="import-summary">
      {{
        $gettext('%{total} found: %{created} added, %{updated} replaced, %{failed} failed.', {
          total: String(report.total),
          created: String(report.created),
          updated: String(report.updated),
          failed: String(report.failed)
        })
      }}
    </p>
    <template v-if="failures.length > 0">
      <p class="oc-text-bold">{{ $gettext('These could not be stored:') }}</p>
      <ul class="oc-list groupware-failures">
        <li v-for="f in failures" :key="f.index" data-testid="import-failure">
          <strong>{{ $gettext('Number %{n}', { n: String(f.index + 1) }) }}</strong>
          <span v-if="f.uid">, {{ f.uid }}</span>
          <br />
          <span class="oc-text-muted">{{ f.error }}</span>
        </li>
      </ul>
    </template>
    <div class="groupware-modal-actions">
      <button
        type="button"
        class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
        data-testid="dialog-confirm"
        @click="emit('close')"
      >
        {{ $gettext('Close') }}
      </button>
    </div>
  </modal-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'
import type { ImportReport } from '../api/types'

defineOptions({ name: 'ImportReportDialog' })

const props = defineProps<{ name: string; report: ImportReport }>()
const emit = defineEmits<{ close: [] }>()

const { $gettext } = useGettext()
const failures = computed(() => props.report.results.filter((r) => r.status === 'failed'))
</script>

<style>
.groupware-failures {
  max-height: 14rem;
  overflow: auto;
}
.groupware-failures li {
  padding: 0.25rem 0;
}
</style>
