import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AppMenuItemExtension } from '@ownclouders/web-pkg'
import type { RouteRecordRaw } from 'vue-router'
import PageLayout from '../../src/components/PageLayout.vue'
import app from '../../src/index'
import translations from '../../src/l10n/translations.json'

// The host injects gettext; the tests only need the strings back.
vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

// web-pkg's module pulls in the whole design system; the application only
// needs its defineWebApplication wrapper.
vi.mock('@ownclouders/web-pkg', () => ({
  defineWebApplication: (args: unknown) => args,
  useAuthStore: () => ({ userContextReady: false, publicLinkContextReady: false })
}))

// The pages read through the API composable; the host's services are not
// available here, so it answers from the golden files.
vi.mock('../../src/composables/useApi', async () => {
  const { default: calendars } = await import(
    '../../../internal/graphapi/testdata/contract/calendars.json'
  )
  const { default: folders } = await import(
    '../../../internal/graphapi/testdata/contract/contact-folders.json'
  )
  const client = {
    listCalendars: () => Promise.resolve(calendars.value),
    calendarView: () => Promise.resolve([]),
    listContactFolders: () => Promise.resolve(folders.value),
    listContacts: () => Promise.resolve([]),
    getAccount: () => new Promise(() => {}),
    listAppPasswords: () => new Promise(() => {})
  }
  return { useApi: () => ({ client, run: (_t: string, f: (c: unknown) => unknown) => f(client) }) }
})

const mountOptions = { global: { mocks: { $gettext: (s: string) => s } } }

describe('the application', () => {
  const script = app.setup({ applicationConfig: {} })

  it('registers under the id groupware with a name', () => {
    expect(script.appInfo?.id).toBe('groupware')
    expect(script.appInfo?.name).toBe('Calendar and Contacts')
  })

  it('carries its icon and brand colour', () => {
    expect(script.appInfo?.img).toMatch(/^data:image\/svg\+xml/)
    const [item] = script.extensions?.value as AppMenuItemExtension[]
    expect(item.color).toBe('#1f6f8b')
  })

  it('hands its compiled translations to the host', () => {
    expect(script.translations).toEqual(translations)
  })

  it('has a calendar and a contacts route, and a redirect from its root', () => {
    const routes = script.routes as RouteRecordRaw[]
    const byPath = Object.fromEntries(routes.map((r) => [r.path, r]))
    expect(Object.keys(byPath).sort()).toEqual(['/', '/account', '/calendar', '/contacts'])
    expect(byPath['/'].redirect).toEqual({ name: 'groupware-calendar' })
    expect(byPath['/calendar'].name).toBe('groupware-calendar')
    expect(byPath['/contacts'].name).toBe('groupware-contacts')
    expect(byPath['/account'].name).toBe('groupware-account')
    for (const path of ['/calendar', '/contacts', '/account']) {
      expect(byPath[path].meta?.authContext).toBe('user')
    }
  })

  it('offers calendar, contacts and the account in the sidebar', () => {
    const items = script.navItems as { name: string | (() => string); route?: unknown }[]
    const names = items.map((i) => (typeof i.name === 'function' ? i.name() : i.name))
    expect(names).toEqual(['Calendar', 'Contacts', 'Account'])
    expect(items.map((i) => i.route)).toEqual([
      { name: 'groupware-calendar' },
      { name: 'groupware-contacts' },
      { name: 'groupware-account' }
    ])
  })

  it('adds itself to the application menu', () => {
    const extensions = script.extensions?.value as AppMenuItemExtension[]
    expect(extensions).toHaveLength(1)
    const [item] = extensions
    expect(item.type).toBe('appMenuItem')
    expect(item.label()).toBe('Calendar and Contacts')
    expect(item.path).toBe('/groupware')
  })

  it('renders a heading on each page', () => {
    const routes = script.routes as RouteRecordRaw[]
    for (const [path, heading] of [
      ['/calendar', 'Calendar'],
      ['/contacts', 'Contacts'],
      ['/account', 'Account']
    ]) {
      const route = routes.find((r) => r.path === path)
      const wrapper = mount(route!.component as never, mountOptions)
      expect(wrapper.find('h1').text()).toBe(heading)
    }
  })

  it('lists the calendars with their owners', async () => {
    const routes = script.routes as RouteRecordRaw[]
    const wrapper = mount(routes.find((r) => r.path === '/calendar')!.component as never, mountOptions)
    await flushPromises()
    const items = wrapper.findAll('[data-testid="calendar-item"]')
    expect(items.map((i) => i.find('.name').text())).toEqual(['Personal', 'Work', 'Team'])
    expect(items[2].text()).toContain('Marie Curie')
  })

  it('lists the address books', async () => {
    const routes = script.routes as RouteRecordRaw[]
    const wrapper = mount(routes.find((r) => r.path === '/contacts')!.component as never, mountOptions)
    await flushPromises()
    const items = wrapper.findAll('[data-testid="folder-item"]')
    expect(items.map((i) => i.find('.name').text())).toEqual(['Contacts', 'Team'])
    expect(items[1].text()).toContain('Marie Curie')
  })

  it('says so when there is nothing to list', () => {
    const wrapper = mount(PageLayout, {
      ...mountOptions,
      props: { title: 'Calendar', empty: true, emptyText: 'No calendars yet.' }
    })
    expect(wrapper.text()).toContain('No calendars yet.')
  })
})
