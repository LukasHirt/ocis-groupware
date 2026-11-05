import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, reactive, type EffectScope } from 'vue'

const getAccount = vi.fn()
const auth = reactive({ userContextReady: false, publicLinkContextReady: false })

vi.mock('@ownclouders/web-pkg', () => ({
  useAuthStore: () => auth
}))
vi.mock('../../src/composables/useApi', () => ({
  useApi: () => ({ client: { getAccount } })
}))

import { useLeaseRenewal as useRenewal } from '../../src/composables/useLeaseRenewal'

// Each test runs the composable in a scope that is stopped afterwards, as a
// component's setup would be, so one test's watcher does not see the next
// test's changes.
let scope: EffectScope
const useLeaseRenewal = () => scope.run(useRenewal)

afterEach(() => scope.stop())

beforeEach(() => {
  scope = effectScope()
  getAccount.mockReset().mockResolvedValue({})
  auth.userContextReady = false
  auth.publicLinkContextReady = false
})

describe('useLeaseRenewal', () => {
  it('does nothing until the user context is ready', async () => {
    useLeaseRenewal()
    await nextTick()
    expect(getAccount).not.toHaveBeenCalled()
  })

  it('asks the service about the user once, when the context becomes ready', async () => {
    useLeaseRenewal()
    auth.userContextReady = true
    await nextTick()
    expect(getAccount).toHaveBeenCalledTimes(1)

    // A later change of the store, such as a token refresh, does not repeat it.
    auth.userContextReady = false
    await nextTick()
    auth.userContextReady = true
    await nextTick()
    expect(getAccount).toHaveBeenCalledTimes(1)
  })

  it('calls at once when the user is signed in already', async () => {
    auth.userContextReady = true
    useLeaseRenewal()
    await nextTick()
    expect(getAccount).toHaveBeenCalledTimes(1)
  })

  it('never calls in a public link context', async () => {
    auth.publicLinkContextReady = true
    useLeaseRenewal()
    auth.userContextReady = true
    await nextTick()
    expect(getAccount).not.toHaveBeenCalled()
  })

  it('ignores a failed call', async () => {
    getAccount.mockRejectedValue(new Error('offline'))
    const unhandled = vi.fn()
    process.on('unhandledRejection', unhandled)
    auth.userContextReady = true
    expect(() => useLeaseRenewal()).not.toThrow()
    await nextTick()
    await new Promise((r) => setTimeout(r, 0))
    process.off('unhandledRejection', unhandled)
    expect(getAccount).toHaveBeenCalledTimes(1)
    expect(unhandled).not.toHaveBeenCalled()
  })
})
