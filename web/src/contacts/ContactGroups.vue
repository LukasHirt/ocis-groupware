<template>
  <section class="contact-groups oc-mt-m" :aria-label="$gettext('Groups')">
    <span class="oc-text-muted">{{ $gettext('Groups') }}</span>
    <ul class="oc-list oc-flex oc-flex-wrap oc-mt-xs">
      <li v-for="name in contact.categories" :key="name" class="chip oc-mr-xs oc-mb-xs">
        <span data-testid="membership-name">{{ name }}</span>
        <button
          v-if="canEdit"
          type="button"
          class="oc-button oc-button-s oc-button-passive"
          :aria-label="$gettext('Remove from group %{name}', { name })"
          data-testid="membership-remove"
          @click="emit('change', removeFromGroup(contact.categories, name))"
        >
          &times;
        </button>
      </li>
    </ul>
    <p v-if="contact.categories.length === 0" class="oc-text-muted oc-m-rm">
      {{ $gettext('Not in any group.') }}
    </p>
    <div v-if="canEdit" class="oc-flex oc-flex-middle oc-mt-xs">
      <input
        v-model="newName"
        list="contact-group-names"
        class="oc-text-input oc-mr-s"
        :aria-label="$gettext('Add to group')"
        :placeholder="$gettext('Add to group')"
        data-testid="membership-input"
        @keydown.enter.prevent="add"
      />
      <datalist id="contact-group-names">
        <option v-for="s in suggestions" :key="s" :value="s" />
      </datalist>
      <button
        type="button"
        class="oc-button oc-button-s oc-button-passive"
        :disabled="!newName.trim()"
        data-testid="membership-add"
        @click="add"
      >
        {{ $gettext('Add') }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact } from '../api/types'
import { addToGroup, removeFromGroup } from './groups'

defineOptions({ name: 'ContactGroups' })

const props = defineProps<{
  contact: GraphContact
  canEdit: boolean
  // The names of the groups that exist, offered while typing.
  suggestions: string[]
}>()
const emit = defineEmits<{ change: [categories: string[]] }>()
const { $gettext } = useGettext()
const newName = ref('')

function add() {
  const next = addToGroup(props.contact.categories, newName.value)
  newName.value = ''
  if (next !== props.contact.categories) emit('change', next)
}
</script>

<style scoped>
.chip {
  display: inline-flex;
  align-items: center;
  padding-left: var(--oc-space-small);
  border-radius: var(--oc-space-medium);
  background: var(--oc-color-background-highlight);
  list-style: none;
}
</style>
