import { describe, expect, it } from 'vitest'
import { createClient, type Request, type Response } from '../../../src/api/client'
import type { Account } from '../../../src/api/types'
import account from '../../../../internal/graphapi/testdata/contract/account.json'

function fake(answer: Response) {
  const calls: Request[] = []
  const client = createClient((req) => {
    calls.push(req)
    return Promise.resolve(answer)
  })
  return { client, calls }
}

describe('account', () => {
  it('reads the DAV address of the signed-in user', async () => {
    const { client, calls } = fake({ status: 200, data: account })
    const got: Account = await client.getAccount()
    expect(got).toEqual(account)
    expect(calls).toEqual([{ method: 'GET', url: '/groupware/v1/me' }])
  })
})

describe('app passwords', () => {
  const item = {
    id: 'a1',
    name: 'Phone',
    created_at: '2026-11-05T10:00:00Z',
    expires_at: '2027-11-05T10:00:00Z',
    last_used_at: null
  }

  it('lists them from the wrapper object', async () => {
    const { client, calls } = fake({ status: 200, data: { app_passwords: [item] } })
    expect(await client.listAppPasswords()).toEqual([item])
    expect(calls[0]).toMatchObject({ method: 'GET', url: '/groupware/v1/app-passwords' })
  })

  it('creates one with a name and an optional lifetime', async () => {
    const { client, calls } = fake({ status: 201, data: { ...item, password: 'abc' } })
    const issued = await client.createAppPassword({ name: 'Phone', expires_in_days: 30 })
    expect(issued.password).toBe('abc')
    expect(calls[0]).toMatchObject({
      method: 'POST',
      url: '/groupware/v1/app-passwords',
      data: { name: 'Phone', expires_in_days: 30 }
    })
  })

  it('revokes one by id, escaping it', async () => {
    const { client, calls } = fake({ status: 204, data: undefined })
    await client.revokeAppPassword('a/1')
    expect(calls[0]).toMatchObject({ method: 'DELETE', url: '/groupware/v1/app-passwords/a%2F1' })
  })
})
