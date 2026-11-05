<template>
  <modal-dialog :title="$gettext('Share %{name}', { name })" wide @close="emit('close')">
    <h3 class="oc-h5 oc-mt-rm">{{ $gettext('Shared with') }}</h3>
    <div v-if="loading" role="status"><oc-spinner :aria-label="$gettext('Loading')" /></div>
    <p v-else-if="permissions.length === 0" class="oc-text-muted" data-testid="share-none">
      {{ $gettext('Not shared with anyone yet.') }}
    </p>
    <ul v-else class="oc-list groupware-share-list" :aria-label="$gettext('Shared with')">
      <li
        v-for="p in permissions"
        :key="p.id"
        class="oc-flex oc-flex-middle oc-py-xs"
        data-testid="share-item"
      >
        <span class="groupware-share-who">
          <span class="name">{{ p.emailAddress.name }}</span>
          <span class="oc-text-muted oc-ml-s">{{ typeLabel(p.grantee.type) }}</span>
        </span>
        <select
          class="oc-select oc-mr-xs"
          :value="p.role"
          :aria-label="$gettext('Access of %{name}', { name: p.emailAddress.name })"
          data-testid="share-role"
          @change="changeRole(p, $event)"
        >
          <option v-for="role in p.allowedRoles" :key="role" :value="role">
            {{ roleLabel(role) }}
          </option>
        </select>
        <button
          v-if="p.isRemovable"
          type="button"
          class="oc-button oc-button-s oc-button-passive"
          :aria-label="$gettext('Stop sharing with %{name}', { name: p.emailAddress.name })"
          data-testid="share-revoke"
          @click="revoke(p)"
        >
          {{ $gettext('Remove') }}
        </button>
      </li>
    </ul>

    <h3 class="oc-h5 oc-mt-m">{{ $gettext('Add people') }}</h3>
    <div v-if="chosen" class="oc-flex oc-flex-middle oc-flex-wrap" data-testid="share-chosen">
      <span class="groupware-share-who">
        <span class="name">{{ chosen.name }}</span>
        <span class="oc-text-muted oc-ml-s">{{ typeLabel(chosen.type) }}</span>
      </span>
      <select
        v-model="newRole"
        class="oc-select oc-mr-xs"
        :aria-label="$gettext('Access')"
        data-testid="share-new-role"
      >
        <option value="read">{{ roleLabel('read') }}</option>
        <option value="write">{{ roleLabel('write') }}</option>
      </select>
      <button
        type="button"
        class="oc-button oc-button-s oc-button-passive"
        data-testid="share-unchoose"
        @click="chosen = undefined"
      >
        {{ $gettext('Change') }}
      </button>
    </div>
    <div v-else>
      <label class="oc-invisible-sr" for="groupware-share-search">{{
        $gettext('Search users and groups')
      }}</label>
      <input
        id="groupware-share-search"
        v-model="term"
        type="search"
        class="oc-text-input oc-width-1-1"
        autocomplete="off"
        :placeholder="$gettext('Search users and groups')"
        data-testid="share-search"
        @input="scheduleSearch"
      />
      <p v-if="!enough" class="oc-text-muted oc-text-small oc-mb-rm" data-testid="share-hint">
        {{ $gettext('Type at least %{n} characters of a name.', { n: String(minSearchLength) }) }}
      </p>
      <p
        v-else-if="searched && results.length === 0"
        class="oc-text-muted oc-text-small oc-mb-rm"
        role="status"
        data-testid="share-no-match"
      >
        {{ $gettext('No user or group matches.') }}
      </p>
      <ul v-if="results.length > 0" class="oc-list oc-mt-xs" :aria-label="$gettext('Results')">
        <li v-for="s in results" :key="`${s.type}:${s.id}`">
          <button
            type="button"
            class="oc-button oc-button-m oc-button-passive oc-button-justify-content-left oc-width-1-1"
            data-testid="share-result"
            @click="choose(s)"
          >
            <span class="name">{{ s.name }}</span>
            <span class="oc-text-muted oc-ml-s">{{ typeLabel(s.type) }}</span>
          </button>
        </li>
      </ul>
    </div>

    <div class="groupware-modal-actions">
      <button
        type="button"
        class="oc-button oc-button-m oc-button-passive"
        data-testid="dialog-cancel"
        @click="emit('close')"
      >
        {{ $gettext('Close') }}
      </button>
      <button
        type="button"
        class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
        :disabled="!chosen"
        data-testid="share-add"
        @click="add"
      >
        {{ $gettext('Share') }}
      </button>
    </div>
  </modal-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import ModalDialog from '../components/ModalDialog.vue'
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

const typeLabel = (t: string) => (t === 'group' ? $gettext('Group') : $gettext('User'))
const roleLabel = (r: string) => (r === 'write' ? $gettext('Can edit') : $gettext('Can view'))

// The search waits until the typing stops, and only the newest search may
// write its answer, so a slow one for an older text cannot replace it.
const delay = 300
let timer: ReturnType<typeof setTimeout> | undefined
let searchCounter = 0

function scheduleSearch() {
  clearTimeout(timer)
  const mine = ++searchCounter
  searched.value = false
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

async function changeRole(p: GraphPermission, event: Event) {
  const select = event.target as HTMLSelectElement
  const role = select.value as Role
  const changed = await run($gettext('Could not change the access'), (c) =>
    c.updatePermission(props.kind, props.collectionId, p.id, role)
  )
  if (changed) {
    permissions.value = permissions.value.map((x) => (x.id === p.id ? changed : x))
  } else {
    // The list still has the old role, so the control goes back to it.
    select.value = p.role
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
.groupware-share-who {
  flex: 1 1 auto;
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
