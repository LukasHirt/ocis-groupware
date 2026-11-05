<template>
  <app-shell :title="$gettext('Devices')" :loading="loading">
    <div class="groupware-account oc-mt-m">
      <dav-setup v-if="account" :account="account" />

      <section class="oc-mt-l" aria-labelledby="groupware-app-passwords-title">
        <div class="oc-flex oc-flex-middle oc-flex-between">
          <h2 id="groupware-app-passwords-title" class="oc-h3 oc-m-rm">
            {{ $gettext('App passwords') }}
          </h2>
          <button
            type="button"
            class="oc-button oc-button-m oc-button-primary oc-button-primary-filled"
            data-testid="app-password-add"
            @click="creating = true"
          >
            {{ $gettext('New app password') }}
          </button>
        </div>
        <p class="oc-text-muted">
          {{
            $gettext('Use one app password for each device, so you can revoke a single device without touching the others.')
          }}
        </p>
        <p v-if="passwords.length === 0" class="oc-text-muted" data-testid="app-password-none">
          {{ $gettext('You have no app passwords yet.') }}
        </p>
        <ul v-else class="oc-list oc-list-divider" :aria-label="$gettext('App passwords')">
          <li
            v-for="p in passwords"
            :key="p.id"
            class="oc-flex oc-flex-middle oc-flex-between oc-py-s"
            data-testid="app-password-item"
          >
            <div>
              <span class="oc-text-bold">{{ p.name }}</span>
              <span
                v-if="isExpired(p)"
                class="oc-ml-s oc-text-danger"
                data-testid="app-password-expired"
              >
                {{ $gettext('Expired') }}
              </span>
              <div class="oc-text-muted oc-text-small">
                {{ $gettext('Created %{date}', { date: day(p.created_at) }) }},
                {{
                  isExpired(p)
                    ? $gettext('expired %{date}', { date: day(p.expires_at) })
                    : $gettext('expires %{date}', { date: day(p.expires_at) })
                }},
                {{
                  p.last_used_at
                    ? $gettext('Last used %{date}', { date: day(p.last_used_at) })
                    : $gettext('Never used')
                }}
              </div>
            </div>
            <button
              type="button"
              class="oc-button oc-button-s oc-button-passive"
              :aria-label="$gettext('Revoke %{name}', { name: p.name })"
              data-testid="app-password-revoke"
              @click="revoking = p"
            >
              {{ $gettext('Revoke') }}
            </button>
          </li>
        </ul>
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
import type { Account, AppPassword } from '../api/types'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import AppShell from '../components/AppShell.vue'
import { useApi } from '../composables/useApi'
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

const day = (iso: string) => new Date(iso).toLocaleDateString()
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
