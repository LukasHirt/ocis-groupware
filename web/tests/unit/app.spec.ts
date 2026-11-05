import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { AppMenuItemExtension } from '@ownclouders/web-pkg'
import type { RouteRecordRaw } from 'vue-router'
import app from '../../src/index'

// The host injects gettext; the tests only need the strings back.
vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

// web-pkg's module pulls in the whole design system; the application only
// needs its defineWebApplication wrapper.
vi.mock('@ownclouders/web-pkg', () => ({
  defineWebApplication: (args: unknown) => args
}))

const mountOptions = { global: { mocks: { $gettext: (s: string) => s } } }

describe('the application', () => {
  const script = app.setup({ applicationConfig: {} })

  it('registers under the id groupware with a name', () => {
    expect(script.appInfo?.id).toBe('groupware')
    expect(script.appInfo?.name).toBe('Calendar and Contacts')
  })

  it('has a calendar and a contacts route, and a redirect from its root', () => {
    const routes = script.routes as RouteRecordRaw[]
    const byPath = Object.fromEntries(routes.map((r) => [r.path, r]))
    expect(Object.keys(byPath).sort()).toEqual(['/', '/calendar', '/contacts'])
    expect(byPath['/'].redirect).toEqual({ name: 'groupware-calendar' })
    expect(byPath['/calendar'].name).toBe('groupware-calendar')
    expect(byPath['/contacts'].name).toBe('groupware-contacts')
    for (const path of ['/calendar', '/contacts']) {
      expect(byPath[path].meta?.authContext).toBe('user')
    }
  })

  it('offers calendar and contacts in the sidebar', () => {
    const items = script.navItems as { name: string | (() => string); route?: unknown }[]
    const names = items.map((i) => (typeof i.name === 'function' ? i.name() : i.name))
    expect(names).toEqual(['Calendar', 'Contacts'])
    expect(items.map((i) => i.route)).toEqual([
      { name: 'groupware-calendar' },
      { name: 'groupware-contacts' }
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
      ['/contacts', 'Contacts']
    ]) {
      const route = routes.find((r) => r.path === path)
      const wrapper = mount(route!.component as never, mountOptions)
      expect(wrapper.find('h1').text()).toBe(heading)
    }
  })
})
