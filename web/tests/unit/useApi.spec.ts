import { beforeEach, describe, expect, it, vi } from 'vitest'

const request = vi.fn()
const showErrorMessage = vi.fn()

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))
vi.mock('@ownclouders/web-pkg', () => ({
  useClientService: () => ({ httpAuthenticated: { request } }),
  useMessages: () => ({ showErrorMessage })
}))

import { useApi } from '../../src/composables/useApi'

beforeEach(() => {
  request.mockReset()
  showErrorMessage.mockReset()
})

describe('useApi', () => {
  it('sends requests through the authenticated client of the host', async () => {
    request.mockResolvedValue({ status: 200, data: { value: [] } })
    const { client } = useApi()
    await client.listCalendars()
    expect(request).toHaveBeenCalledWith(
      expect.objectContaining({ method: 'GET', url: '/graph/v1.0/me/calendars' })
    )
  })

  it('returns the result of a call that works', async () => {
    request.mockResolvedValue({ status: 200, data: { value: [] } })
    const { run } = useApi()
    expect(await run('Could not load', (c) => c.listCalendars())).toEqual([])
    expect(showErrorMessage).not.toHaveBeenCalled()
  })

  it('shows an error toast with the server message and answers undefined', async () => {
    request.mockRejectedValue({
      response: { status: 403, data: { error: { code: 'accessDenied', message: 'Not yours' } } }
    })
    const { run } = useApi()
    expect(await run('Could not load calendars', (c) => c.listCalendars())).toBeUndefined()
    expect(showErrorMessage).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'Could not load calendars', desc: 'Not yours' })
    )
  })

  it('explains a failed precondition', async () => {
    request.mockRejectedValue({ response: { status: 412, data: {} } })
    const { run } = useApi()
    await run('Could not save', (c) => c.deleteEvent('e', 'W/"1"'))
    expect(showErrorMessage.mock.calls[0][0].desc).toMatch(/changed elsewhere/)
  })
})
