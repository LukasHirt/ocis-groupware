import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AppMenuItemExtension } from '@ownclouders/web-pkg'
import type { RouteRecordRaw } from 'vue-router'
import app from '../../src/index'
import translations from '../../src/l10n/translations.json'

// The host injects gettext; the tests only need the strings back.
vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

// web-pkg's module pulls in the whole design system; the application only
// needs its defineWebApplication wrapper.
vi.mock('@ownclouders/web-pkg', async () =>
  (await import('./mocks/webPkg')).webPkgMock({ useRouter: () => ({ currentRoute }) })
)

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

const currentRoute = vi.hoisted(() => ({ value: { name: undefined as string | undefined } }))

describe('the application', () => {
  const script = app.setup({ applicationConfig: {} })
  const inArea = (name: string | undefined) => {
    currentRoute.value = { name }
  }

  it('registers under the id organizer with a name', () => {
    expect(script.appInfo?.id).toBe('organizer')
    expect(script.appInfo?.name).toBe('Calendar and Contacts')
  })

  it('does not take an id that the proxy sends to the service', () => {
    for (const reserved of ['groupware', 'dav', 'graph', 'web', 'remote.php', '.well-known']) {
      expect(script.appInfo?.id).not.toBe(reserved)
    }
  })

  it('carries its icon', () => {
    expect(script.appInfo?.img).toMatch(/^data:image\/svg\+xml/)
  })

  it('hands its compiled translations to the host', () => {
    expect(script.translations).toEqual(translations)
  })

  it('has one route per page and redirects from its root and its areas', () => {
    const routes = script.routes as RouteRecordRaw[]
    const byPath = Object.fromEntries(routes.map((r) => [r.path, r]))
    expect(Object.keys(byPath).sort()).toEqual([
      '/',
      '/calendar',
      '/calendar/devices',
      '/calendar/events',
      '/contacts',
      '/contacts/devices',
      '/contacts/list'
    ])
    expect(byPath['/'].redirect).toEqual({ name: 'organizer-calendar-events' })
    expect(byPath['/calendar'].redirect).toEqual({ name: 'organizer-calendar-events' })
    expect(byPath['/contacts'].redirect).toEqual({ name: 'organizer-contacts-list' })
    expect(byPath['/calendar/events'].name).toBe('organizer-calendar-events')
    expect(byPath['/calendar/devices'].name).toBe('organizer-calendar-devices')
    expect(byPath['/contacts/list'].name).toBe('organizer-contacts-list')
    expect(byPath['/contacts/devices'].name).toBe('organizer-contacts-devices')
    expect(byPath['/calendar/devices'].component).toBe(byPath['/contacts/devices'].component)
    for (const path of Object.keys(byPath).filter((p) => !byPath[p].redirect)) {
      expect(byPath[path].meta?.authContext).toBe('user')
    }
    expect(byPath['/calendar/devices'].meta?.title).toBe('Devices')
  })

  type Item = {
    name: string
    icon: string
    priority: number
    route: unknown
    isVisible: () => boolean
  }
  const items = () => script.navItems as unknown as Item[]

  it('has two navigation items for each area, the second one for devices', () => {
    expect(items().map((i) => [i.name, i.icon, i.priority])).toEqual([
      ['Calendar', 'calendar', 10],
      ['Devices', 'smartphone', 90],
      ['Contacts', 'contacts-book', 10],
      ['Devices', 'smartphone', 90]
    ])
    expect(items().map((i) => i.route)).toEqual([
      { name: 'organizer-calendar-events' },
      { name: 'organizer-calendar-devices' },
      { name: 'organizer-contacts-list' },
      { name: 'organizer-contacts-devices' }
    ])
  })

  it.each([
    ['organizer-calendar-events', [true, true, false, false]],
    ['organizer-calendar-devices', [true, true, false, false]],
    ['organizer-contacts-list', [false, false, true, true]],
    ['organizer-contacts-devices', [false, false, true, true]],
    ['files-spaces-generic', [false, false, false, false]],
    [undefined, [false, false, false, false]]
  ])('shows the navigation of the area of the route %s', (name, visible) => {
    inArea(name)
    expect(items().map((i) => i.isVisible())).toEqual(visible)
  })

  it('adds an entry per area to the application menu', () => {
    const extensions = script.extensions?.value as AppMenuItemExtension[]
    expect(extensions).toHaveLength(2)
    expect(extensions.map((e) => e.type)).toEqual(['appMenuItem', 'appMenuItem'])
    expect(extensions.map((e) => e.id)).toEqual([
      'organizer.calendar.app-menu-item',
      'organizer.contacts.app-menu-item'
    ])
    expect(extensions.map((e) => e.label())).toEqual(['Calendar', 'Contacts'])
    expect(extensions.map((e) => e.icon)).toEqual(['calendar', 'contacts-book'])
    expect(extensions.map((e) => e.color)).toEqual(['#1f6f8b', '#2f9e6e'])
    expect(extensions.map((e) => e.path)).toEqual(['/organizer/calendar', '/organizer/contacts'])
  })

  it('renders a heading on each page', () => {
    const routes = script.routes as RouteRecordRaw[]
    for (const [path, heading] of [
      ['/calendar/events', 'Calendar'],
      ['/contacts/list', 'Contacts'],
      ['/calendar/devices', 'Devices']
    ]) {
      const route = routes.find((r) => r.path === path)
      const wrapper = mount(route!.component as never, mountOptions)
      expect(wrapper.find('h1').text()).toBe(heading)
    }
  })

  it('lists the calendars with their owners', async () => {
    const routes = script.routes as RouteRecordRaw[]
    const wrapper = mount(routes.find((r) => r.path === '/calendar/events')!.component as never, mountOptions)
    await flushPromises()
    const items = wrapper.findAll('[data-testid="calendar-item"]')
    expect(items.map((i) => i.find('.name').text())).toEqual(['Personal', 'Work', 'Team'])
    expect(items[2].text()).toContain('Marie Curie')
  })

  it('lists the address books', async () => {
    const routes = script.routes as RouteRecordRaw[]
    const wrapper = mount(routes.find((r) => r.path === '/contacts/list')!.component as never, mountOptions)
    await flushPromises()
    const items = wrapper.findAll('[data-testid="folder-item"]')
    expect(items.map((i) => i.find('.name').text())).toEqual(['Contacts', 'Team'])
    expect(items[1].text()).toContain('Marie Curie')
  })
})
