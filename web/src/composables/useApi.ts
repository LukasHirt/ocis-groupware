import { useClientService, useMessages } from '@ownclouders/web-pkg'
import { useGettext } from 'vue3-gettext'
import { createClient, type Client } from '../api/client'
import { toApiError } from '../api/errors'

// useApi gives the typed client, bound to the signed-in user's HTTP client of
// oCIS Web (bearer token, refreshed when it expires), and run(), which shows
// a failed call as an error toast.
export function useApi() {
  const { httpAuthenticated } = useClientService()
  const { showErrorMessage } = useMessages()
  const { $gettext } = useGettext()

  const client: Client = createClient((req) =>
    httpAuthenticated.request({
      method: req.method,
      url: req.url,
      params: req.params,
      data: req.data,
      headers: req.headers,
      responseType: req.responseType
    })
  )

  // run awaits the call. A failure is reported with the title and the
  // server's message, and run answers undefined, so callers keep their
  // previous state.
  async function run<T>(title: string, call: (client: Client) => Promise<T>): Promise<T | undefined> {
    try {
      return await call(client)
    } catch (err) {
      const e = toApiError(err)
      showErrorMessage({
        title,
        desc: e.status === 412 ? $gettext('The item was changed elsewhere. Reload and try again.') : e.message,
        errors: [e]
      })
      return undefined
    }
  }

  return { client, run }
}
