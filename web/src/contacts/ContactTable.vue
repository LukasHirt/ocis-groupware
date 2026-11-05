<template>
  <oc-table
    class="gw-contact-table"
    :data="contacts"
    :fields="fields"
    id-key="id"
    :highlighted="selectedId ?? undefined"
    :sort-by="tableSortBy"
    :sort-dir="sortDesc ? 'desc' : 'asc'"
    :caption="$gettext('Contacts')"
    :caption-visible="false"
    hover
    sticky
    padding-x="medium"
    @highlight="onHighlight"
    @sort="onSort"
  >
    <template #name="{ item }">
      <div class="oc-flex oc-flex-middle gw-contact-name">
        <oc-avatar :user-name="label(item)" :width="28" class="oc-mr-s" />
        <oc-button
          appearance="raw"
          justify-content="left"
          class="gw-contact-link"
          :aria-current="item.id === selectedId"
          data-testid="contact-item"
          @click.stop="emit('select', item.id)"
        >
          <span class="name oc-text-truncate">{{ label(item) }}</span>
        </oc-button>
      </div>
    </template>
    <template #email="{ item }">
      <span v-if="first(item).email" class="oc-text-truncate">{{ first(item).email }}</span>
      <span v-else class="oc-text-muted">&ndash;</span>
    </template>
    <template #phone="{ item }">
      <span v-if="first(item).phone" class="oc-text-truncate">{{ first(item).phone }}</span>
      <span v-else class="oc-text-muted">&ndash;</span>
    </template>
    <template #company="{ item }">
      <span v-if="first(item).company" class="oc-text-truncate">{{ first(item).company }}</span>
      <span v-else class="oc-text-muted">&ndash;</span>
    </template>
  </oc-table>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact } from '../api/types'
import { contactName, type SortKey } from './model'

defineOptions({ name: 'ContactTable' })

// The contacts as a table, one row each. The name is a button, so a row can
// be chosen with the keyboard; a click anywhere in the row chooses it too.
const props = defineProps<{
  contacts: GraphContact[]
  selectedId: string | null
  sortKey: SortKey
  sortDesc: boolean
}>()
const emit = defineEmits<{
  select: [id: string]
  sort: [key: SortKey, descending: boolean]
}>()

const { $gettext } = useGettext()

// The columns that the page can sort by; the others are not sortable.
const sortable: SortKey[] = ['name', 'company']
const tableSortBy = computed(() => (sortable.includes(props.sortKey) ? props.sortKey : undefined))

// The email and phone columns go below 960px, the company below 1200px, and
// the name stays, see the style below.
const fields = computed(() => [
  { name: 'name', title: $gettext('Name'), type: 'slot', sortable: true },
  { name: 'email', title: $gettext('Email'), type: 'slot', thClass: 'gw-col-wide', tdClass: 'gw-col-wide' },
  { name: 'phone', title: $gettext('Phone'), type: 'slot', thClass: 'gw-col-wide', tdClass: 'gw-col-wide' },
  {
    name: 'company',
    title: $gettext('Company'),
    type: 'slot',
    sortable: true,
    thClass: 'gw-col-wider',
    tdClass: 'gw-col-wider'
  }
])

// The table hands over its items as plain records.
const asContact = (item: unknown) => item as GraphContact

function label(item: unknown): string {
  return contactName(asContact(item)) || $gettext('Unnamed contact')
}

// first picks what each column shows: the first address, the mobile phone,
// else a work phone, else a home phone.
function first(item: unknown) {
  const c = asContact(item)
  return {
    email: c.emailAddresses[0]?.address ?? '',
    phone: c.mobilePhone || c.businessPhones[0] || c.homePhones[0] || '',
    company: c.companyName
  }
}

// The table emits the item with the click, or a list of both.
function onHighlight(args: unknown) {
  const item = (Array.isArray(args) ? args[0] : args) as GraphContact | undefined
  if (item?.id) emit('select', item.id)
}

function onSort({ sortBy, sortDir }: { sortBy: string; sortDir: 'asc' | 'desc' }) {
  if (sortable.includes(sortBy as SortKey)) emit('sort', sortBy as SortKey, sortDir === 'desc')
}
</script>

<style>
.gw-contact-name {
  min-width: 0;
}
.gw-contact-link {
  min-width: 0;
}
.gw-contact-table .gw-col-wide,
.gw-contact-table .gw-col-wider {
  display: none;
}
@media (min-width: 960px) {
  .gw-contact-table .gw-col-wide {
    display: table-cell;
  }
}
@media (min-width: 1200px) {
  .gw-contact-table .gw-col-wider {
    display: table-cell;
  }
}
</style>
