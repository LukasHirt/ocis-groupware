<template>
  <article class="contact-detail" data-testid="contact-detail">
    <h2 class="oc-h3">{{ name || $gettext('Unnamed contact') }}</h2>
    <p v-if="subtitle" class="oc-text-muted">{{ subtitle }}</p>
    <dl class="contact-fields">
      <template v-for="row in rows" :key="row.label">
        <dt class="oc-text-muted">{{ row.label }}</dt>
        <dd>
          <div v-for="(line, i) in row.lines" :key="i" class="contact-line">{{ line }}</div>
        </dd>
      </template>
    </dl>
    <div v-if="contact['groupware.groups'].length" class="oc-mt-m">
      <span class="oc-text-muted">{{ $gettext('Member of') }}</span>
      <span
        v-for="g in contact['groupware.groups']"
        :key="g.id"
        class="oc-ml-s"
        data-testid="detail-membership"
        >{{ g.displayName }}</span
      >
    </div>
    <slot />
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { GraphContact, PhysicalAddress } from '../api/types'
import { contactName } from './model'

defineOptions({ name: 'ContactDetail' })

const props = defineProps<{ contact: GraphContact }>()
const { $gettext } = useGettext()

const name = computed(() => contactName(props.contact))
const subtitle = computed(() =>
  [props.contact.jobTitle, props.contact.companyName].filter(Boolean).join(', ')
)

function address(a: PhysicalAddress): string {
  return [
    a.street,
    [a.postalCode, a.city].filter(Boolean).join(' '),
    a.state,
    a.countryOrRegion
  ]
    .filter(Boolean)
    .join(', ')
}

interface Row {
  label: string
  lines: string[]
}

const rows = computed<Row[]>(() => {
  const c = props.contact
  const all: Row[] = [
    {
      label: $gettext('Email'),
      lines: c.emailAddresses.map((e) => e.address)
    },
    {
      label: $gettext('Phone'),
      lines: [c.mobilePhone, ...c.businessPhones, ...c.homePhones].filter(Boolean)
    },
    { label: $gettext('Home address'), lines: [address(c.homeAddress)] },
    { label: $gettext('Work address'), lines: [address(c.businessAddress)] },
    { label: $gettext('Other address'), lines: [address(c.otherAddress)] },
    { label: $gettext('Website'), lines: [c.businessHomePage] },
    { label: $gettext('Messaging'), lines: c.imAddresses },
    { label: $gettext('Birthday'), lines: [c.birthday ? birthday(c.birthday) : ''] },
    { label: $gettext('Nickname'), lines: [c.nickName] },
    { label: $gettext('Department'), lines: [c.department] },
    { label: $gettext('Spouse'), lines: [c.spouseName] },
    { label: $gettext('Children'), lines: c.children },
    { label: $gettext('Categories'), lines: c.categories },
    { label: $gettext('Notes'), lines: c.personalNotes ? c.personalNotes.split('\n') : [] }
  ]
  return all
    .map((r) => ({ ...r, lines: r.lines.filter(Boolean) }))
    .filter((r) => r.lines.length > 0)
})

// birthday shows the date the server sent. Cards without a year come back
// with year 1, which is shown as day and month only.
function birthday(value: string): string {
  const [year, month, day] = value.slice(0, 10).split('-')
  return year === '0001' ? `${day}.${month}.` : `${day}.${month}.${year}`
}
</script>

<style scoped>
.contact-fields {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: var(--oc-space-small) var(--oc-space-medium);
}
.contact-fields dd {
  margin: 0;
}
.contact-line {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
