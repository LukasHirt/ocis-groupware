<template>
  <form class="gw-contact-form" data-testid="contact-form" novalidate @submit.prevent="submit">
    <oc-select
      v-if="!contact && folders.length > 1"
      id="gw-form-folder"
      v-model="folderId"
      class="oc-mb-m"
      :label="$gettext('Address book')"
      :options="folders"
      option-label="displayName"
      :reduce="(f: GraphContactFolder) => f.id"
      :clearable="false"
      :searchable="false"
      data-testid="form-folder"
    />

    <section class="gw-form-section">
      <h3 class="gw-form-heading">{{ $gettext('Name') }}</h3>
      <oc-text-input
        v-for="f in nameFields"
        :id="`gw-field-${f.key}`"
        :key="f.key"
        v-model="draft[f.key]"
        :label="f.label"
        :data-testid="`field-${f.key}`"
      />
      <oc-button
        appearance="raw"
        class="gw-form-toggle"
        :aria-expanded="moreName"
        data-testid="name-more"
        @click="moreName = !moreName"
      >
        <oc-icon :name="moreName ? 'arrow-down-s' : 'arrow-right-s'" fill-type="line" />
        {{ $gettext('More name fields') }}
      </oc-button>
      <template v-if="moreName">
        <oc-text-input
          v-for="f in moreNameFields"
          :id="`gw-field-${f.key}`"
          :key="f.key"
          v-model="draft[f.key]"
          :label="f.label"
          :data-testid="`field-${f.key}`"
        />
      </template>
    </section>

    <section class="gw-form-section">
      <h3 class="gw-form-heading">{{ $gettext('Work') }}</h3>
      <oc-text-input
        v-for="f in workFields"
        :id="`gw-field-${f.key}`"
        :key="f.key"
        v-model="draft[f.key]"
        :label="f.label"
        :data-testid="`field-${f.key}`"
      />
    </section>

    <section class="gw-form-section">
      <h3 class="gw-form-heading">{{ $gettext('Contact') }}</h3>
      <row-list
        v-model="draft.emails"
        name="emails"
        :label="$gettext('Email')"
        :add-label="$gettext('Add email')"
        type="email"
      />
      <oc-text-input
        id="gw-field-mobilePhone"
        v-model="draft.mobilePhone"
        type="tel"
        :label="$gettext('Mobile phone')"
        class="oc-mb-m"
        data-testid="field-mobilePhone"
      />
      <row-list
        v-model="draft.businessPhones"
        name="businessPhones"
        :label="$gettext('Work phone')"
        :add-label="$gettext('Add work phone')"
        type="tel"
      />
      <row-list
        v-model="draft.homePhones"
        name="homePhones"
        :label="$gettext('Home phone')"
        :add-label="$gettext('Add home phone')"
        type="tel"
      />
      <row-list
        v-model="draft.imAddresses"
        name="imAddresses"
        :label="$gettext('Messaging')"
        :add-label="$gettext('Add messaging address')"
      />
    </section>

    <section class="gw-form-section">
      <h3 class="gw-form-heading">{{ $gettext('Addresses') }}</h3>
      <div
        v-for="a in addresses"
        :key="a.key"
        class="gw-address"
        :data-testid="`address-${a.key}`"
      >
        <oc-button
          appearance="raw"
          class="gw-form-toggle"
          :aria-expanded="open[a.key]"
          :data-testid="`${a.key}-toggle`"
          @click="open[a.key] = !open[a.key]"
        >
          <oc-icon :name="open[a.key] ? 'arrow-down-s' : 'arrow-right-s'" fill-type="line" />
          {{ a.label }}
        </oc-button>
        <template v-if="open[a.key]">
          <oc-text-input
            v-for="p in addressParts"
            :id="`gw-${a.key}-${p.key}`"
            :key="p.key"
            v-model="draft[a.key][p.key]"
            :label="p.label"
            :data-testid="`${a.key}-${p.key}`"
          />
        </template>
      </div>
    </section>

    <section class="gw-form-section">
      <h3 class="gw-form-heading">{{ $gettext('More') }}</h3>
      <oc-text-input
        id="gw-field-birthday"
        v-model="draft.birthday"
        type="date"
        :label="$gettext('Birthday')"
        :description-message="
          yearless
            ? $gettext('This contact has a birthday without a year. Pick a date to replace it.')
            : ''
        "
        data-testid="field-birthday"
      />
      <oc-button
        appearance="raw"
        class="gw-form-toggle"
        :aria-expanded="moreDetails"
        data-testid="details-more"
        @click="moreDetails = !moreDetails"
      >
        <oc-icon :name="moreDetails ? 'arrow-down-s' : 'arrow-right-s'" fill-type="line" />
        {{ $gettext('More details') }}
      </oc-button>
      <template v-if="moreDetails">
        <oc-text-input
          v-for="f in moreFields"
          :id="`gw-field-${f.key}`"
          :key="f.key"
          v-model="draft[f.key]"
          :label="f.label"
          :data-testid="`field-${f.key}`"
        />
        <row-list
          v-model="draft.children"
          name="children"
          :label="$gettext('Children')"
          :add-label="$gettext('Add child')"
        />
      </template>
      <oc-textarea
        id="gw-field-personalNotes"
        v-model="draft.personalNotes"
        :label="$gettext('Notes')"
        rows="4"
        data-testid="field-personalNotes"
      />
    </section>

    <div class="gw-form-footer">
      <p v-if="!contact && !changed" class="oc-text-muted oc-text-small oc-mt-rm" data-testid="form-hint">
        {{ $gettext('Fill in at least one field.') }}
      </p>
      <div class="oc-flex oc-flex-right gw-form-actions">
        <oc-button
          variation="passive"
          appearance="outline"
          data-testid="form-cancel"
          @click="emit('cancel')"
        >
          {{ $gettext('Cancel') }}
        </oc-button>
        <oc-button
          variation="primary"
          appearance="filled"
          submit="submit"
          :disabled="!changed"
          data-testid="form-save"
        >
          {{ $gettext('Save') }}
        </oc-button>
      </div>
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

// A group of fields that is closed to begin with opens when the contact has
// something in it. The home address is open for a new contact.
const filled = (keys: readonly TextKey[]) => keys.some((k) => draft[k].trim() !== '')
const addressFilled = (a: PhysicalAddress) => Object.values(a).some((v) => v.trim() !== '')
const moreName = ref(filled(['middleName', 'generation', 'nickName']))
const moreDetails = ref(
  filled([
    'businessHomePage',
    'profession',
    'assistantName',
    'manager',
    'spouseName',
    'yomiGivenName',
    'yomiSurname',
    'yomiCompany'
  ]) || draft.children.some((c) => c.trim() !== '')
)
const open = reactive({
  homeAddress: !props.contact || addressFilled(draft.homeAddress),
  businessAddress: addressFilled(draft.businessAddress),
  otherAddress: addressFilled(draft.otherAddress)
})

const input = computed(() => buildInput(original, draft))
const changed = computed(() => Object.keys(input.value).length > 0)
const yearless = computed(
  () => !!props.contact?.birthday && props.contact.birthday.startsWith('0001')
)

type TextKey = {
  [K in keyof Draft]: Draft[K] extends string ? K : never
}[keyof Draft]

const nameFields: { key: TextKey; label: string }[] = [
  { key: 'title', label: $gettext('Title') },
  { key: 'givenName', label: $gettext('First name') },
  { key: 'surname', label: $gettext('Surname') },
  { key: 'displayName', label: $gettext('Display name') }
]

// Rarely used, so they stay behind a button unless the contact has one.
const moreNameFields: { key: TextKey; label: string }[] = [
  { key: 'middleName', label: $gettext('Middle name') },
  { key: 'generation', label: $gettext('Suffix') },
  { key: 'nickName', label: $gettext('Nickname') }
]

const workFields: { key: TextKey; label: string }[] = [
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

<style>
.gw-contact-form {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.gw-form-section {
  display: flex;
  flex-direction: column;
  gap: var(--oc-space-small);
  margin-bottom: var(--oc-space-medium);
}
.gw-form-heading {
  margin: 0;
  font-size: var(--oc-font-size-medium);
  font-weight: var(--oc-font-weight-semibold);
}
.gw-form-toggle {
  align-self: flex-start;
}
.gw-address {
  display: flex;
  flex-direction: column;
  gap: var(--oc-space-small);
}
.gw-contact-form input[type='date'] {
  color-scheme: light dark;
}
/* The buttons stay in view while a long form scrolls. */
.gw-form-footer {
  position: sticky;
  bottom: 0;
  background: var(--oc-color-background-default);
  padding: var(--oc-space-small) 0;
  border-top: 1px solid var(--oc-color-border);
}
.gw-form-actions {
  gap: var(--oc-space-small);
}
</style>
