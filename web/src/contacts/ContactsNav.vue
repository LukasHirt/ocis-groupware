<template>
  <nav class="contacts-nav" :aria-label="$gettext('Address books')">
    <div class="oc-flex oc-flex-middle oc-flex-between gw-nav-heading-row">
      <h2 class="gw-list-heading gw-nav-heading">{{ $gettext('Address books') }}</h2>
      <oc-button
        appearance="raw"
        :aria-label="$gettext('New address book')"
        data-testid="book-new"
        @click="emit('newBook')"
      >
        <oc-icon name="add" fill-type="line" size="small" />
      </oc-button>
    </div>
    <ul class="oc-list">
      <li>
        <oc-button
          appearance="raw"
          justify-content="left"
          class="gw-list-item oc-width-1-1"
          :class="{ selected: folderId === null }"
          :aria-current="folderId === null"
          data-testid="folder-all"
          @click="emit('selectFolder', null)"
        >
          <oc-icon name="contacts-book" fill-type="line" />
          <span class="name">{{ $gettext('All contacts') }}</span>
          <span class="gw-count">{{ total }}</span>
        </oc-button>
      </li>
      <li v-for="folder in folders" :key="folder.id">
        <oc-button
          appearance="raw"
          justify-content="left"
          class="gw-list-item gw-list-item-tall oc-width-1-1"
          :class="{ selected: folderId === folder.id }"
          :aria-current="folderId === folder.id"
          data-testid="folder-item"
          @click="emit('selectFolder', folder.id)"
        >
          <oc-icon name="contacts-book" fill-type="line" />
          <span class="gw-list-text">
            <span class="name oc-text-truncate">{{ folder.displayName }}</span>
            <span v-if="sharedNote(folder)" class="gw-list-note oc-text-truncate">{{
              sharedNote(folder)
            }}</span>
          </span>
          <span class="gw-count">{{ counts[folder.id] ?? 0 }}</span>
        </oc-button>
      </li>
    </ul>

    <h2 class="gw-list-heading">{{ $gettext('Groups') }}</h2>
    <ul class="oc-list">
      <li>
        <oc-button
          appearance="raw"
          justify-content="left"
          class="gw-list-item oc-width-1-1"
          :class="{ selected: group === null }"
          :aria-current="group === null"
          data-testid="group-all"
          @click="emit('selectGroup', null)"
        >
          <oc-icon name="group" fill-type="line" />
          <span class="name">{{ $gettext('Every contact') }}</span>
        </oc-button>
      </li>
      <li v-for="g in groups" :key="`c:${g.name}`">
        <oc-button
          appearance="raw"
          justify-content="left"
          class="gw-list-item oc-width-1-1"
          :class="{ selected: group?.kind === 'category' && group.name === g.name }"
          :aria-current="group?.kind === 'category' && group.name === g.name"
          data-testid="group-item"
          @click="emit('selectGroup', { kind: 'category', name: g.name })"
        >
          <oc-icon name="price-tag-3" fill-type="line" />
          <span class="name oc-text-truncate">{{ g.name }}</span>
          <span class="gw-count">{{ g.count }}</span>
        </oc-button>
      </li>
      <li v-for="g in groupCards" :key="`g:${g.id}`">
        <oc-button
          appearance="raw"
          justify-content="left"
          class="gw-list-item gw-list-item-tall oc-width-1-1"
          :class="{ selected: group?.kind === 'card' && group.id === g.id }"
          :aria-current="group?.kind === 'card' && group.id === g.id"
          data-testid="groupcard-item"
          @click="emit('selectGroup', { kind: 'card', id: g.id })"
        >
          <oc-icon name="team" fill-type="line" />
          <span class="gw-list-text">
            <span class="name oc-text-truncate">{{ g.name }}</span>
            <span class="gw-list-note">{{ $gettext('Read only') }}</span>
          </span>
          <span class="gw-count">{{ g.count }}</span>
        </oc-button>
      </li>
    </ul>
  </nav>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'
import type { GraphContactFolder } from '../api/types'
import type { GroupFilter } from './groups'

defineOptions({ name: 'ContactsNav' })

// The list pane: the address books and the groups, with the number of
// contacts in each. Choosing one only emits; the page decides what to show.
defineProps<{
  folders: GraphContactFolder[]
  // The chosen address book; null for all of them.
  folderId: string | null
  // Contacts per address book, and in all of them.
  counts: Record<string, number>
  total: number
  groups: { name: string; count: number }[]
  groupCards: { id: string; name: string; count: number }[]
  group: GroupFilter
}>()
const emit = defineEmits<{
  selectFolder: [id: string | null]
  selectGroup: [group: GroupFilter]
  newBook: []
}>()

const { $gettext } = useGettext()

// A book that belongs to someone else says whose it is and what the user may
// do with it. The user's own books say nothing.
function sharedNote(folder: GraphContactFolder): string {
  if (folder['groupware.canShare']) return ''
  const access = folder['groupware.canEdit'] ? $gettext('Can edit') : $gettext('Read only')
  return `${folder['groupware.owner'].name} · ${access}`
}
</script>

<style scoped>
.gw-nav-heading-row {
  margin: var(--oc-space-small) 0 var(--oc-space-xsmall);
  padding: 0 var(--oc-space-small);
}
.gw-nav-heading {
  margin: 0;
}
.gw-list-item-tall {
  height: auto;
  min-height: 2.25rem;
  padding-top: var(--oc-space-xsmall);
  padding-bottom: var(--oc-space-xsmall);
}
.gw-list-text {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  min-width: 0;
  text-align: left;
}
.gw-list-note {
  font-size: var(--oc-font-size-small);
  color: var(--oc-color-text-muted);
  max-width: 100%;
}
.gw-list-item {
  font-weight: normal;
}
.gw-list-item.selected {
  font-weight: var(--oc-font-weight-semibold);
}
</style>
