import { watch } from 'vue'
import { useAuthStore } from '@ownclouders/web-pkg'
import { useApi } from './useApi'

// useLeaseRenewal asks the service about the signed-in user once per page
// load, as soon as oCIS Web has the user context. The service confirms the
// user with oCIS on that request, which renews the time its app passwords
// stay valid. It runs from the application's setup, so a user who only works
// in other apps of oCIS Web keeps their devices signed in too. Public link
// contexts have no user and are skipped. A failure is ignored: the next page
// load tries again.
export function useLeaseRenewal() {
  const { client } = useApi()
  const auth = useAuthStore()
  let done = false

  watch(
    () => auth.userContextReady && !auth.publicLinkContextReady,
    (ready) => {
      if (!ready || done) return
      done = true
      client.getAccount().catch(() => {})
    },
    { immediate: true }
  )
}
