import type { GraphErrorBody } from './types'

// ApiError is a failed call to the service. code and message come from the
// Graph error body when the server sent one.
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
  }

  // The item changed since it was read (If-Match did not match).
  get isConflict(): boolean {
    return this.status === 412 || this.status === 409
  }

  get isNotFound(): boolean {
    return this.status === 404
  }
}

// toApiError turns whatever the transport threw into an ApiError. The
// transport is axios in the browser: a failed status carries `response`, a
// network failure does not.
export function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) {
    return err
  }
  const response = (err as { response?: { status?: number; data?: unknown } } | null)?.response
  if (response && typeof response.status === 'number') {
    const body = (response.data as Partial<GraphErrorBody> | undefined)?.error
    return new ApiError(
      response.status,
      body?.code ?? 'generalException',
      body?.message ?? `The server answered ${response.status}`,
      body?.innerError?.['request-id']
    )
  }
  const message = err instanceof Error ? err.message : String(err)
  return new ApiError(0, 'networkError', message)
}
