<template>
  <article class="gw-contact-detail" data-testid="contact-detail">
    <div class="gw-contact-head">
      <contact-photo
        :contact="contact"
        :name="name"
        :can-edit="canEdit"
        @changed="emit('photoChanged')"
      />
      <h3 class="gw-contact-name">{{ name }}</h3>
      <p v-if="subtitle" class="oc-text-muted oc-m-rm" data-testid="contact-subtitle">
        {{ subtitle }}
      </p>
      <div v-if="canEdit" class="gw-contact-actions oc-flex oc-flex-center" data-testid="contact-actions">
        <oc-button
          variation="primary"
          appearance="filled"
          size="small"
          data-testid="contact-edit"
          @click="emit('edit')"
        >
          <oc-icon name="pencil" fill-type="line" />
          {{ $gettext('Edit') }}
        </oc-button>
        <oc-button
          variation="danger"
          appearance="outline"
          size="small"
          data-testid="contact-delete"
          @click="emit('delete')"
        >
          <oc-icon name="delete-bin" fill-type="line" />
          {{ $gettext('Delete') }}
        </oc-button>
      </div>
    </div>

    <dl v-if="rows.length > 0" class="gw-contact-fields">
      <template v-for="row in rows" :key="row.label">
        <dt>{{ row.label }}</dt>
        <dd>
          <div v-for="(line, i) in row.lines" :key="i" class="gw-contact-line">
            <a v-if="line.href" :href="line.href">{{ line.text }}</a>
            <template v-else>{{ line.text }}</template>
          </div>
        </dd>
      </template>
    </dl>

    <contact-groups
      :contact="contact"
      :can-edit="canEdit"
      :suggestions="suggestions"
      @change="(categories) => emit('setCategories', categories)"
    />
    <div v-if="contact['groupware.groups'].length" class="gw-contact-section">
      <h4 class="gw-section-label">{{ $gettext('Member of') }}</h4>
      <ul class="oc-list oc-m-rm">
        <li v-for="g in contact['groupware.groups']" :key="g.id" data-testid="detail-membership">
          {{ g.displayName }}
        </li>
      </ul>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact, PhysicalAddress } from '../api/types'
import ContactGroups from './ContactGroups.vue'
import ContactPhoto from './ContactPhoto.vue'
import { contactName } from './model'

defineOptions({ name: 'ContactDetail' })

// What the details panel of the sidebar shows for a contact: the photo and
// name, the actions for someone who may change it, the fields that have a
// value and the groups. Values that can be opened are links.
const props = defineProps<{
  contact: GraphContact
  canEdit: boolean
  // The names of the groups that exist, offered when adding one.
  suggestions: string[]
}>()
const emit = defineEmits<{
  edit: []
  delete: []
  photoChanged: []
  setCategories: [categories: string[]]
}>()
const { $gettext } = useGettext()

const name = computed(() => contactName(props.contact) || $gettext('Unnamed contact'))
const subtitle = computed(() =>
  [props.contact.jobTitle, props.contact.companyName].filter(Boolean).join(' · ')
)

function address(a: PhysicalAddress): string {
  return [a.street, [a.postalCode, a.city].filter(Boolean).join(' '), a.state, a.countryOrRegion]
    .filter(Boolean)
    .join(', ')
}

interface Line {
  text: string
  href?: string
}
interface Row {
  label: string
  lines: Line[]
}

const plain = (values: string[]): Line[] => values.map((text) => ({ text }))
const phones = (values: string[]): Line[] =>
  values.map((text) => ({ text, href: `tel:${text.replace(/[^\d+]/g, '')}` }))

// An address of a website needs a scheme to be a link; one that is typed
// without it gets https.
function site(value: string): Line[] {
  if (!value) return []
  const href = /^[a-z][a-z\d+.-]*:/i.test(value) ? value : `https://${value}`
  return [{ text: value, href: /^https?:/i.test(href) ? href : undefined }]
}

const rows = computed<Row[]>(() => {
  const c = props.contact
  const all: Row[] = [
    {
      label: $gettext('Email'),
      lines: c.emailAddresses.map((e) => ({ text: e.address, href: `mailto:${e.address}` }))
    },
    { label: $gettext('Mobile'), lines: phones([c.mobilePhone]) },
    { label: $gettext('Work phone'), lines: phones(c.businessPhones) },
    { label: $gettext('Home phone'), lines: phones(c.homePhones) },
    { label: $gettext('Home address'), lines: plain([address(c.homeAddress)]) },
    { label: $gettext('Work address'), lines: plain([address(c.businessAddress)]) },
    { label: $gettext('Other address'), lines: plain([address(c.otherAddress)]) },
    { label: $gettext('Website'), lines: site(c.businessHomePage) },
    { label: $gettext('Messaging'), lines: plain(c.imAddresses) },
    { label: $gettext('Birthday'), lines: plain([c.birthday ? birthday(c.birthday) : '']) },
    { label: $gettext('Nickname'), lines: plain([c.nickName]) },
    { label: $gettext('Department'), lines: plain([c.department]) },
    { label: $gettext('Spouse'), lines: plain([c.spouseName]) },
    { label: $gettext('Children'), lines: plain(c.children) },
    { label: $gettext('Notes'), lines: plain(c.personalNotes ? c.personalNotes.split('\n') : []) }
  ]
  return all
    .map((r) => ({ ...r, lines: r.lines.filter((l) => l.text) }))
    .filter((r) => r.lines.length > 0)
})

// birthday writes the date the server sent in the user's language. Cards
// without a year come back with year 1, which is shown as day and month only.
function birthday(value: string): string {
  const [year, month, day] = value.slice(0, 10).split('-').map(Number)
  if (!year || !month || !day) return ''
  // A date made in UTC keeps its day in every time zone.
  const date = new Date(Date.UTC(year === 1 ? 2000 : year, month - 1, day))
  return new Intl.DateTimeFormat(undefined, {
    day: 'numeric',
    month: 'long',
    ...(year === 1 ? {} : { year: 'numeric' }),
    timeZone: 'UTC'
  }).format(date)
}
</script>

<style>
.gw-contact-detail {
  display: flex;
  flex-direction: column;
  gap: var(--oc-space-medium);
}
.gw-contact-head {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--oc-space-xsmall);
  text-align: center;
}
.gw-contact-name {
  margin: 0;
  font-size: var(--oc-font-size-large);
  font-weight: var(--oc-font-weight-semibold);
  overflow-wrap: anywhere;
}
.gw-contact-actions {
  gap: var(--oc-space-small);
  margin-top: var(--oc-space-small);
}
.gw-contact-fields {
  display: grid;
  grid-template-columns: 7rem 1fr;
  gap: var(--oc-space-small) var(--oc-space-medium);
  margin: 0;
  padding: var(--oc-space-medium) 0;
  border-top: 1px solid var(--oc-color-border);
  border-bottom: 1px solid var(--oc-color-border);
}
.gw-contact-fields dt {
  color: var(--oc-color-text-muted);
}
.gw-contact-fields dd {
  margin: 0;
  min-width: 0;
}
.gw-contact-line {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.gw-section-label {
  margin: 0 0 var(--oc-space-xsmall);
  font-size: var(--oc-font-size-default);
  font-weight: normal;
  color: var(--oc-color-text-muted);
}
</style>
