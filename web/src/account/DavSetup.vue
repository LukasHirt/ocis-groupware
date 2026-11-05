<template>
  <section aria-labelledby="groupware-dav-title" data-testid="dav-account">
    <h2 id="groupware-dav-title" class="gw-section-title">
      {{ $gettext('Connect a calendar or contacts app') }}
    </h2>
    <p class="oc-text-muted oc-mt-s">
      {{
        $gettext(
          'Calendar and contact apps sign in with your username and an app password, not with your regular password.'
        )
      }}
    </p>
    <inline-notice
      v-if="!account.username && account.username_conflict"
      variation="warning"
      data-testid="dav-username-conflict"
    >
      {{ $gettext('Your user name is in use by another account. Ask your administrator.') }}
    </inline-notice>
    <inline-notice v-else-if="!account.username" variation="warning" data-testid="dav-no-username">
      {{
        $gettext(
          'Your DAV username is not available. If another user has taken over your name, ask an administrator.'
        )
      }}
    </inline-notice>
    <oc-table-simple class="gw-connection" :aria-label="$gettext('Connection details')">
      <oc-tbody>
        <copy-field
          v-if="account.username"
          id="dav-username"
          :label="$gettext('Username')"
          :value="account.username"
        />
        <copy-field id="dav-url" :label="$gettext('Server address')" :value="account.dav.url" />
        <template v-if="account.username">
          <copy-field
            id="dav-calendar"
            :label="$gettext('Calendar home')"
            :value="account.dav.calendarHomeUrl"
          />
          <copy-field
            id="dav-addressbook"
            :label="$gettext('Address book home')"
            :value="account.dav.addressBookHomeUrl"
          />
        </template>
      </oc-tbody>
    </oc-table-simple>
    <inline-notice v-if="validUntil" class="oc-mt-s" data-testid="dav-valid-until">
      {{
        $gettext(
          'Your app passwords work until %{date}, unless you open oCIS Web again. Opening it renews them.',
          { date: formatDay(validUntil) }
        )
      }}
    </inline-notice>
    <inline-notice
      v-if="validUntil && daysLeft < 7"
      variation="warning"
      class="oc-mt-s"
      data-testid="dav-valid-until-warning"
    >
      {{
        daysLeft <= 0
          ? $gettext('Your app passwords have stopped working. Open oCIS Web to renew them.')
          : $gettext('Your app passwords stop working soon. Open oCIS Web to renew them.')
      }}
    </inline-notice>

    <div class="oc-mt-l" data-testid="dav-setup">
      <h3 class="gw-subsection-title">{{ $gettext('Set up a device') }}</h3>
      <details v-for="hint in hints" :key="hint.name" class="gw-hint">
        <summary>{{ hint.name }}</summary>
        <p class="oc-text-muted">{{ hint.text }}</p>
      </details>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import type { Account } from '../api/types'
import InlineNotice from '../components/InlineNotice.vue'
import CopyField from './CopyField.vue'
import { formatDay } from './format'

defineOptions({ name: 'DavSetup' })

const props = defineProps<{ account: Account }>()

const { $gettext } = useGettext()

const validUntil = computed(() =>
  props.account.app_passwords_valid_until ? new Date(props.account.app_passwords_valid_until) : null
)
const daysLeft = computed(() =>
  validUntil.value ? (validUntil.value.getTime() - Date.now()) / 86_400_000 : Infinity
)

const hints = computed(() => [
  {
    name: 'Thunderbird',
    text: $gettext(
      'Calendar: New Calendar, On the Network, CalDAV, and paste the calendar home. Contacts: New CardDAV Address Book, then enter your username and the server address. Use an app password when it asks for a password.'
    )
  },
  {
    name: 'Apple (macOS and iOS)',
    text: $gettext(
      'Add an account under Internet Accounts or Settings, choose Other, then CalDAV or CardDAV account with manual setup. Enter your username, an app password and the server address.'
    )
  },
  {
    name: 'DAVx5 (Android)',
    text: $gettext(
      'Add an account, choose Login with URL and user name, and enter the server address, your username and an app password. DAVx5 then finds both your calendars and your address books.'
    )
  }
])
</script>

<style>
.gw-connection {
  width: 100%;
}
.gw-hint {
  border-bottom: 1px solid var(--oc-color-border);
  padding: var(--oc-space-small) 0;
}
.gw-hint summary {
  cursor: pointer;
  font-weight: 600;
  border-radius: var(--oc-space-xsmall);
}
.gw-hint summary:focus-visible {
  outline: 2px solid var(--oc-color-swatch-primary-default);
  outline-offset: 2px;
}
.gw-hint p {
  margin: var(--oc-space-small) 0 0;
}
</style>
