<template>
  <gw-modal
    :title="$gettext('Import finished')"
    wide
    initial-focus="[data-testid=import-report-close]"
    @close="emit('close')"
  >
    <div data-testid="import-report">
      <p class="oc-mt-rm" role="status" data-testid="import-summary">
        {{
          $gettext('Import finished: %{created} created, %{updated} updated, %{failed} failed.', {
            created: String(report.created),
            updated: String(report.updated),
            failed: String(report.failed)
          })
        }}
      </p>
      <template v-if="failures.length > 0">
        <p class="oc-text-bold">{{ $gettext('These cards could not be stored:') }}</p>
        <div class="gw-import-failures">
          <oc-table-simple>
            <oc-thead>
              <oc-tr>
                <oc-th>{{ $gettext('Card') }}</oc-th>
                <oc-th>{{ $gettext('Reason') }}</oc-th>
              </oc-tr>
            </oc-thead>
            <oc-tbody>
              <oc-tr v-for="f in failures" :key="f.index" data-testid="import-failure">
                <oc-td>{{ f.index + 1 }}</oc-td>
                <oc-td>{{ f.error }}</oc-td>
              </oc-tr>
            </oc-tbody>
          </oc-table-simple>
        </div>
      </template>
    </div>
    <template #actions>
      <oc-button
        variation="primary"
        appearance="filled"
        data-testid="import-report-close"
        @click="emit('close')"
      >
        {{ $gettext('Close') }}
      </oc-button>
    </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'
import type { ImportReport } from '../api/types'

defineOptions({ name: 'ContactImportReport' })

// What an import of a vCard file did, and the cards it could not store.
const props = defineProps<{ report: ImportReport }>()
const emit = defineEmits<{ close: [] }>()

const { $gettext } = useGettext()
const failures = computed(() => props.report.results.filter((r) => r.status === 'failed'))
</script>

<style>
.gw-import-failures {
  max-height: 14rem;
  overflow: auto;
}
</style>
