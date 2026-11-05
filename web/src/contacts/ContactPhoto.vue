<template>
  <div class="contact-photo oc-flex oc-flex-middle oc-mb-m">
    <img
      v-if="url"
      :src="url"
      :alt="$gettext('Photo of %{name}', { name })"
      class="avatar"
      data-testid="photo-image"
    />
    <span v-else class="avatar initials" aria-hidden="true" data-testid="photo-initials">{{
      initials(name)
    }}</span>
    <div v-if="canEdit" class="oc-ml-m">
      <label class="oc-button oc-button-s oc-button-passive photo-choose">
        {{ contact['groupware.hasPhoto'] ? $gettext('Change photo') : $gettext('Add photo') }}
        <input
          type="file"
          accept="image/jpeg,image/png"
          class="oc-invisible-sr"
          data-testid="photo-input"
          @change="onChoose"
        />
      </label>
      <button
        v-if="contact['groupware.hasPhoto']"
        type="button"
        class="oc-button oc-button-s oc-button-passive oc-ml-xs"
        data-testid="photo-remove"
        @click="remove"
      >
        {{ $gettext('Remove photo') }}
      </button>
      <p v-if="error" class="oc-text-error oc-text-small oc-m-rm" role="alert" data-testid="photo-error">
        {{ error }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact } from '../api/types'
import { useApi } from '../composables/useApi'
import { initials } from './model'
import { checkPhoto } from './photo'

defineOptions({ name: 'ContactPhoto' })

const props = defineProps<{
  contact: GraphContact
  // What to show as initials when there is no photo.
  name: string
  canEdit: boolean
}>()
const emit = defineEmits<{ changed: [] }>()

const { $gettext } = useGettext()
const { run } = useApi()
const url = ref('')
const error = ref('')

function release() {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
}

// Only the newest request may set the image, since the contact can change
// while a photo is still loading.
let loads = 0

async function load() {
  const mine = ++loads
  release()
  if (!props.contact['groupware.hasPhoto']) return
  const blob = await run($gettext('Could not load the photo'), (c) =>
    c.getContactPhoto(props.contact.parentFolderId ?? '', props.contact.id)
  )
  if (mine !== loads || !blob) return
  url.value = URL.createObjectURL(blob)
}

// The photo changes with the contact, or with its etag after an upload.
watch(
  () => [props.contact.id, props.contact['groupware.hasPhoto'], props.contact['@odata.etag']],
  load,
  { immediate: true }
)
onBeforeUnmount(release)

async function onChoose(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  error.value = ''
  if (!file) return
  const problem = checkPhoto(file)
  if (problem === 'type') {
    error.value = $gettext('The photo must be a JPEG or PNG image.')
  } else if (problem === 'size') {
    error.value = $gettext('The photo must be at most 1 MB.')
  } else {
    const done = await run($gettext('Could not save the photo'), (c) =>
      c.setContactPhoto(props.contact.parentFolderId ?? '', props.contact.id, file)
    )
    if (done) emit('changed')
  }
  // The same file can be chosen again after a failure.
  input.value = ''
}

async function remove() {
  error.value = ''
  const done = await run($gettext('Could not remove the photo'), async (c) => {
    await c.deleteContactPhoto(props.contact.parentFolderId ?? '', props.contact.id)
    return true
  })
  if (done) emit('changed')
}
</script>

<style scoped>
.avatar {
  width: 5rem;
  height: 5rem;
  border-radius: 50%;
  object-fit: cover;
  flex: none;
}
.initials {
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--oc-color-background-highlight);
  font-size: 1.75rem;
}
.photo-choose {
  cursor: pointer;
}
</style>
