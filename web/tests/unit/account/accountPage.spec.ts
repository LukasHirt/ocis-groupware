import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import account from '../../../../internal/graphapi/testdata/contract/account.json'
import DevicesPage from '../../../src/pages/DevicesPage.vue'

// The design system and the host's services are not here; see mocks/webPkg.
vi.mock('@ownclouders/web-pkg', async () => (await import('../mocks/webPkg')).webPkgMock())

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const api = {
  getAccount: vi.fn(),
  listAppPasswords: vi.fn(),
  createAppPassword: vi.fn(),
  revokeAppPassword: vi.fn()
}
vi.mock('../../../src/composables/useApi', () => ({
  useApi: () => ({
    client: api,
    run: (_t: string, f: (c: unknown) => unknown) => Promise.resolve(f(api))
  })
}))

const gettext = (s: string, params: Record<string, string> = {}) =>
  s.replace(/%\{(\w+)\}/g, (_, k: string) => params[k] ?? '')
const global = { mocks: { $gettext: gettext } }

const phone = {
  id: 'a1',
  name: 'Phone',
  created_at: '2026-11-01T10:00:00Z',
  expires_at: '2027-11-01T10:00:00Z',
  last_used_at: '2026-11-04T08:00:00Z'
}
const laptop = {
  id: 'a2',
  name: 'Laptop',
  created_at: '2026-10-01T10:00:00Z',
  expires_at: '2026-11-02T10:00:00Z',
  last_used_at: null
}

let writeText: ReturnType<typeof vi.fn>

async function page() {
  const wrapper = mount(DevicesPage, { global, attachTo: document.body })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
  api.getAccount.mockResolvedValue(structuredClone(account))
  api.listAppPasswords.mockResolvedValue({
    appPasswords: [structuredClone(phone), structuredClone(laptop)],
    maxLifetimeDays: 365
  })
  api.createAppPassword.mockReset()
  api.revokeAppPassword.mockReset().mockResolvedValue(undefined)
  writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
})

describe('DAV address', () => {
  it('shows the server address, the username and the two home addresses', async () => {
    const wrapper = await page()
    const text = wrapper.get('[data-testid="dav-account"]').text()
    expect(text).toContain('https://ocis.example/dav/')
    expect(text).toContain('einstein')
    expect(text).toContain('https://ocis.example/dav/calendars/einstein/')
    expect(text).toContain('https://ocis.example/dav/addressbooks/einstein/')
  })

  it('copies an address', async () => {
    const wrapper = await page()
    await wrapper.get('[data-testid="copy-dav-url"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('https://ocis.example/dav/')
  })

  it('has a setup hint for Thunderbird, Apple and DAVx5', async () => {
    const wrapper = await page()
    const hints = wrapper.get('[data-testid="dav-setup"]').text()
    for (const name of ['Thunderbird', 'Apple', 'DAVx5']) {
      expect(hints).toContain(name)
    }
  })

  it('says so when the account has no DAV username', async () => {
    api.getAccount.mockResolvedValue({
      username: '',
      displayName: 'Albert Einstein',
      dav: { url: 'https://ocis.example/dav/', calendarHomeUrl: '', addressBookHomeUrl: '' }
    })
    const wrapper = await page()
    expect(wrapper.find('[data-testid="dav-no-username"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dav-username-conflict"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="copy-dav-calendar"]').exists()).toBe(false)
  })

  it('tells the user when an active account holds their username', async () => {
    api.getAccount.mockResolvedValue({
      username: '',
      username_conflict: true,
      displayName: 'Albert Einstein',
      dav: { url: 'https://ocis.example/dav/', calendarHomeUrl: '', addressBookHomeUrl: '' }
    })
    const wrapper = await page()
    expect(wrapper.find('[data-testid="dav-username-conflict"]').text()).toBe(
      'Your user name is in use by another account. Ask your administrator.'
    )
    expect(wrapper.find('[data-testid="dav-no-username"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="copy-dav-calendar"]').exists()).toBe(false)
  })
})

describe('app password validity', () => {
  const now = new Date('2026-11-05T12:00:00Z')
  function withValidUntil(iso: string | null) {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(now)
    api.getAccount.mockResolvedValue({ ...structuredClone(account), app_passwords_valid_until: iso })
    return page()
  }

  it('shows nothing when the lease is off or unknown', async () => {
    const wrapper = await withValidUntil(null)
    expect(wrapper.find('[data-testid="dav-valid-until"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="dav-valid-until-warning"]').exists()).toBe(false)
  })

  it('says until when app passwords work and that opening oCIS Web renews it', async () => {
    const wrapper = await withValidUntil('2026-12-05T10:00:00Z')
    const text = wrapper.get('[data-testid="dav-valid-until"]').text()
    expect(text).toContain(new Date('2026-12-05T10:00:00Z').toLocaleDateString())
    expect(text).toContain('open oCIS Web again')
    expect(wrapper.find('[data-testid="dav-valid-until-warning"]').exists()).toBe(false)
  })

  it('warns when fewer than 7 days remain', async () => {
    const wrapper = await withValidUntil('2026-11-11T12:00:00Z')
    expect(wrapper.find('[data-testid="dav-valid-until-warning"]').exists()).toBe(true)
  })

  it('does not warn with exactly 7 days left', async () => {
    const wrapper = await withValidUntil('2026-11-12T12:00:00Z')
    expect(wrapper.find('[data-testid="dav-valid-until-warning"]').exists()).toBe(false)
  })

  it('warns when the date has passed', async () => {
    const wrapper = await withValidUntil('2026-11-04T12:00:00Z')
    expect(wrapper.get('[data-testid="dav-valid-until-warning"]').text()).toContain('stopped')
  })
})

describe('app password list', () => {
  it('lists each password with its expiry and last use', async () => {
    const wrapper = await page()
    const [first, second] = wrapper.findAll('[data-testid="app-password-item"]')
    expect(first.text()).toContain('Phone')
    expect(first.text()).toContain('Last used')
    expect(second.text()).toContain('Laptop')
    expect(second.text()).toContain('Never used')
  })

  it('marks one that has expired', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-11-05T12:00:00Z'))
    const wrapper = await page()
    const [first, second] = wrapper.findAll('[data-testid="app-password-item"]')
    expect(first.find('[data-testid="app-password-expired"]').exists()).toBe(false)
    expect(second.find('[data-testid="app-password-expired"]').exists()).toBe(true)
  })

  it('says so when there are none', async () => {
    api.listAppPasswords.mockResolvedValue({ appPasswords: [], maxLifetimeDays: 365 })
    const wrapper = await page()
    expect(wrapper.find('[data-testid="app-password-none"]').exists()).toBe(true)
  })
})

describe('creating an app password', () => {
  async function open() {
    const wrapper = await page()
    await wrapper.get('[data-testid="app-password-add"]').trigger('click')
    return wrapper
  }

  it('needs a name', async () => {
    const wrapper = await open()
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    expect(api.createAppPassword).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="app-password-name-error"]').exists()).toBe(true)
  })

  it('sends the name and the default lifetime, then shows the password once', async () => {
    api.createAppPassword.mockResolvedValue({ ...phone, name: 'Tablet', password: 'mfrggzdfmztwq2lknnwg23tpobyxe43u' })
    const wrapper = await open()
    await wrapper.get('[data-testid="app-password-name"]').setValue('Tablet')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    expect(api.createAppPassword).toHaveBeenCalledWith({ name: 'Tablet' })
    expect(wrapper.get('[data-testid="app-password-secret"]').text()).toContain(
      'mfrggzdfmztwq2lknnwg23tpobyxe43u'
    )

    await wrapper.get('[data-testid="app-password-copy"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('mfrggzdfmztwq2lknnwg23tpobyxe43u')

    // The list is read again, and the password is gone for good after Done.
    api.listAppPasswords.mockClear()
    await wrapper.get('[data-testid="app-password-done"]').trigger('click')
    await flushPromises()
    expect(api.listAppPasswords).toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('mfrggzdfmztwq2lknnwg23tpobyxe43u')
  })

  it('sends a shorter lifetime when one is chosen', async () => {
    api.createAppPassword.mockResolvedValue({ ...phone, password: 'x' })
    const wrapper = await open()
    await wrapper.get('[data-testid="app-password-name"]').setValue('Guest phone')
    await wrapper.get('[data-testid="app-password-expiry"]').setValue('30')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    expect(api.createAppPassword).toHaveBeenCalledWith({ name: 'Guest phone', expires_in_days: 30 })
  })

  it('offers the server maximum as the default and nothing longer', async () => {
    api.listAppPasswords.mockResolvedValue({ appPasswords: [], maxLifetimeDays: 180 })
    api.createAppPassword.mockResolvedValue({ ...phone, password: 'x' })
    const wrapper = await open()
    const select = wrapper.get('[data-testid="app-password-expiry"]')
    const options = select.findAll('option').map((o) => o.text())
    expect(options).toEqual(['180 days (default)', '90 days', '30 days', '7 days'])
    expect((select.element as HTMLSelectElement).value).toBe('180')

    // The default is not sent, so the server applies its own maximum.
    await wrapper.get('[data-testid="app-password-name"]').setValue('Laptop')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    expect(api.createAppPassword).toHaveBeenCalledWith({ name: 'Laptop' })
  })

  it('drops the choices above a short maximum', async () => {
    api.listAppPasswords.mockResolvedValue({ appPasswords: [], maxLifetimeDays: 30 })
    const wrapper = await open()
    const options = wrapper.findAll('[data-testid="app-password-expiry"] option').map((o) => o.text())
    expect(options).toEqual(['30 days (default)', '7 days'])
  })

  it('keeps the password when the dialog is dismissed with Escape', async () => {
    api.createAppPassword.mockResolvedValue({ ...phone, password: 'secret-once' })
    const wrapper = await open()
    await wrapper.get('[data-testid="app-password-name"]').setValue('Tablet')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="app-password-secret"]').exists()).toBe(true)
  })

  it('stays on the form when the server refuses', async () => {
    api.createAppPassword.mockResolvedValue(undefined)
    const wrapper = await open()
    await wrapper.get('[data-testid="app-password-name"]').setValue('Tablet')
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="app-password-secret"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="app-password-name"]').exists()).toBe(true)
  })

  it('cancels without a request', async () => {
    const wrapper = await open()
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(api.createAppPassword).not.toHaveBeenCalled()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
})

describe('revoking an app password', () => {
  it('asks first, then revokes that one and reads the list again', async () => {
    const wrapper = await page()
    await wrapper.findAll('[data-testid="app-password-revoke"]')[1].trigger('click')
    expect(api.revokeAppPassword).not.toHaveBeenCalled()
    expect(wrapper.get('[role="dialog"]').text()).toContain('Laptop')

    api.listAppPasswords.mockResolvedValue({ appPasswords: [structuredClone(phone)], maxLifetimeDays: 365 })
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    await flushPromises()
    expect(api.revokeAppPassword).toHaveBeenCalledWith('a2')
    expect(wrapper.findAll('[data-testid="app-password-item"]')).toHaveLength(1)
  })

  it('does nothing when cancelled', async () => {
    const wrapper = await page()
    await wrapper.findAll('[data-testid="app-password-revoke"]')[0].trigger('click')
    await wrapper.get('[data-testid="dialog-cancel"]').trigger('click')
    expect(api.revokeAppPassword).not.toHaveBeenCalled()
  })
})
