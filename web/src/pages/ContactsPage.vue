<template>
  <app-shell
    :title="title"
    :loading="loadingFolders"
    :list-toggle-label="$gettext('Show address books')"
    :side-bar-panels="sideBarPanels"
  >
    <template #list>
      <contacts-nav
        :folders="folders"
        :folder-id="folderId"
        :counts="counts"
        :total="contacts.length"
        :groups="groups"
        :group-cards="groupCards"
        :group="group"
        @select-folder="selectFolder"
        @select-group="group = $event"
        @new-book="dialog = { kind: 'newBook' }"
      />
    </template>

    <template #viewControls>
      <oc-text-input
        id="gw-contact-search"
        v-model="query"
        type="search"
        class="gw-search"
        :label="$gettext('Search contacts')"
        :default-value="$gettext('Search')"
        clear-button-enabled
        data-testid="contact-search"
      />
      <oc-select
        id="gw-contact-sort"
        :model-value="sortKey"
        class="gw-sort oc-ml-s"
        :label="$gettext('Sort by')"
        label-hidden
        :options="sortOptions"
        :reduce="(o: SortOption) => o.value"
        :clearable="false"
        :searchable="false"
        data-testid="contact-sort"
        @update:model-value="chooseSort"
      />
    </template>

    <template #actions>
      <oc-button
        variation="primary"
        appearance="filled"
        class="oc-mr-s"
        :disabled="targetFolders.length === 0"
        data-testid="contact-new"
        @click="startNew"
      >
        <oc-icon name="add" fill-type="line" />
        {{ $gettext('New contact') }}
      </oc-button>

      <template v-if="group?.kind === 'category'">
        <oc-button appearance="raw" class="oc-mr-s" data-testid="group-rename" @click="startRename">
          <oc-icon name="pencil" fill-type="line" />
          {{ $gettext('Rename group') }}
        </oc-button>
        <oc-button
          appearance="raw"
          variation="danger"
          data-testid="group-delete"
          @click="dialog = { kind: 'deleteGroup' }"
        >
          <oc-icon name="delete-bin" fill-type="line" />
          {{ $gettext('Delete group') }}
        </oc-button>
      </template>
      <template v-else>
        <oc-button
          v-if="importTarget"
          appearance="raw"
          class="oc-mr-s"
          data-testid="book-import"
          @click="importInput?.click()"
        >
          <oc-icon name="upload" fill-type="line" />
          {{ $gettext('Import vCard') }}
        </oc-button>
        <input
          v-if="importTarget"
          ref="importInput"
          type="file"
          accept=".vcf,text/vcard,text/x-vcard"
          class="oc-invisible-sr"
          tabindex="-1"
          aria-hidden="true"
          data-testid="book-import-input"
          @change="onImport"
        />
        <oc-button
          v-if="currentFolder"
          appearance="raw"
          class="oc-mr-s"
          data-testid="book-download"
          @click="downloadBook"
        >
          <oc-icon name="download" fill-type="line" />
          {{ $gettext('Download vCard') }}
        </oc-button>
        <oc-button
          v-if="currentFolder?.['groupware.canShare']"
          appearance="raw"
          class="oc-mr-s"
          data-testid="book-share"
          @click="sharing = true"
        >
          <oc-icon name="share-forward" fill-type="line" />
          {{ $gettext('Share') }}
        </oc-button>
        <template v-if="currentFolder?.['groupware.canShare']">
          <oc-button
            id="gw-book-actions"
            appearance="raw"
            :aria-label="$gettext('More actions for this address book')"
            data-testid="book-actions"
          >
            <oc-icon name="more-2" fill-type="line" />
          </oc-button>
          <oc-drop
            drop-id="gw-book-actions-drop"
            toggle="#gw-book-actions"
            mode="click"
            close-on-click
            padding-size="small"
            class="gw-book-menu"
          >
            <ul class="oc-list oc-files-context-actions">
              <li>
                <oc-button
                  appearance="raw"
                  justify-content="left"
                  data-testid="book-rename"
                  @click="dialog = { kind: 'renameBook' }"
                >
                  <oc-icon name="pencil" fill-type="line" />
                  {{ $gettext('Rename') }}
                </oc-button>
              </li>
              <li v-if="!currentFolder['groupware.isDefault']">
                <oc-button
                  appearance="raw"
                  justify-content="left"
                  variation="danger"
                  data-testid="book-delete"
                  @click="dialog = { kind: 'deleteBook' }"
                >
                  <oc-icon name="delete-bin" fill-type="line" />
                  {{ $gettext('Delete') }}
                </oc-button>
              </li>
            </ul>
          </oc-drop>
        </template>
      </template>
    </template>

    <no-content-message
      v-if="folders.length === 0"
      icon="contacts-book"
      class="oc-mt-l"
      data-testid="empty"
    >
      <template #message>
        <span>{{ $gettext('You have no address books yet.') }}</span>
      </template>
      <template #callToAction>
        <oc-button variation="primary" appearance="filled" @click="dialog = { kind: 'newBook' }">
          {{ $gettext('New address book') }}
        </oc-button>
      </template>
    </no-content-message>
    <div v-else-if="loadingContacts" class="oc-flex oc-flex-center oc-mt-l" role="status">
      <oc-spinner size="large" :aria-label="$gettext('Loading')" />
    </div>
    <no-content-message
      v-else-if="inView.length === 0"
      icon="contacts-book"
      class="oc-mt-l"
      data-testid="no-contacts"
    >
      <template #message>
        <span>{{ $gettext('There are no contacts here yet.') }}</span>
      </template>
      <template v-if="targetFolders.length > 0" #callToAction>
        <oc-button variation="primary" appearance="filled" @click="startNew">
          {{ $gettext('New contact') }}
        </oc-button>
      </template>
    </no-content-message>
    <no-content-message
      v-else-if="shown.length === 0"
      icon="search"
      class="oc-mt-l"
      data-testid="no-match"
    >
      <template #message>
        <span>{{ $gettext('No contact matches your search.') }}</span>
      </template>
    </no-content-message>
    <template v-else>
      <contact-table
        :contacts="shown"
        :selected-id="selectedId"
        :sort-key="sortKey"
        :sort-desc="sortDesc"
        @select="select"
        @sort="onSort"
      />
      <p class="oc-text-muted oc-text-small oc-mt-s oc-mb-rm" data-testid="contact-count">
        {{
          shown.length === 1
            ? $gettext('One contact')
            : $gettext('%{count} contacts', { count: String(shown.length) })
        }}
      </p>
    </template>

    <name-dialog
      v-if="dialog?.kind === 'newBook'"
      :title="$gettext('New address book')"
      :label="$gettext('Name of the address book')"
      :confirm-label="$gettext('Create')"
      testid="book-new"
      @close="dialog = null"
      @save="createBook"
    />
    <name-dialog
      v-else-if="dialog?.kind === 'renameBook' && currentFolder"
      :title="$gettext('Rename address book')"
      :label="$gettext('New name of the address book')"
      :confirm-label="$gettext('Save')"
      :initial="currentFolder.displayName"
      testid="book-rename"
      @close="dialog = null"
      @save="renameBook"
    />
    <name-dialog
      v-else-if="dialog?.kind === 'renameGroup'"
      :title="$gettext('Rename group')"
      :label="$gettext('New name of the group')"
      :confirm-label="$gettext('Save')"
      :initial="dialog.from"
      testid="group-rename"
      @close="dialog = null"
      @save="renameGroupTo"
    />
    <confirm-dialog
      v-else-if="dialog?.kind === 'deleteBook' && currentFolder"
      :title="$gettext('Delete address book')"
      :message="
        $gettext('Delete the address book %{name} with all its contacts?', {
          name: currentFolder.displayName
        })
      "
      :confirm-label="$gettext('Delete')"
      @cancel="dialog = null"
      @confirm="deleteBook"
    />
    <confirm-dialog
      v-else-if="dialog?.kind === 'deleteGroup'"
      :title="$gettext('Delete group')"
      :message="$gettext('Remove this group from its contacts?')"
      :confirm-label="$gettext('Delete group')"
      @cancel="dialog = null"
      @confirm="deleteGroup"
    />
    <contact-import-report
      v-if="importReport"
      :report="importReport"
      @close="importReport = null"
    />
    <share-dialog
      v-if="sharing && currentFolder"
      kind="addressBook"
      :collection-id="currentFolder.id"
      :name="currentFolder.displayName"
      @close="sharing = false"
    />
  </app-shell>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type Component } from 'vue'
import { useGettext } from 'vue3-gettext'
import { NoContentMessage, SideBarEventTopics, eventBus, useSideBar } from '@ownclouders/web-pkg'
import type { SideBarPanel } from '@ownclouders/web-pkg'
import type { Item } from '@ownclouders/web-client'
import AppShell from '../components/AppShell.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { fileNameFor, saveBlob } from '../components/download'
import ShareDialog from '../sharing/ShareDialog.vue'
import ContactImportReport from '../contacts/ContactImportReport.vue'
import ContactPanel from '../contacts/ContactPanel.vue'
import ContactTable from '../contacts/ContactTable.vue'
import ContactsNav from '../contacts/ContactsNav.vue'
import NameDialog from '../contacts/NameDialog.vue'
import {
  inGroup,
  listGroupCards,
  listGroups,
  removeFromGroup,
  renameGroup,
  type GroupFilter
} from '../contacts/groups'
import { filterContacts, sortContacts, type SortKey } from '../contacts/model'
import { useApi } from '../composables/useApi'
import type { ContactInput, ImportReport, GraphContact, GraphContactFolder } from '../api/types'

defineOptions({ name: 'ContactsPage' })

const { $gettext } = useGettext()
const { run } = useApi()
const { isSideBarOpen } = useSideBar()

const folders = ref<GraphContactFolder[]>([])
const loadingFolders = ref(true)
// null shows the contacts of every address book.
const folderId = ref<string | null>(null)
const sharing = ref(false)
// The contacts of every address book. Choosing an address book or a group
// filters them, so the counts of the list pane stay right.
const contacts = ref<GraphContact[]>([])
const loadingContacts = ref(false)
const query = ref('')
const sortKey = ref<SortKey>('name')
const sortDesc = ref(false)
const selectedId = ref<string | null>(null)

interface SortOption {
  label: string
  value: SortKey
}
const sortOptions = computed<SortOption[]>(() => [
  { label: $gettext('Name'), value: 'name' },
  { label: $gettext('Surname'), value: 'surname' },
  { label: $gettext('Company'), value: 'company' }
])

// Choosing a key in the list starts ascending; the table's headers turn it.
function chooseSort(key: SortKey) {
  sortKey.value = key
  sortDesc.value = false
}
function onSort(key: SortKey, descending: boolean) {
  sortKey.value = key
  sortDesc.value = descending
}

const currentFolder = computed(() => folders.value.find((f) => f.id === folderId.value) ?? null)
const group = ref<GroupFilter>(null)
const groups = computed(() => listGroups(contacts.value))
const groupCards = computed(() => listGroupCards(contacts.value))

const counts = computed(() => {
  const out: Record<string, number> = {}
  for (const c of contacts.value) {
    const id = c.parentFolderId ?? ''
    out[id] = (out[id] ?? 0) + 1
  }
  return out
})

// The contacts of the chosen address book and group, and of those the ones
// that match the search.
const inView = computed(() =>
  contacts.value.filter(
    (c) => (folderId.value === null || c.parentFolderId === folderId.value) && inGroup(c, group.value)
  )
)
const shown = computed(() =>
  sortContacts(filterContacts(inView.value, query.value), sortKey.value, sortDesc.value)
)
const selected = computed(() => contacts.value.find((c) => c.id === selectedId.value) ?? null)
const editing = ref<'new' | 'edit' | null>(null)

// The title is the group when one is chosen, else the address book.
const title = computed(() => {
  if (group.value?.kind === 'category') return group.value.name
  if (group.value?.kind === 'card') {
    const id = group.value.id
    return groupCards.value.find((g) => g.id === id)?.name ?? $gettext('Group')
  }
  return currentFolder.value?.displayName ?? $gettext('All contacts')
})

// The address books a new contact may go into: the chosen one, or every one
// the user may write to.
const targetFolders = computed(() =>
  (folderId.value === null ? folders.value : folders.value.filter((f) => f.id === folderId.value)).filter(
    (f) => f['groupware.canEdit']
  )
)

// The address book a vCard file goes into: the chosen one if it can be
// written to, and while all are shown the default one, else the first that
// can be.
const importTarget = computed(() => {
  if (currentFolder.value) {
    return currentFolder.value['groupware.canEdit'] ? currentFolder.value : null
  }
  const writable = folders.value.filter((f) => f['groupware.canEdit'])
  return writable.find((f) => f['groupware.isDefault']) ?? writable[0] ?? null
})

const canEdit = (c: GraphContact) =>
  !!folders.value.find((f) => f.id === c.parentFolderId)?.['groupware.canEdit']

// What can be open at once: one dialog.
type Dialog =
  | { kind: 'newBook' }
  | { kind: 'renameBook' }
  | { kind: 'deleteBook' }
  | { kind: 'renameGroup'; from: string }
  | { kind: 'deleteGroup' }
const dialog = ref<Dialog | null>(null)

// Another address book or group ends an edit in progress, and the contact
// that is no longer in the list.
watch([folderId, group], () => {
  editing.value = null
  dialog.value = null
  if (selectedId.value && !inView.value.some((c) => c.id === selectedId.value)) {
    selectedId.value = null
  }
})
watch(folderId, () => {
  importReport.value = null
})

// Closing the sidebar drops a form that is open, so it does not come back
// with old input when the sidebar opens again.
watch(isSideBarOpen, (open) => {
  if (!open) editing.value = null
})

// Only the newest load may write its result, so a slow answer that is no
// longer wanted cannot replace the list.
let loadCounter = 0

async function loadContacts() {
  const mine = ++loadCounter
  loadingContacts.value = true
  const lists = await Promise.all(
    folders.value.map((f) =>
      run($gettext('Could not load the contacts'), (c) => c.listContacts(f.id))
    )
  )
  if (mine !== loadCounter) return
  contacts.value = lists.flatMap((l) => l ?? [])
  if (!contacts.value.some((c) => c.id === selectedId.value)) selectedId.value = null
  loadingContacts.value = false
}

function selectFolder(id: string | null) {
  folderId.value = id
}

function openSideBar() {
  eventBus.publish(SideBarEventTopics.open)
}

function select(id: string) {
  if (id !== selectedId.value) editing.value = null
  selectedId.value = id
  openSideBar()
}

function startNew() {
  selectedId.value = null
  editing.value = 'new'
  openSideBar()
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

async function setCategories(contact: GraphContact, categories: string[]) {
  const updated = await run($gettext('Could not change the groups'), (c) =>
    c.updateContact(contact.parentFolderId ?? '', contact.id, { categories }, contact['@odata.etag'])
  )
  if (updated) replaceContact(updated)
}

function startRename() {
  if (group.value?.kind === 'category') {
    dialog.value = { kind: 'renameGroup', from: group.value.name }
  }
}

// changeGroup rewrites the categories of every contact in the group that the
// user may write to, one request each, and keeps the answers that came back.
async function changeGroup(change: (categories: string[]) => string[]) {
  if (group.value?.kind !== 'category') return false
  const name = group.value.name
  const members = contacts.value.filter((c) => c.categories.includes(name) && canEdit(c))
  let ok = true
  for (const member of members) {
    const updated = await run($gettext('Could not change the group'), (c) =>
      c.updateContact(
        member.parentFolderId ?? '',
        member.id,
        { categories: change(member.categories) },
        member['@odata.etag']
      )
    )
    if (updated) replaceContact(updated)
    else ok = false
  }
  return ok
}

async function renameGroupTo(to: string) {
  const from = dialog.value?.kind === 'renameGroup' ? dialog.value.from : ''
  dialog.value = null
  if (!from || !to) return
  const ok = await changeGroup((cats) => renameGroup(cats, from, to))
  if (ok) group.value = { kind: 'category', name: to }
}

async function deleteGroup() {
  const name = group.value?.kind === 'category' ? group.value.name : ''
  dialog.value = null
  if (!name) return
  const ok = await changeGroup((cats) => removeFromGroup(cats, name))
  if (ok) group.value = null
}

const importReport = ref<ImportReport | null>(null)
const importInput = ref<HTMLInputElement | null>(null)

// downloadBook saves the chosen address book as a .vcf file. The file comes
// through the authenticated client, so it is handed to the browser as an
// object URL on a link that is clicked once.
async function downloadBook() {
  const current = currentFolder.value
  if (!current) return
  const blob = await run($gettext('Could not download the address book'), (c) =>
    c.exportAddressBook(current.id)
  )
  if (blob) saveBlob(blob, fileNameFor(current.displayName, 'vcf'))
}

async function onImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  // The same file can be chosen again.
  input.value = ''
  const target = importTarget.value
  if (!file || !target) return
  const report = await run($gettext('Could not import the file'), (c) =>
    c.importAddressBook(target.id, file)
  )
  if (!report) return
  importReport.value = report
  await loadContacts()
}

async function createBook(name: string) {
  dialog.value = null
  const created = await run($gettext('Could not create the address book'), (c) =>
    c.createContactFolder({ displayName: name })
  )
  if (!created) return
  folders.value = [...folders.value, created]
  selectFolder(created.id)
}

async function renameBook(name: string) {
  dialog.value = null
  const current = currentFolder.value
  if (!current) return
  const updated = await run($gettext('Could not rename the address book'), (c) =>
    c.updateContactFolder(current.id, { displayName: name })
  )
  if (updated) folders.value = folders.value.map((f) => (f.id === updated.id ? updated : f))
}

async function deleteBook() {
  dialog.value = null
  const current = currentFolder.value
  if (!current) return
  const done = await run($gettext('Could not delete the address book'), async (c) => {
    await c.deleteContactFolder(current.id)
    return true
  })
  if (!done) return
  folders.value = folders.value.filter((f) => f.id !== current.id)
  contacts.value = contacts.value.filter((c) => c.parentFolderId !== current.id)
  selectFolder(null)
}

async function saveNew(input: ContactInput, targetId: string) {
  const created = await run($gettext('Could not create the contact'), (c) =>
    c.createContact(targetId, input)
  )
  if (!created) return
  contacts.value = [...contacts.value, created]
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
}

// The sidebar has one panel, with the details of the chosen contact. The
// attributes are read while the sidebar draws, so they follow the page.
const sideBarPanels: SideBarPanel<Item, Item, Item>[] = [
  {
    name: 'details',
    icon: 'information',
    iconFillType: 'line',
    title: () => $gettext('Details'),
    isVisible: () => true,
    isRoot: () => true,
    component: ContactPanel as Component as SideBarPanel<Item, Item, Item>['component'],
    componentAttrs: () => ({
      contact: selected.value,
      editing: editing.value,
      canEdit: selected.value ? canEdit(selected.value) : false,
      folders: folders.value,
      targetFolders: targetFolders.value,
      suggestions: groups.value.map((g) => g.name),
      onEdit: () => (editing.value = 'edit'),
      onCancel: () => (editing.value = null),
      onDelete: () => selected.value && remove(selected.value),
      onPhotoChanged: () => selected.value && reload(selected.value),
      onSaveNew: saveNew,
      onSaveEdit: saveEdit,
      onSetCategories: (categories: string[]) =>
        selected.value && setCategories(selected.value, categories)
    })
  }
]

// "/" goes to the search and "n" starts a new contact, unless the user is
// typing or holds a modifier.
function onKeydown(event: KeyboardEvent) {
  if (event.ctrlKey || event.metaKey || event.altKey || dialog.value) return
  const target = event.target as HTMLElement | null
  if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return
  if (event.key === '/') {
    event.preventDefault()
    document.getElementById('gw-contact-search')?.focus()
  } else if (event.key === 'n' && targetFolders.value.length > 0) {
    event.preventDefault()
    startNew()
  }
}

onMounted(async () => {
  document.addEventListener('keydown', onKeydown)
  folders.value =
    (await run($gettext('Could not load the address books'), (c) => c.listContactFolders())) ?? []
  loadingFolders.value = false
  await loadContacts()
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<style>
.gw-search {
  width: 16rem;
}
.gw-search .oc-label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
.gw-sort {
  width: 9rem;
}
.gw-book-menu {
  min-width: 12rem;
}
/* On a phone the app bar has room for the search only; the table's headers
   still sort. */
@media (max-width: 639px) {
  .gw-search {
    width: 9rem;
  }
  .gw-sort {
    display: none;
  }
}
</style>
