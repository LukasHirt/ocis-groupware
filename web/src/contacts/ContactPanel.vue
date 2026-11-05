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
    <contact-detail v-else-if="contact" :contact="contact">
      <template #groups>
        <contact-groups
          :contact="contact"
          :can-edit="canEdit"
          :suggestions="suggestions"
          @change="(categories) => emit('setCategories', categories)"
        />
      </template>
      <template #photo>
        <contact-photo
          :contact="contact"
          :name="contactName(contact) || $gettext('Unnamed contact')"
          :can-edit="canEdit"
          @changed="emit('photoChanged')"
        />
      </template>
      <div v-if="canEdit" class="oc-mt-m" data-testid="contact-actions">
        <oc-button
          class="oc-mr-s"
          size="small"
          appearance="outline"
          data-testid="contact-edit"
          @click="emit('edit')"
        >
          {{ $gettext('Edit') }}
        </oc-button>
        <oc-button
          v-if="!confirmingDelete"
          size="small"
          variation="danger"
          appearance="outline"
          data-testid="contact-delete"
          @click="confirmingDelete = true"
        >
          {{ $gettext('Delete') }}
        </oc-button>
        <template v-else>
          <span class="oc-mr-s">{{ $gettext('Delete this contact?') }}</span>
          <oc-button
            class="oc-mr-s"
            size="small"
            variation="danger"
            appearance="filled"
            data-testid="contact-delete-confirm"
            @click="emit('delete')"
          >
            {{ $gettext('Delete') }}
          </oc-button>
          <oc-button size="small" appearance="outline" @click="confirmingDelete = false">
            {{ $gettext('Cancel') }}
          </oc-button>
        </template>
      </div>
    </contact-detail>
    <no-content-message v-else icon="contacts-book" data-testid="no-selection">
      <template #message>
        <span>{{ $gettext('Select a contact to see its details') }}</span>
      </template>
    </no-content-message>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import { NoContentMessage } from '@ownclouders/web-pkg'
import type { ContactInput, GraphContact, GraphContactFolder } from '../api/types'
import ContactDetail from './ContactDetail.vue'
import ContactForm from './ContactForm.vue'
import ContactGroups from './ContactGroups.vue'
import ContactPhoto from './ContactPhoto.vue'
import { contactName } from './model'

defineOptions({ name: 'ContactPanel' })

// The body of the details panel of the sidebar: the contact that is chosen, a
// form to change it or to write a new one, or a note to choose a contact.
// It holds no data of its own; the page passes the state in and gets the
// changes back as events.
const props = defineProps<{
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

// Another contact, or leaving the form, ends a question about deleting.
const confirmingDelete = ref(false)
watch(
  () => [props.contact?.id, props.editing],
  () => {
    confirmingDelete.value = false
  }
)
</script>
