<template>
  <form class="contact-form" data-testid="contact-form" @submit.prevent="submit">
    <h2 class="oc-h3">{{ contact ? $gettext('Edit contact') : $gettext('New contact') }}</h2>

    <div v-if="!contact && folders.length > 1" class="oc-mb-m">
      <label for="cf-folder" class="oc-text-bold">{{ $gettext('Address book') }}</label>
      <select
        id="cf-folder"
        v-model="folderId"
        class="oc-select oc-width-1-1"
        data-testid="form-folder"
      >
        <option v-for="f in folders" :key="f.id" :value="f.id">{{ f.displayName }}</option>
      </select>
    </div>

    <div class="field-grid">
      <template v-for="f in mainFields" :key="f.key">
        <div>
          <label :for="`cf-${f.key}`">{{ f.label }}</label>
          <input
            :id="`cf-${f.key}`"
            v-model="draft[f.key]"
            class="oc-text-input oc-width-1-1"
            :data-testid="`field-${f.key}`"
          />
        </div>
      </template>
    </div>

    <row-list v-model="draft.emails" name="emails" :label="$gettext('Email')" type="email" />
    <div class="field-grid">
      <div>
        <label for="cf-mobilePhone">{{ $gettext('Mobile phone') }}</label>
        <input
          id="cf-mobilePhone"
          v-model="draft.mobilePhone"
          type="tel"
          class="oc-text-input oc-width-1-1"
          data-testid="field-mobilePhone"
        />
      </div>
    </div>
    <row-list
      v-model="draft.businessPhones"
      name="businessPhones"
      :label="$gettext('Work phone')"
      type="tel"
    />
    <row-list
      v-model="draft.homePhones"
      name="homePhones"
      :label="$gettext('Home phone')"
      type="tel"
    />
    <row-list v-model="draft.imAddresses" name="imAddresses" :label="$gettext('Messaging')" />

    <fieldset
      v-for="a in addresses"
      :key="a.key"
      class="address"
      :data-testid="`address-${a.key}`"
    >
      <legend class="oc-text-bold">{{ a.label }}</legend>
      <div class="field-grid">
        <div v-for="p in addressParts" :key="p.key">
          <label :for="`cf-${a.key}-${p.key}`">{{ p.label }}</label>
          <input
            :id="`cf-${a.key}-${p.key}`"
            v-model="draft[a.key][p.key]"
            class="oc-text-input oc-width-1-1"
            :data-testid="`${a.key}-${p.key}`"
          />
        </div>
      </div>
    </fieldset>

    <div class="field-grid">
      <div>
        <label for="cf-birthday">{{ $gettext('Birthday') }}</label>
        <input
          id="cf-birthday"
          v-model="draft.birthday"
          type="date"
          class="oc-text-input oc-width-1-1"
          data-testid="field-birthday"
        />
        <p v-if="yearless" class="oc-text-muted oc-text-small">
          {{ $gettext('This contact has a birthday without a year. Pick a date to replace it.') }}
        </p>
      </div>
      <div v-for="f in moreFields" :key="f.key">
        <label :for="`cf-${f.key}`">{{ f.label }}</label>
        <input
          :id="`cf-${f.key}`"
          v-model="draft[f.key]"
          class="oc-text-input oc-width-1-1"
          :data-testid="`field-${f.key}`"
        />
      </div>
    </div>
    <row-list v-model="draft.children" name="children" :label="$gettext('Children')" />

    <label for="cf-personalNotes">{{ $gettext('Notes') }}</label>
    <textarea
      id="cf-personalNotes"
      v-model="draft.personalNotes"
      rows="4"
      class="oc-textarea oc-width-1-1"
      data-testid="field-personalNotes"
    />

    <p v-if="!contact && !changed" class="oc-text-muted" data-testid="form-hint">
      {{ $gettext('Fill in at least one field.') }}
    </p>
    <div class="oc-mt-m">
      <button
        type="submit"
        class="oc-button oc-button-m oc-button-primary oc-mr-s"
        :disabled="!changed"
        data-testid="form-save"
      >
        {{ $gettext('Save') }}
      </button>
      <button
        type="button"
        class="oc-button oc-button-m oc-button-passive"
        data-testid="form-cancel"
        @click="emit('cancel')"
      >
        {{ $gettext('Cancel') }}
      </button>
    </div>
  </form>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { ContactInput, GraphContact, GraphContactFolder, PhysicalAddress } from '../api/types'
import RowList from './RowList.vue'
import { buildInput, emptyDraft, toDraft, type Draft } from './form'

defineOptions({ name: 'ContactForm' })

const props = defineProps<{
  // The contact to edit; none makes the form create one.
  contact?: GraphContact
  // The address books a new contact can go into.
  folders: GraphContactFolder[]
}>()
const emit = defineEmits<{
  save: [input: ContactInput, folderId: string]
  cancel: []
}>()
const { $gettext } = useGettext()

const original = props.contact ? toDraft(props.contact) : emptyDraft()
const draft = reactive<Draft>(structuredClone(original))
const folderId = ref(props.contact?.parentFolderId ?? props.folders[0]?.id ?? '')

const input = computed(() => buildInput(original, draft))
const changed = computed(() => Object.keys(input.value).length > 0)
const yearless = computed(
  () => !!props.contact?.birthday && props.contact.birthday.startsWith('0001')
)

type TextKey = {
  [K in keyof Draft]: Draft[K] extends string ? K : never
}[keyof Draft]

const mainFields: { key: TextKey; label: string }[] = [
  { key: 'title', label: $gettext('Title') },
  { key: 'givenName', label: $gettext('First name') },
  { key: 'middleName', label: $gettext('Middle name') },
  { key: 'surname', label: $gettext('Surname') },
  { key: 'generation', label: $gettext('Suffix') },
  { key: 'displayName', label: $gettext('Display name') },
  { key: 'nickName', label: $gettext('Nickname') },
  { key: 'companyName', label: $gettext('Company') },
  { key: 'jobTitle', label: $gettext('Job title') },
  { key: 'department', label: $gettext('Department') }
]

const moreFields: { key: TextKey; label: string }[] = [
  { key: 'businessHomePage', label: $gettext('Website') },
  { key: 'profession', label: $gettext('Profession') },
  { key: 'assistantName', label: $gettext('Assistant') },
  { key: 'manager', label: $gettext('Manager') },
  { key: 'spouseName', label: $gettext('Spouse') },
  { key: 'yomiGivenName', label: $gettext('Phonetic first name') },
  { key: 'yomiSurname', label: $gettext('Phonetic surname') },
  { key: 'yomiCompany', label: $gettext('Phonetic company') }
]

const addresses = [
  { key: 'homeAddress', label: $gettext('Home address') },
  { key: 'businessAddress', label: $gettext('Work address') },
  { key: 'otherAddress', label: $gettext('Other address') }
] as const

const addressParts: { key: keyof PhysicalAddress; label: string }[] = [
  { key: 'street', label: $gettext('Street') },
  { key: 'postalCode', label: $gettext('Postal code') },
  { key: 'city', label: $gettext('City') },
  { key: 'state', label: $gettext('State') },
  { key: 'countryOrRegion', label: $gettext('Country or region') }
]

function submit() {
  if (changed.value) emit('save', input.value, folderId.value)
}
</script>

<style scoped>
.field-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
  gap: var(--oc-space-small) var(--oc-space-medium);
  margin-bottom: var(--oc-space-medium);
}
.address {
  border: 0;
  padding: 0;
  margin: 0;
}
label {
  display: block;
}
</style>
