<template>
  <div class="gw-contact-photo">
    <img
      v-if="url"
      :src="url"
      :alt="$gettext('Photo of %{name}', { name })"
      class="gw-photo-image"
      data-testid="photo-image"
    />
    <oc-avatar v-else :user-name="name" :width="80" data-testid="photo-initials" />
    <div v-if="canEdit" class="gw-photo-actions oc-flex oc-flex-center">
      <oc-button
        appearance="raw"
        size="small"
        data-testid="photo-choose"
        @click="input?.click()"
      >
        {{ contact['groupware.hasPhoto'] ? $gettext('Change photo') : $gettext('Add photo') }}
      </oc-button>
      <input
        ref="input"
        type="file"
        accept="image/jpeg,image/png"
        class="oc-invisible-sr"
        tabindex="-1"
        aria-hidden="true"
        data-testid="photo-input"
        @change="onChoose"
      />
      <oc-button
        v-if="contact['groupware.hasPhoto']"
        appearance="raw"
        size="small"
        data-testid="photo-remove"
        @click="remove"
      >
        {{ $gettext('Remove photo') }}
      </oc-button>
    </div>
    <p v-if="error" class="gw-photo-error" role="alert" data-testid="photo-error">
      {{ error }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact } from '../api/types'
import { useApi } from '../composables/useApi'
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
const input = ref<HTMLInputElement | null>(null)

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

<style>
.gw-contact-photo {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--oc-space-xsmall);
}
.gw-photo-image {
  width: 80px;
  height: 80px;
  border-radius: 50%;
  object-fit: cover;
}
.gw-photo-actions {
  gap: var(--oc-space-small);
  margin-bottom: var(--oc-space-small);
}
.gw-photo-error {
  margin: 0;
  font-size: var(--oc-font-size-small);
  color: var(--oc-color-swatch-danger-default);
}
</style>
