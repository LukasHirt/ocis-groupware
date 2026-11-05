<template>
  <gw-modal :title="$gettext('Import into %{name}', { name })" wide @close="emit('close')">
    <p class="oc-mt-rm oc-flex oc-flex-middle gw-import-summary" data-testid="import-summary">
      <oc-icon
        :name="failures.length === 0 ? 'checkbox-circle' : 'error-warning'"
        fill-type="line"
        :variation="failures.length === 0 ? 'success' : 'warning'"
        class="oc-mr-xs"
      />
      <span>{{
        $gettext('%{total} found: %{created} added, %{updated} replaced, %{failed} failed.', {
          total: String(report.total),
          created: String(report.created),
          updated: String(report.updated),
          failed: String(report.failed)
        })
      }}</span>
    </p>
    <template v-if="failures.length > 0">
      <h3 class="gw-import-heading">{{ $gettext('These could not be stored:') }}</h3>
      <div class="gw-import-failures">
        <oc-table-simple :hover="false">
          <oc-thead>
            <oc-tr>
              <oc-th>{{ $gettext('Item') }}</oc-th>
              <oc-th>{{ $gettext('Error') }}</oc-th>
            </oc-tr>
          </oc-thead>
          <oc-tbody>
            <oc-tr v-for="f in failures" :key="f.index" data-testid="import-failure">
              <oc-td>
                {{ $gettext('Number %{n}', { n: String(f.index + 1) }) }}
                <span v-if="f.uid" class="oc-text-muted">{{ f.uid }}</span>
              </oc-td>
              <oc-td>{{ f.error }}</oc-td>
            </oc-tr>
          </oc-tbody>
        </oc-table-simple>
      </div>
    </template>
    <template #actions>
      <oc-button variation="primary" appearance="filled" data-testid="dialog-confirm" @click="emit('close')">
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

defineOptions({ name: 'ImportReportDialog' })

const props = defineProps<{ name: string; report: ImportReport }>()
const emit = defineEmits<{ close: [] }>()

const { $gettext } = useGettext()
const failures = computed(() => props.report.results.filter((r) => r.status === 'failed'))
</script>

<style>
.gw-import-heading {
  margin: var(--oc-space-medium) 0 var(--oc-space-small);
  font-size: var(--oc-font-size-default);
  font-weight: var(--oc-font-weight-semibold);
}
.gw-import-failures {
  max-height: 14rem;
  overflow: auto;
}
</style>
