<template>
  <page-layout
    :title="$gettext('Contacts')"
    :loading="loading"
    :empty="folders.length === 0"
    :empty-text="$gettext('You have no address books yet.')"
  >
    <ul class="oc-list oc-mt-m">
      <li
        v-for="folder in folders"
        :key="folder.id"
        class="oc-flex oc-flex-middle oc-py-xs"
        data-testid="folder-item"
      >
        <span class="name">{{ folder.displayName }}</span>
        <span class="oc-text-muted oc-ml-s">
          {{ folder['groupware.owner'].name
          }}<template v-if="!folder['groupware.canEdit']">, {{ $gettext('read only') }}</template>
        </span>
      </li>
    </ul>
  </page-layout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import PageLayout from '../components/PageLayout.vue'
import { useApi } from '../composables/useApi'
import type { GraphContactFolder } from '../api/types'

defineOptions({ name: 'ContactsPage' })

const { $gettext } = useGettext()
const { run } = useApi()
const folders = ref<GraphContactFolder[]>([])
const loading = ref(true)

onMounted(async () => {
  folders.value =
    (await run($gettext('Could not load the address books'), (c) => c.listContactFolders())) ?? []
  loading.value = false
})
</script>
