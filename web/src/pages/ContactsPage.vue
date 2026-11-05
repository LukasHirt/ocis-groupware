<template>
  <page-layout
    :title="$gettext('Contacts')"
    :loading="loadingFolders"
    :empty="folders.length === 0"
    :empty-text="$gettext('You have no address books yet.')"
  >
    <div class="contacts-layout oc-mt-m">
      <nav :aria-label="$gettext('Address books')">
        <ul class="oc-list">
          <li>
            <button
              class="oc-button oc-button-m oc-button-passive oc-button-justify-content-left oc-width-1-1"
              :class="{ selected: folderId === null }"
              :aria-current="folderId === null"
              data-testid="folder-all"
              @click="selectFolder(null)"
            >
              {{ $gettext('All contacts') }}
            </button>
          </li>
          <li v-for="folder in folders" :key="folder.id">
            <button
              class="oc-button oc-button-m oc-button-passive oc-button-justify-content-left oc-width-1-1"
              :class="{ selected: folderId === folder.id }"
              :aria-current="folderId === folder.id"
              data-testid="folder-item"
              @click="selectFolder(folder.id)"
            >
              <span class="name">{{ folder.displayName }}</span>
              <span class="oc-text-muted oc-ml-s">
                {{ folder['groupware.owner'].name
                }}<template v-if="!folder['groupware.canEdit']"
                  >, {{ $gettext('read only') }}</template
                >
              </span>
            </button>
          </li>
        </ul>
      </nav>

      <section :aria-label="$gettext('Contact list')">
        <div class="oc-flex oc-flex-middle contact-toolbar">
          <input
            v-model="query"
            type="search"
            class="oc-text-input oc-mr-s"
            :aria-label="$gettext('Search contacts')"
            :placeholder="$gettext('Search')"
            data-testid="contact-search"
          />
          <select
            v-model="sortKey"
            class="oc-select"
            :aria-label="$gettext('Sort by')"
            data-testid="contact-sort"
          >
            <option value="name">{{ $gettext('Name') }}</option>
            <option value="surname">{{ $gettext('Surname') }}</option>
            <option value="company">{{ $gettext('Company') }}</option>
          </select>
          <button
            class="oc-button oc-button-m oc-button-primary oc-ml-s"
            :disabled="targetFolders.length === 0"
            data-testid="contact-new"
            @click="startNew"
          >
            {{ $gettext('New contact') }}
          </button>
        </div>
        <div v-if="loadingContacts" class="oc-mt-m" role="status">
          <oc-spinner :aria-label="$gettext('Loading')" />
        </div>
        <p v-else-if="contacts.length === 0" class="oc-text-muted oc-mt-m" data-testid="no-contacts">
          {{ $gettext('There are no contacts here yet.') }}
        </p>
        <p v-else-if="shown.length === 0" class="oc-text-muted oc-mt-m" data-testid="no-match">
          {{ $gettext('No contact matches your search.') }}
        </p>
        <ul v-else class="oc-list oc-mt-s">
          <li v-for="contact in shown" :key="contact.id">
            <button
              class="oc-button oc-button-m oc-button-passive oc-button-justify-content-left oc-width-1-1"
              :class="{ selected: contact.id === selectedId }"
              :aria-current="contact.id === selectedId"
              data-testid="contact-item"
              @click="selectedId = contact.id"
            >
              <span class="name">{{ contactName(contact) || $gettext('Unnamed contact') }}</span>
              <span v-if="contact.companyName" class="oc-text-muted oc-ml-s">{{
                contact.companyName
              }}</span>
            </button>
          </li>
        </ul>
      </section>

      <section :aria-label="$gettext('Contact details')">
        <contact-form
          v-if="editing === 'new'"
          key="new"
          :folders="targetFolders"
          @save="saveNew"
          @cancel="editing = null"
        />
        <contact-form
          v-else-if="editing === 'edit' && selected"
          :key="selected.id"
          :contact="selected"
          :folders="folders"
          @save="saveEdit"
          @cancel="editing = null"
        />
        <contact-detail v-else-if="selected" :contact="selected">
          <template #photo>
            <contact-photo
              :contact="selected"
              :name="contactName(selected) || $gettext('Unnamed contact')"
              :can-edit="canEdit(selected)"
              @changed="reload(selected)"
            />
          </template>
          <div v-if="canEdit(selected)" class="oc-mt-m" data-testid="contact-actions">
            <button
              class="oc-button oc-button-m oc-button-passive oc-mr-s"
              data-testid="contact-edit"
              @click="editing = 'edit'"
            >
              {{ $gettext('Edit') }}
            </button>
            <template v-if="!confirmingDelete">
              <button
                class="oc-button oc-button-m oc-button-passive"
                data-testid="contact-delete"
                @click="confirmingDelete = true"
              >
                {{ $gettext('Delete') }}
              </button>
            </template>
            <template v-else>
              <span class="oc-mr-s">{{ $gettext('Delete this contact?') }}</span>
              <button
                class="oc-button oc-button-m oc-button-danger oc-mr-s"
                data-testid="contact-delete-confirm"
                @click="remove(selected)"
              >
                {{ $gettext('Delete') }}
              </button>
              <button
                class="oc-button oc-button-m oc-button-passive"
                @click="confirmingDelete = false"
              >
                {{ $gettext('Cancel') }}
              </button>
            </template>
          </div>
        </contact-detail>
      </section>
    </div>
  </page-layout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import PageLayout from '../components/PageLayout.vue'
import ContactDetail from '../contacts/ContactDetail.vue'
import ContactForm from '../contacts/ContactForm.vue'
import ContactPhoto from '../contacts/ContactPhoto.vue'
import { contactName, filterContacts, sortContacts, type SortKey } from '../contacts/model'
import { useApi } from '../composables/useApi'
import type { ContactInput, GraphContact, GraphContactFolder } from '../api/types'

defineOptions({ name: 'ContactsPage' })

const { $gettext } = useGettext()
const { run } = useApi()

const folders = ref<GraphContactFolder[]>([])
const loadingFolders = ref(true)
// null shows the contacts of every address book.
const folderId = ref<string | null>(null)
const contacts = ref<GraphContact[]>([])
const loadingContacts = ref(false)
const query = ref('')
const sortKey = ref<SortKey>('name')
const selectedId = ref<string | null>(null)

const shown = computed(() => sortContacts(filterContacts(contacts.value, query.value), sortKey.value))
const selected = computed(() => contacts.value.find((c) => c.id === selectedId.value))
const editing = ref<'new' | 'edit' | null>(null)
const confirmingDelete = ref(false)

// The address books a new contact may go into: the chosen one, or every one
// the user may write to.
const targetFolders = computed(() =>
  (folderId.value === null ? folders.value : folders.value.filter((f) => f.id === folderId.value)).filter(
    (f) => f['groupware.canEdit']
  )
)

const canEdit = (c: GraphContact) =>
  !!folders.value.find((f) => f.id === c.parentFolderId)?.['groupware.canEdit']

// Another contact, or another address book, ends an edit in progress.
watch([selectedId, folderId], () => {
  editing.value = null
  confirmingDelete.value = false
})

// Only the newest load may write its result, so a slow answer for an
// address book that is no longer shown cannot replace the current list.
let loadCounter = 0

async function loadContacts() {
  const mine = ++loadCounter
  loadingContacts.value = true
  const ids = folderId.value === null ? folders.value.map((f) => f.id) : [folderId.value]
  const lists = await Promise.all(
    ids.map((id) =>
      run($gettext('Could not load the contacts'), (c) => c.listContacts(id))
    )
  )
  if (mine !== loadCounter) return
  contacts.value = lists.flatMap((l) => l ?? [])
  if (!contacts.value.some((c) => c.id === selectedId.value)) selectedId.value = null
  loadingContacts.value = false
}

function selectFolder(id: string | null) {
  folderId.value = id
  return loadContacts()
}

function startNew() {
  selectedId.value = null
  editing.value = 'new'
}

function replaceContact(updated: GraphContact) {
  contacts.value = contacts.value.map((c) => (c.id === updated.id ? updated : c))
}

// reload reads one contact again, for a change that was made by another
// call than the form, such as a new photo.
async function reload(contact: GraphContact) {
  const fresh = await run($gettext('Could not load the contact'), (c) =>
    c.getContact(contact.parentFolderId ?? '', contact.id)
  )
  if (fresh) replaceContact(fresh)
}

async function saveNew(input: ContactInput, targetId: string) {
  const created = await run($gettext('Could not create the contact'), (c) =>
    c.createContact(targetId, input)
  )
  if (!created) return
  if (folderId.value === null || folderId.value === targetId) {
    contacts.value = [...contacts.value, created]
  }
  selectedId.value = created.id
  editing.value = null
}

async function saveEdit(input: ContactInput) {
  const current = selected.value
  if (!current) return
  const updated = await run($gettext('Could not save the contact'), (c) =>
    c.updateContact(current.parentFolderId ?? '', current.id, input, current['@odata.etag'])
  )
  if (!updated) return
  replaceContact(updated)
  editing.value = null
}

async function remove(contact: GraphContact) {
  const done = await run($gettext('Could not delete the contact'), async (c) => {
    await c.deleteContact(contact.parentFolderId ?? '', contact.id, contact['@odata.etag'])
    return true
  })
  if (!done) return
  contacts.value = contacts.value.filter((c) => c.id !== contact.id)
  selectedId.value = null
  confirmingDelete.value = false
}

onMounted(async () => {
  folders.value =
    (await run($gettext('Could not load the address books'), (c) => c.listContactFolders())) ?? []
  loadingFolders.value = false
  await loadContacts()
})
</script>

<style scoped>
.contacts-layout {
  display: grid;
  grid-template-columns: minmax(10rem, 1fr) minmax(14rem, 2fr) minmax(16rem, 3fr);
  gap: var(--oc-space-large);
  align-items: start;
}
.contact-toolbar input {
  flex: 1;
}
.selected {
  background-color: var(--oc-color-background-highlight);
}
@media (max-width: 900px) {
  .contacts-layout {
    grid-template-columns: 1fr;
  }
}
</style>
