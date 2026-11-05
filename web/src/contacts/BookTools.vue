<template>
  <div class="book-tools oc-mt-s">
    <form v-if="mode === 'new'" class="oc-flex oc-flex-middle" @submit.prevent="saveNew">
      <input
        v-model="text"
        class="oc-text-input oc-mr-xs"
        :aria-label="$gettext('Name of the address book')"
        data-testid="book-new-input"
        @keydown.enter.prevent="saveNew"
      />
      <button
        type="button"
        class="oc-button oc-button-s oc-button-primary oc-mr-xs"
        :disabled="!text.trim()"
        data-testid="book-new-save"
        @click="saveNew"
      >
        {{ $gettext('Create') }}
      </button>
      <button type="button" class="oc-button oc-button-s oc-button-passive" @click="mode = null">
        {{ $gettext('Cancel') }}
      </button>
    </form>

    <div v-else-if="mode === 'rename'" class="oc-flex oc-flex-middle">
      <input
        v-model="text"
        class="oc-text-input oc-mr-xs"
        :aria-label="$gettext('New name of the address book')"
        data-testid="book-rename-input"
        @keydown.enter.prevent="saveRename"
      />
      <button
        type="button"
        class="oc-button oc-button-s oc-button-primary oc-mr-xs"
        :disabled="!text.trim()"
        data-testid="book-rename-save"
        @click="saveRename"
      >
        {{ $gettext('Save') }}
      </button>
      <button type="button" class="oc-button oc-button-s oc-button-passive" @click="mode = null">
        {{ $gettext('Cancel') }}
      </button>
    </div>

    <div v-else-if="mode === 'delete' && folder">
      <p class="oc-mb-xs">
        {{
          $gettext('Delete the address book %{name} with all its contacts?', {
            name: folder.displayName
          })
        }}
      </p>
      <button
        type="button"
        class="oc-button oc-button-s oc-button-danger oc-mr-xs"
        data-testid="book-delete-confirm"
        @click="confirmDelete"
      >
        {{ $gettext('Delete') }}
      </button>
      <button type="button" class="oc-button oc-button-s oc-button-passive" @click="mode = null">
        {{ $gettext('Cancel') }}
      </button>
    </div>

    <div v-else>
      <button
        type="button"
        class="oc-button oc-button-s oc-button-passive oc-mr-xs"
        data-testid="book-new"
        @click="start('new', '')"
      >
        {{ $gettext('New address book') }}
      </button>
      <template v-if="folder">
        <button
          type="button"
          class="oc-button oc-button-s oc-button-passive oc-mr-xs"
          data-testid="book-download"
          @click="emit('download')"
        >
          {{ $gettext('Download vCard') }}
        </button>
        <label
          v-if="folder['groupware.canEdit']"
          class="oc-button oc-button-s oc-button-passive oc-mr-xs book-import"
        >
          {{ $gettext('Import vCard') }}
          <input
            type="file"
            accept=".vcf,text/vcard,text/x-vcard"
            class="oc-invisible-sr"
            data-testid="book-import-input"
            @change="onImport"
          />
        </label>
      </template>
      <template v-if="folder && folder['groupware.canShare']">
        <button
          type="button"
          class="oc-button oc-button-s oc-button-passive oc-mr-xs"
          data-testid="book-rename"
          @click="start('rename', folder.displayName)"
        >
          {{ $gettext('Rename') }}
        </button>
        <button
          v-if="!folder['groupware.isDefault']"
          type="button"
          class="oc-button oc-button-s oc-button-passive"
          data-testid="book-delete"
          @click="mode = 'delete'"
        >
          {{ $gettext('Delete') }}
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContactFolder } from '../api/types'

defineOptions({ name: 'BookTools' })

// folder is the chosen address book, or none when all of them are shown.
// Only the owner may rename or delete one, and the default one stays.
const props = defineProps<{ folder: GraphContactFolder | null }>()
const emit = defineEmits<{
  create: [name: string]
  rename: [name: string]
  remove: []
  download: []
  import: [file: File]
}>()
const { $gettext } = useGettext()

const mode = ref<'new' | 'rename' | 'delete' | null>(null)
const text = ref('')

function start(next: 'new' | 'rename', value: string) {
  text.value = value
  mode.value = next
}

// Choosing another address book ends what was started for the last one.
watch(
  () => props.folder?.id,
  () => {
    mode.value = null
  }
)

function saveNew() {
  const name = text.value.trim()
  if (!name) return
  mode.value = null
  emit('create', name)
}

function saveRename() {
  const name = text.value.trim()
  if (!name) return
  mode.value = null
  emit('rename', name)
}

function onImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) emit('import', file)
  // The same file can be chosen again.
  input.value = ''
}

function confirmDelete() {
  mode.value = null
  emit('remove')
}
</script>

<style scoped>
.book-import {
  cursor: pointer;
}
</style>
