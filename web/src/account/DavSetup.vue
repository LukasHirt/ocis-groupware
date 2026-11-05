<template>
  <section aria-labelledby="groupware-dav-title" data-testid="dav-account">
    <h2 id="groupware-dav-title" class="oc-h3">{{ $gettext('Connect a calendar or contacts app') }}</h2>
    <p>
      {{
        $gettext(
          'Calendar and contact apps sign in with your username and an app password, not with your regular password.'
        )
      }}
    </p>
    <p
      v-if="!account.username && account.username_conflict"
      class="oc-text-muted"
      data-testid="dav-username-conflict"
    >
      {{ $gettext('Your user name is in use by another account. Ask your administrator.') }}
    </p>
    <p v-else-if="!account.username" class="oc-text-muted" data-testid="dav-no-username">
      {{
        $gettext(
          'Your DAV username is not available. If another user has taken over your name, ask an administrator.'
        )
      }}
    </p>
    <template v-else>
      <copy-field id="dav-username" :label="$gettext('Username')" :value="account.username" />
    </template>
    <p v-if="validUntil" class="oc-text-muted" data-testid="dav-valid-until">
      {{
        $gettext(
          'Your app passwords work until %{date}, unless you open oCIS Web again. Opening it renews them.',
          { date: validUntil.toLocaleDateString() }
        )
      }}
    </p>
    <p v-if="validUntil && daysLeft < 7" class="oc-text-danger" role="alert" data-testid="dav-valid-until-warning">
      {{
        daysLeft <= 0
          ? $gettext('Your app passwords have stopped working. Open oCIS Web to renew them.')
          : $gettext('Your app passwords stop working soon. Open oCIS Web to renew them.')
      }}
    </p>
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

    <div class="oc-mt-m" data-testid="dav-setup">
      <h3 class="oc-h4">{{ $gettext('Setup hints') }}</h3>
      <dl class="groupware-hints">
        <dt>Thunderbird</dt>
        <dd>
          {{
            $gettext(
              'Calendar: New Calendar, On the Network, CalDAV, and paste the calendar home. Contacts: New CardDAV Address Book, then enter your username and the server address. Use an app password when it asks for a password.'
            )
          }}
        </dd>
        <dt>Apple (macOS and iOS)</dt>
        <dd>
          {{
            $gettext(
              'Add an account under Internet Accounts or Settings, choose Other, then CalDAV or CardDAV account with manual setup. Enter your username, an app password and the server address.'
            )
          }}
        </dd>
        <dt>DAVx5 (Android)</dt>
        <dd>
          {{
            $gettext(
              'Add an account, choose Login with URL and user name, and enter the server address, your username and an app password. DAVx5 then finds both your calendars and your address books.'
            )
          }}
        </dd>
      </dl>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Account } from '../api/types'
import CopyField from './CopyField.vue'

defineOptions({ name: 'DavSetup' })

const props = defineProps<{ account: Account }>()

const validUntil = computed(() =>
  props.account.app_passwords_valid_until ? new Date(props.account.app_passwords_valid_until) : null
)
const daysLeft = computed(() =>
  validUntil.value ? (validUntil.value.getTime() - Date.now()) / 86_400_000 : Infinity
)
</script>

<style>
.groupware-hints dt {
  font-weight: bold;
  margin-top: 0.5rem;
}
.groupware-hints dd {
  margin-left: 0;
}
</style>
