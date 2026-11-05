<template>
  <gw-modal
    :title="$gettext('Share %{name}', { name })"
    wide
    initial-focus="input[data-testid=share-search]"
    @close="emit('close')"
  >
    <h3 class="gw-share-heading oc-mt-rm">{{ $gettext('Add people') }}</h3>
    <div v-if="chosen" class="gw-share-invite" data-testid="share-chosen">
      <oc-tag size="medium" class="gw-share-chosen">
        <oc-avatar :user-name="chosen.name" :width="24" />
        <span class="oc-text-truncate">{{ chosen.name }}</span>
        <span class="oc-text-muted">{{ typeLabel(chosen.type) }}</span>
      </oc-tag>
      <oc-button
        appearance="raw"
        :aria-label="$gettext('Change the person to share with')"
        data-testid="share-unchoose"
        @click="chosen = undefined"
      >
        <oc-icon name="close" fill-type="line" size="small" />
      </oc-button>
      <oc-select
        v-model="newRole"
        :label="$gettext('Access')"
        label-hidden
        :options="roleOptions"
        :reduce="(o: RoleOption) => o.value"
        :clearable="false"
        :searchable="false"
        class="gw-share-role"
        data-testid="share-new-role"
      />
      <oc-button variation="primary" appearance="filled" data-testid="share-add" @click="add">
        {{ $gettext('Share') }}
      </oc-button>
    </div>
    <div v-else class="gw-share-search">
      <oc-text-input
        id="gw-share-search"
        ref="searchField"
        v-model="term"
        type="search"
        autocomplete="off"
        :label="$gettext('Search users and groups')"
        :description-message="enough ? '' : hint"
        :fix-message-line="true"
        data-testid="share-search"
        @keydown="onSearchKey"
      />
      <p
        v-if="enough && searched && results.length === 0"
        class="oc-text-muted oc-text-small oc-mt-xs oc-mb-rm"
        role="status"
        data-testid="share-no-match"
      >
        {{ $gettext('No user or group matches.') }}
      </p>
      <ul
        v-if="results.length > 0"
        id="gw-share-results"
        class="oc-list gw-share-results"
        role="listbox"
        :aria-label="$gettext('Results')"
      >
        <li
          v-for="(s, i) in results"
          :id="`gw-share-result-${i}`"
          :key="`${s.type}:${s.id}`"
          role="option"
          tabindex="-1"
          class="gw-list-option"
          :class="{ 'gw-list-option-active': i === active }"
          :aria-selected="i === active ? 'true' : 'false'"
          data-testid="share-result"
          @click="choose(s)"
          @keydown.enter.prevent="choose(s)"
          @mousemove="active = i"
        >
          <oc-avatar :user-name="s.name" :width="24" />
          <span class="oc-text-truncate">{{ s.name }}</span>
          <span class="oc-text-muted gw-share-kind">{{ typeLabel(s.type) }}</span>
        </li>
      </ul>
    </div>

    <h3 class="gw-share-heading">{{ $gettext('Shared with') }}</h3>
    <div v-if="loading" role="status"><oc-spinner :aria-label="$gettext('Loading')" /></div>
    <p v-else-if="permissions.length === 0" class="oc-text-muted oc-mt-rm" data-testid="share-none">
      {{ $gettext('Not shared with anyone yet.') }}
    </p>
    <ul v-else class="oc-list gw-share-list" :aria-label="$gettext('Shared with')">
      <li v-for="p in permissions" :key="p.id" class="gw-share-row" data-testid="share-item">
        <oc-avatar :user-name="p.emailAddress.name" :width="24" />
        <span class="gw-share-who">
          <span class="name oc-text-truncate">{{ p.emailAddress.name }}</span>
          <span class="oc-text-muted gw-share-kind">{{ typeLabel(p.grantee.type) }}</span>
        </span>
        <oc-select
          :model-value="p.role"
          :label="$gettext('Access of %{name}', { name: p.emailAddress.name })"
          label-hidden
          :options="roleOptions"
          :reduce="(o: RoleOption) => o.value"
          :clearable="false"
          :searchable="false"
          class="gw-share-role"
          data-testid="share-role"
          @update:model-value="changeRole(p, $event)"
        />
        <oc-button
          v-if="p.isRemovable"
          appearance="raw"
          :aria-label="$gettext('Stop sharing with %{name}', { name: p.emailAddress.name })"
          data-testid="share-revoke"
          @click="revoke(p)"
        >
          <oc-icon name="close" fill-type="line" />
        </oc-button>
      </li>
    </ul>

    <template #actions>
      <oc-button variation="passive" appearance="outline" data-testid="dialog-cancel" @click="emit('close')">
        {{ $gettext('Close') }}
      </oc-button>
          </template>
  </gw-modal>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, watchEffect } from 'vue'
import { useGettext } from 'vue3-gettext'
import GwModal from '../components/GwModal.vue'
import { useApi } from '../composables/useApi'
import type { GraphPermission, Role, ShareKind } from '../api/types'
import { minSearchLength, searchSharees, type Sharee } from './search'

defineOptions({ name: 'ShareDialog' })

// The owner's view of one calendar or address book: who it is shared with,
// and a search to add users and groups of the oCIS directory.
const props = defineProps<{ kind: ShareKind; collectionId: string; name: string }>()
const emit = defineEmits<{ close: [] }>()

const { $gettext } = useGettext()
const { run } = useApi()

const permissions = ref<GraphPermission[]>([])
const loading = ref(true)
const term = ref('')
const results = ref<Sharee[]>([])
const searched = ref(false)
const chosen = ref<Sharee>()
const newRole = ref<Role>('read')

const enough = computed(() => term.value.trim().length >= minSearchLength)
const hint = computed(() =>
  $gettext('Type at least %{n} characters of a name.', { n: String(minSearchLength) })
)

interface RoleOption {
  label: string
  value: Role
}
const roleOptions = computed<RoleOption[]>(() => [
  { label: roleLabel('read'), value: 'read' },
  { label: roleLabel('write'), value: 'write' }
])

// The result the arrow keys point at. It is not chosen before Enter.
const active = ref(-1)

// The text field of the design system hands the attributes it does not know to
// its root element as well as to the input. The combobox roles belong on the
// input only, so they are set on it directly.
const searchField = ref<{ $el: HTMLElement }>()
watchEffect(
  () => {
    const input = searchField.value?.$el.querySelector('input')
    if (!input) {
      return
    }
    input.setAttribute('role', 'combobox')
    input.setAttribute('aria-autocomplete', 'list')
    input.setAttribute('aria-controls', 'gw-share-results')
    input.setAttribute('aria-expanded', results.value.length > 0 ? 'true' : 'false')
    if (active.value >= 0) {
      input.setAttribute('aria-activedescendant', `gw-share-result-${active.value}`)
    } else {
      input.removeAttribute('aria-activedescendant')
    }
  },
  { flush: 'post' }
)

function onSearchKey(event: KeyboardEvent) {
  const count = results.value.length
  if (count === 0) {
    return
  }
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    active.value = (active.value + 1) % count
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    active.value = (active.value - 1 + count) % count
  } else if (event.key === 'Enter' && active.value >= 0) {
    event.preventDefault()
    choose(results.value[active.value])
  }
}

const typeLabel = (t: string) => (t === 'group' ? $gettext('Group') : $gettext('User'))
const roleLabel = (r: string) => (r === 'write' ? $gettext('Can edit') : $gettext('Can view'))

// The search waits until the typing stops, and only the newest search may
// write its answer, so a slow one for an older text cannot replace it.
const delay = 300
let timer: ReturnType<typeof setTimeout> | undefined
let searchCounter = 0

watch(term, scheduleSearch)

function scheduleSearch() {
  clearTimeout(timer)
  const mine = ++searchCounter
  searched.value = false
  active.value = -1
  if (!enough.value) {
    results.value = []
    return
  }
  timer = setTimeout(async () => {
    const found = await run($gettext('Could not search for users and groups'), (c) =>
      searchSharees(
        c,
        term.value.trim(),
        permissions.value.map((p) => p.grantee)
      )
    )
    if (mine !== searchCounter) {
      return
    }
    results.value = found ?? []
    searched.value = true
  }, delay)
}

function choose(s: Sharee) {
  chosen.value = s
  newRole.value = 'read'
  term.value = ''
  results.value = []
  searched.value = false
}

async function add() {
  const who = chosen.value
  if (!who) {
    return
  }
  const created = await run($gettext('Could not share'), (c) =>
    c.createPermission(props.kind, props.collectionId, { type: who.type, id: who.id }, newRole.value)
  )
  if (created) {
    permissions.value = [...permissions.value, created]
    chosen.value = undefined
  }
}

async function changeRole(p: GraphPermission, role: Role) {
  const changed = await run($gettext('Could not change the access'), (c) =>
    c.updatePermission(props.kind, props.collectionId, p.id, role)
  )
  // On a failure the list keeps the old role, which the control shows again.
  if (changed) {
    permissions.value = permissions.value.map((x) => (x.id === p.id ? changed : x))
  }
}

async function revoke(p: GraphPermission) {
  const done = await run($gettext('Could not stop sharing'), async (c) => {
    await c.deletePermission(props.kind, props.collectionId, p.id)
    return true
  })
  if (done) {
    permissions.value = permissions.value.filter((x) => x.id !== p.id)
  }
}

onMounted(async () => {
  permissions.value =
    (await run($gettext('Could not load who this is shared with'), (c) =>
      c.listPermissions(props.kind, props.collectionId)
    )) ?? []
  loading.value = false
})

onBeforeUnmount(() => clearTimeout(timer))
</script>

<style>
.gw-share-heading {
  margin: var(--oc-space-medium) 0 var(--oc-space-small);
  font-size: var(--oc-font-size-default);
  font-weight: var(--oc-font-weight-semibold);
}
.gw-share-invite {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--oc-space-small);
}
.gw-share-chosen {
  display: inline-flex;
  align-items: center;
  gap: var(--oc-space-small);
  min-width: 0;
}
.gw-share-role {
  min-width: 9rem;
}
.gw-share-results {
  max-height: 12rem;
  overflow-y: auto;
  margin: 0;
  border: 1px solid var(--oc-color-border);
  border-radius: 5px;
}
.gw-list-option {
  display: flex;
  align-items: center;
  gap: var(--oc-space-small);
  min-height: 2.25rem;
  padding: 0 var(--oc-space-small);
  cursor: pointer;
}
.gw-list-option:hover,
.gw-list-option-active {
  background: var(--oc-color-background-hover);
}
.gw-share-row {
  display: flex;
  align-items: center;
  gap: var(--oc-space-small);
  min-height: 2.75rem;
  border-bottom: 1px solid var(--oc-color-border);
}
.gw-share-who {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  align-items: baseline;
  column-gap: var(--oc-space-small);
  min-width: 0;
}
.gw-share-kind {
  font-size: var(--oc-font-size-small);
}
</style>
