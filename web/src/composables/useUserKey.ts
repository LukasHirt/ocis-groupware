import { useUserStore } from '@ownclouders/web-pkg'

// useUserKey is the id of the signed-in user, which keeps browser settings of
// different users on one machine apart. It is empty when the host's store is
// not there, for example in a unit test.
export function useUserKey(): string {
  try {
    return useUserStore().user?.id ?? ''
  } catch {
    return ''
  }
}
