<template>
  <div class="contact-panel">
    <contact-form
      v-if="editing === 'new'"
      key="new"
      :folders="targetFolders"
      @save="(input, target) => emit('saveNew', input, target)"
      @cancel="emit('cancel')"
    />
    <contact-form
      v-else-if="editing === 'edit' && contact"
      :key="contact.id"
      :contact="contact"
      :folders="folders"
      @save="(input) => emit('saveEdit', input)"
      @cancel="emit('cancel')"
    />
    <contact-detail
      v-else-if="contact"
      :contact="contact"
      :can-edit="canEdit"
      :suggestions="suggestions"
      @edit="emit('edit')"
      @delete="emit('delete')"
      @photo-changed="emit('photoChanged')"
      @set-categories="(categories) => emit('setCategories', categories)"
    />
    <no-content-message v-else icon="contacts-book" data-testid="no-selection">
      <template #message>
        <span>{{ $gettext('Select a contact to see its details') }}</span>
      </template>
    </no-content-message>
  </div>
</template>

<script setup lang="ts">
import { NoContentMessage } from '@ownclouders/web-pkg'
import { useGettext } from 'vue3-gettext'
import type { ContactInput, GraphContact, GraphContactFolder } from '../api/types'
import ContactDetail from './ContactDetail.vue'
import ContactForm from './ContactForm.vue'

defineOptions({ name: 'ContactPanel' })

// The body of the details panel of the sidebar: the contact that is chosen, a
// form to change it or to write a new one, or a note to choose a contact.
// It holds no data of its own; the page passes the state in and gets the
// changes back as events.
defineProps<{
  contact: GraphContact | null
  editing: 'new' | 'edit' | null
  canEdit: boolean
  // Every address book, and those a new contact may go into.
  folders: GraphContactFolder[]
  targetFolders: GraphContactFolder[]
  // The names of the groups that exist.
  suggestions: string[]
}>()
const emit = defineEmits<{
  edit: []
  cancel: []
  delete: []
  photoChanged: []
  saveNew: [input: ContactInput, folderId: string]
  saveEdit: [input: ContactInput]
  setCategories: [categories: string[]]
}>()

const { $gettext } = useGettext()
</script>
