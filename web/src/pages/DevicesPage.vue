<template>
  <app-shell :title="$gettext('Devices')" :loading="loading">
    <div class="gw-devices oc-mt-m">
      <dav-setup v-if="account" :account="account" />

      <section class="oc-mt-xl" aria-labelledby="groupware-app-passwords-title">
        <div class="gw-section-head">
          <h2 id="groupware-app-passwords-title" class="gw-section-title">
            {{ $gettext('App passwords') }}
          </h2>
          <oc-button
            variation="primary"
            appearance="filled"
            data-testid="app-password-add"
            @click="creating = true"
          >
            <oc-icon name="add" size="small" />
            {{ $gettext('New app password') }}
          </oc-button>
        </div>
        <p class="oc-text-muted oc-mt-s">
          {{
            $gettext(
              'Use one app password for each device, so you can revoke a single device without touching the others.'
            )
          }}
        </p>
        <div v-if="passwords.length === 0" data-testid="app-password-none">
          <no-content-message icon="lock" class="oc-my-l">
            <template #message>{{ $gettext('You have no app passwords yet') }}</template>
          </no-content-message>
        </div>
        <oc-table-simple v-else class="gw-passwords" :aria-label="$gettext('App passwords')">
          <oc-thead>
            <oc-tr>
              <oc-th>{{ $gettext('Name') }}</oc-th>
              <oc-th class="gw-col-detail">{{ $gettext('Created') }}</oc-th>
              <oc-th class="gw-col-detail">{{ $gettext('Expires') }}</oc-th>
              <oc-th class="gw-col-detail">{{ $gettext('Last used') }}</oc-th>
              <oc-th class="gw-col-action" width="shrink">
                <span class="oc-invisible-sr">{{ $gettext('Actions') }}</span>
              </oc-th>
            </oc-tr>
          </oc-thead>
          <oc-tbody>
            <oc-tr v-for="p in passwords" :key="p.id" data-testid="app-password-item">
              <oc-td>
                <span class="oc-text-bold">{{ p.name }}</span>
                <oc-tag
                  v-if="isExpired(p)"
                  size="small"
                  class="gw-tag-danger oc-ml-s"
                  data-testid="app-password-expired"
                >
                  {{ $gettext('Expired') }}
                </oc-tag>
                <div class="gw-dates oc-text-muted oc-text-small">
                  {{ $gettext('Created %{date}', { date: formatDay(p.created_at) }) }},
                  {{
                    isExpired(p)
                      ? $gettext('expired %{date}', { date: formatDay(p.expires_at) })
                      : $gettext('expires %{date}', { date: formatDay(p.expires_at) })
                  }},
                  {{ lastUsed(p) }}
                </div>
              </oc-td>
              <oc-td class="gw-col-detail">{{ formatDay(p.created_at) }}</oc-td>
              <oc-td class="gw-col-detail">{{ formatDay(p.expires_at) }}</oc-td>
              <oc-td class="gw-col-detail">{{ lastUsed(p) }}</oc-td>
              <oc-td class="gw-col-action" width="shrink" align-h="right">
                <oc-button
                  variation="danger"
                  appearance="raw"
                  size="small"
                  :aria-label="$gettext('Revoke %{name}', { name: p.name })"
                  data-testid="app-password-revoke"
                  @click="revoking = p"
                >
                  {{ $gettext('Revoke') }}
                </oc-button>
              </oc-td>
            </oc-tr>
          </oc-tbody>
        </oc-table-simple>
      </section>
    </div>

    <new-app-password-dialog
      v-if="creating"
      :max-lifetime-days="maxLifetimeDays"
      @cancel="creating = false"
      @done="created"
    />
    <confirm-dialog
      v-if="revoking"
      :title="$gettext('Revoke app password')"
      :message="
        $gettext('Devices that use %{name} will no longer be able to sign in. This cannot be undone.', {
          name: revoking.name
        })
      "
      :confirm-label="$gettext('Revoke')"
      icon="error-warning"
      @confirm="revoke"
      @cancel="revoking = null"
    />
  </app-shell>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useGettext } from 'vue3-gettext'
import { NoContentMessage } from '@ownclouders/web-pkg'
import type { Account, AppPassword } from '../api/types'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import AppShell from '../components/AppShell.vue'
import { useApi } from '../composables/useApi'
import { formatDay } from '../account/format'
import DavSetup from '../account/DavSetup.vue'
import NewAppPasswordDialog from '../account/NewAppPasswordDialog.vue'

defineOptions({ name: 'DevicesPage' })

const { $gettext } = useGettext()
const { run } = useApi()

const loading = ref(true)
const account = ref<Account>()
const passwords = ref<AppPassword[]>([])
// The longest lifetime the server accepts, until the list says otherwise.
const maxLifetimeDays = ref(365)
const creating = ref(false)
const revoking = ref<AppPassword | null>(null)

const lastUsed = (p: AppPassword) => (p.last_used_at ? formatDay(p.last_used_at) : $gettext('Never used'))
const isExpired = (p: AppPassword) => new Date(p.expires_at).getTime() <= Date.now()

async function loadPasswords() {
  const list = await run($gettext('Could not load the app passwords'), (c) => c.listAppPasswords())
  if (list) {
    passwords.value = list.appPasswords
    maxLifetimeDays.value = list.maxLifetimeDays
  }
}

async function created() {
  creating.value = false
  await loadPasswords()
}

async function revoke() {
  const target = revoking.value
  revoking.value = null
  if (!target) {
    return
  }
  await run($gettext('Could not revoke the app password'), (c) => c.revokeAppPassword(target.id))
  await loadPasswords()
}

onMounted(async () => {
  await Promise.all([
    run($gettext('Could not load your account'), (c) => c.getAccount()).then((a) => (account.value = a)),
    loadPasswords()
  ])
  loading.value = false
})
</script>

<style>
.gw-devices {
  /* The page body already has the side padding of the app bar, so the content
     starts under the heading. */
  width: 100%;
  max-width: 60rem;
  min-width: 0;
  padding-bottom: var(--oc-space-xlarge);
}
.gw-section-title {
  font-size: var(--oc-font-size-large);
  font-weight: 600;
  margin: 0;
  padding-bottom: var(--oc-space-small);
  border-bottom: 1px solid var(--oc-color-border);
}
.gw-subsection-title {
  font-size: var(--oc-font-size-medium);
  font-weight: 600;
  margin: 0 0 var(--oc-space-small);
}
.gw-section-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--oc-space-small) var(--oc-space-medium);
  border-bottom: 1px solid var(--oc-color-border);
}
.gw-section-head .gw-section-title {
  border-bottom: 0;
}
.gw-passwords {
  width: 100%;
}
.gw-passwords th,
.gw-passwords td,
.gw-connection th,
.gw-connection td {
  padding: var(--oc-space-small);
  vertical-align: middle;
}
.gw-passwords th {
  color: var(--oc-color-text-muted);
  font-weight: normal;
  text-align: left;
}
.gw-passwords tbody tr,
.gw-connection tr {
  border-bottom: 1px solid var(--oc-color-border);
}
.gw-passwords tbody tr:last-child,
.gw-connection tr:last-child {
  border-bottom: 0;
}
.gw-tag-danger {
  background: var(--oc-color-swatch-danger-muted);
  color: var(--oc-color-swatch-danger-default);
}
/* On a narrow screen the dates move under the name. */
.gw-dates {
  display: none;
}
@media (max-width: 639px) {
  .gw-col-detail {
    display: none;
  }
  .gw-dates {
    display: block;
  }
}
</style>
