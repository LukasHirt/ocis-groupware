<template>
  <section class="gw-contact-groups" :aria-label="$gettext('Groups')">
    <h4 class="gw-section-label">{{ $gettext('Groups') }}</h4>
    <ul class="oc-list gw-group-tags">
      <li v-for="name in contact.categories" :key="name">
        <oc-tag rounded size="small">
          <span data-testid="membership-name">{{ name }}</span>
          <oc-button
            v-if="canEdit"
            appearance="raw"
            size="small"
            :aria-label="$gettext('Remove from group %{name}', { name })"
            data-testid="membership-remove"
            @click="emit('change', removeFromGroup(contact.categories, name))"
          >
            <oc-icon name="close" fill-type="line" size="small" />
          </oc-button>
        </oc-tag>
      </li>
      <li v-if="canEdit && !adding">
        <oc-button
          appearance="raw"
          size="small"
          data-testid="membership-start"
          @click="startAdding"
        >
          <oc-icon name="add" fill-type="line" size="small" />
          {{ $gettext('Add') }}
        </oc-button>
      </li>
    </ul>
    <p v-if="contact.categories.length === 0 && !canEdit" class="oc-text-muted oc-m-rm">
      {{ $gettext('Not in any group.') }}
    </p>
    <div v-if="canEdit && adding" class="gw-group-add oc-flex oc-flex-bottom">
      <oc-text-input
        id="gw-membership-input"
        v-model="newName"
        list="gw-contact-group-names"
        class="gw-group-input"
        :label="$gettext('Add to group')"
        data-testid="membership-input"
        @keydown.enter.prevent="add"
        @keydown.esc.stop="stopAdding"
      />
      <datalist id="gw-contact-group-names">
        <option v-for="s in suggestions" :key="s" :value="s" />
      </datalist>
      <oc-button
        variation="primary"
        appearance="filled"
        :disabled="!newName.trim()"
        data-testid="membership-add"
        @click="add"
      >
        {{ $gettext('Add') }}
      </oc-button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact } from '../api/types'
import { addToGroup, removeFromGroup } from './groups'

defineOptions({ name: 'ContactGroups' })

// The groups a contact is in, as tags. Someone who may change the contact can
// remove a tag and add a group; the field for a name opens on a button.
const props = defineProps<{
  contact: GraphContact
  canEdit: boolean
  // The names of the groups that exist, offered while typing.
  suggestions: string[]
}>()
const emit = defineEmits<{ change: [categories: string[]] }>()
const { $gettext } = useGettext()
const newName = ref('')
const adding = ref(false)

// The field takes the focus when it opens, so the name can be typed at once.
async function startAdding() {
  adding.value = true
  await nextTick()
  document.getElementById('gw-membership-input')?.focus()
}

// Another contact starts with the field closed.
watch(
  () => props.contact.id,
  () => stopAdding()
)

function stopAdding() {
  newName.value = ''
  adding.value = false
}

function add() {
  const next = addToGroup(props.contact.categories, newName.value)
  stopAdding()
  if (next !== props.contact.categories) emit('change', next)
}
</script>

<style>
.gw-group-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--oc-space-xsmall);
}
.gw-group-add {
  gap: var(--oc-space-small);
  margin-top: var(--oc-space-small);
}
.gw-group-input {
  flex: 1;
  min-width: 0;
}
</style>
